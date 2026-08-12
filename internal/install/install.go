// Package install performs the client-side setup that used to be four
// paragraphs of a runbook: register this server with Claude Code, and
// put the /review-branch command where Claude Code looks for it.
//
// It adds no capability. Everything here is something a developer could
// type, and the reason it is a command is that they were typing it
// wrong — the registration takes an absolute path to a binary and an
// absolute path to a workspace, neither of which a person has to hand,
// and the command file had to be copied out of a clone that a `go
// install` user does not have.
//
// Like doctor, it is idempotent and re-runnable: running it twice is how
// you repoint an existing installation at a rebuilt binary.
package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/truelogics/engineering-mcp/integration"
	"github.com/truelogics/engineering-mcp/internal/workspace"
)

// Status is a step's outcome. Skip is not a failure: a machine without
// the claude CLI is a legitimate machine, and the server still works on
// it over any other MCP client.
type Status string

const (
	StatusDone Status = "done"
	StatusSkip Status = "skip"
	StatusFail Status = "fail"
)

// Step is one action and what it did. Detail says what changed on the
// machine, not what was attempted, so a re-run reads differently from a
// first run.
type Step struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// Env is every part of the outside world install touches, injected so
// the steps can be tested without a Claude Code installation or a home
// directory to write into.
type Env struct {
	// Self is the absolute path of the running binary — what Claude Code
	// will be told to launch. A relative path here would produce a
	// registration that works in one directory.
	Self string
	// WorkingDir is where the developer ran the command, and where the
	// workspace search starts.
	WorkingDir string
	// WorkspaceFlag mirrors the server's --workspace.
	WorkspaceFlag string
	// Home locates the user-scope Claude Code command directory.
	Home string

	LookPath  func(file string) (string, error)
	Run       func(ctx context.Context, dir, name string, args ...string) (string, error)
	MkdirAll  func(dir string) error
	ReadFile  func(path string) (string, error)
	WriteFile func(path, content string) error
	Exists    func(path string) bool
}

// serverName is the name the registration is filed under. Claude Code
// prefixes every tool with it — mcp__engineering__find_engineering_rules
// — and integration/claude-code/review-branch.md names those tools
// literally in its allowed-tools list. Registering under any other name
// silently disallows every tool the command needs.
const serverName = "engineering"

// Run performs each step in order and returns what happened. It never
// returns an error: a step that could not be performed is a finding with
// a fix attached, and the steps after it may still be worth doing.
func Run(ctx context.Context, env Env, force bool) []Step {
	ws, wsStep := resolveWorkspace(env)
	steps := []Step{wsStep}

	// Registration is skipped rather than attempted without a workspace.
	// The server exits at startup when it cannot resolve one, so
	// registering here would hand the developer a Claude Code entry that
	// fails on every session — and `claude mcp list` would report it as
	// configured.
	if wsStep.Status == StatusFail {
		steps = append(steps,
			Step{Name: "Claude Code registration", Status: StatusSkip, Detail: "skipped: no workspace to serve"},
			installReviewCommand(env, force))
		return steps
	}

	steps = append(steps, register(ctx, env, ws.Dir))
	steps = append(steps, installReviewCommand(env, force))
	return steps
}

// resolveWorkspace uses the server's own resolver, so the workspace
// written into the registration is the one the server would have chosen
// standing here. Deriving it any other way would let install and serve
// disagree about which knowledge base answers.
func resolveWorkspace(env Env) (workspace.Resolved, Step) {
	const name = "Workspace"

	ws, err := workspace.Resolve(env.WorkspaceFlag, env.WorkingDir)
	if err != nil {
		return workspace.Resolved{}, Step{
			Name:   name,
			Status: StatusFail,
			Detail: err.Error(),
			Fix: "create one over the repositories you want indexed together, then run this again:\n" +
				"    eng setup <directory holding your repositories> --rules <your rules repo>",
		}
	}
	return ws, Step{
		Name:   name,
		Status: StatusDone,
		Detail: fmt.Sprintf("%s (%s)", ws.Dir, ws.Source),
	}
}

// register points Claude Code at this binary, at user scope, for every
// project on the machine.
//
// Remove-then-add rather than add alone. `claude mcp add` refuses a name
// that already exists, so a developer who rebuilt to a new location — the
// single most common way this breaks, and one doctor has a check for —
// would be told "already exists" by the command whose entire job is to
// fix it.
func register(ctx context.Context, env Env, workspaceDir string) Step {
	const name = "Claude Code registration"

	claude, err := env.LookPath("claude")
	if err != nil {
		return Step{
			Name:   name,
			Status: StatusSkip,
			Detail: "the claude CLI is not on $PATH, so nothing was registered",
			Fix: "install Claude Code and run this again, or register another MCP client by hand:\n" +
				fmt.Sprintf("    command: %s\n    env:     %s=%s", env.Self, workspace.EnvVar, workspaceDir),
		}
	}

	// Read before writing, so the report can say what was replaced. A
	// step that prints "registered" whether or not anything changed
	// teaches the reader to stop reading it.
	previous, hadPrevious := currentCommand(ctx, env, claude)

	_, _ = env.Run(ctx, env.WorkingDir, claude, "mcp", "remove", serverName, "--scope", "user")

	out, err := env.Run(ctx, env.WorkingDir, claude, "mcp", "add", serverName,
		"--scope", "user",
		"-e", workspace.EnvVar+"="+workspaceDir,
		"--", env.Self)
	if err != nil {
		return Step{
			Name:   name,
			Status: StatusFail,
			Detail: fmt.Sprintf("claude mcp add failed: %v\n%s", err, trimBlank(out)),
			Fix: "run it by hand to see the whole message:\n" +
				fmt.Sprintf("    claude mcp add %s --scope user \\\n      -e %s=%s \\\n      -- %s",
					serverName, workspace.EnvVar, workspaceDir, env.Self),
		}
	}

	detail := fmt.Sprintf("%s -> %s\n%s=%s", serverName, env.Self, workspace.EnvVar, workspaceDir)
	if hadPrevious && previous != env.Self {
		detail += fmt.Sprintf("\nreplaced a registration pointing at %s", previous)
	}
	return Step{Name: name, Status: StatusDone, Detail: detail}
}

// currentCommand reports the binary Claude Code launches today, if
// anything is registered. Absent and unreadable are the same answer here
// — this is only used to say what changed.
func currentCommand(ctx context.Context, env Env, claude string) (string, bool) {
	out, err := env.Run(ctx, env.WorkingDir, claude, "mcp", "get", serverName)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "Command:"); found {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// installReviewCommand writes the /review-branch command at user scope.
//
// Not a convenience. On a machine with several MCP servers installed,
// Claude Code defers tool schemas: this server's four tools are absent
// from the model's prompt and cannot be called at all until something
// names them. Measured on this repository, on the same commit: 3 MCP
// calls out of 40 tool calls with the command installed, 0 out of 42
// without it (docs/reports/TOOL_DISCOVERY_EXPERIMENT.md).
func installReviewCommand(env Env, force bool) Step {
	const name = "/review-branch command"

	if env.Home == "" {
		return Step{
			Name:   name,
			Status: StatusFail,
			Detail: "$HOME is unset, so there is nowhere to install it at user scope",
			Fix:    "set $HOME, or write the command into <project>/.claude/commands/ yourself",
		}
	}

	dir := filepath.Join(env.Home, ".claude", "commands")
	path := filepath.Join(dir, integration.ReviewBranchFilename)

	// An existing file is left alone unless it is ours. A developer who
	// edited the command has customised how their reviews work, and
	// silently replacing that is the kind of overwrite you only notice
	// three reviews later.
	if env.Exists(path) && !force {
		existing, err := env.ReadFile(path)
		switch {
		case err != nil:
			return Step{
				Name:   name,
				Status: StatusFail,
				Detail: fmt.Sprintf("%s exists but could not be read: %v", path, err),
				Fix:    "check its permissions, or re-run with --force to overwrite it",
			}
		case existing == integration.ReviewBranchCommand:
			return Step{Name: name, Status: StatusDone, Detail: path + " (already current)"}
		default:
			return Step{
				Name:   name,
				Status: StatusSkip,
				Detail: fmt.Sprintf("%s exists and differs from this build's version — left as it is", path),
				Fix:    "re-run with --force to replace it, after checking you did not write it yourself",
			}
		}
	}

	if err := env.MkdirAll(dir); err != nil {
		return Step{Name: name, Status: StatusFail, Detail: fmt.Sprintf("could not create %s: %v", dir, err)}
	}
	if err := env.WriteFile(path, integration.ReviewBranchCommand); err != nil {
		return Step{Name: name, Status: StatusFail, Detail: fmt.Sprintf("could not write %s: %v", path, err)}
	}
	return Step{Name: name, Status: StatusDone, Detail: path}
}

// Report writes the steps and returns whether everything needed
// succeeded. Skips do not fail the run: they describe a machine that is
// set up as far as it can be.
func Report(out io.Writer, steps []Step) (ok bool) {
	ok = true
	fmt.Fprintln(out, "Installing Engineering OS for Claude Code")
	fmt.Fprintln(out)
	for _, s := range steps {
		if s.Status == StatusFail {
			ok = false
		}
		fmt.Fprintf(out, "  %s  %s\n", marker(s.Status), s.Name)
		if s.Detail != "" {
			fmt.Fprintln(out, indentBy(s.Detail, "      "))
		}
		if s.Fix != "" {
			fmt.Fprintln(out, indentBy("→ "+s.Fix, "      "))
		}
		fmt.Fprintln(out)
	}
	if ok {
		fmt.Fprintln(out, "Open Claude Code in any repository and type /review-branch.")
		fmt.Fprintln(out, "Run `eng doctor` there to check the whole chain end to end.")
	} else {
		fmt.Fprintln(out, "Fix the ✘ above and run this again. It is safe to re-run.")
	}
	return ok
}

func marker(s Status) string {
	switch s {
	case StatusDone:
		return "✔"
	case StatusSkip:
		return "!"
	default:
		return "✘"
	}
}

// commandTimeout bounds every external command, for the same reason
// doctor bounds its own: a setup command that hangs gives the developer
// no way to tell which step it stopped on.
const commandTimeout = 30 * time.Second

// System returns an Env wired to the real machine.
func System(self, workingDir, workspaceFlag string) Env {
	return Env{
		Self:          self,
		WorkingDir:    workingDir,
		WorkspaceFlag: workspaceFlag,
		Home:          os.Getenv("HOME"),
		LookPath:      exec.LookPath,
		Run:           runCommand,
		MkdirAll:      func(dir string) error { return os.MkdirAll(dir, 0o755) },
		ReadFile: func(path string) (string, error) {
			b, err := os.ReadFile(path)
			return string(b), err
		},
		WriteFile: func(path, content string) error {
			return os.WriteFile(path, []byte(content), 0o644)
		},
		Exists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	}
}

// runCommand returns combined output whether or not the command
// succeeded: `claude mcp add`'s own failure message is the diagnosis,
// and discarding it would leave this reporting "it failed" and nothing
// a developer could act on.
func runCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func trimBlank(s string) string { return strings.TrimSpace(s) }

func indentBy(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
