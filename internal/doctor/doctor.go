// Package doctor answers, in one command, the questions a developer asks
// when Engineering OS does not appear to be working.
//
// It adds no capability. Every check runs something that already exists —
// the workspace resolver, the `eng` CLI, the `claude` CLI, this server's
// own protocol handshake — and reports what happened. That is deliberate:
// a diagnostic that reimplements what it is diagnosing can pass while the
// real thing fails.
//
// The checks are ordered the way the system is layered, so the first
// failure is the cause and the ones after it are symptoms.
package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/truelogics/ai-memory/pkg/memory"
	"github.com/truelogics/engineering-mcp/internal/workspace"
)

// Status is a check's outcome. Warn exists because most of what goes
// wrong here is survivable and specific — a workspace with no rulebook
// answers every question with "no rule governs this", which is not a
// crash and is not correct either.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Check is one question and its answer. Fix is the command to run, and is
// empty only when there is nothing to do.
type Check struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// Knowledge is the part of *memory.Memory the retrieval probe uses.
// Narrow so the probe can be tested without a database on disk.
type Knowledge interface {
	ContextFor(ctx context.Context, task string, opts memory.ContextOptions) (memory.ContextPackage, error)
	Close() error
}

// Env is every part of the outside world doctor touches, injected so the
// checks can be tested on a machine with no workspace, no $PATH and no
// Claude Code.
type Env struct {
	// Self is the absolute path of the running binary — what Claude Code
	// would have to be pointed at.
	Self string
	// WorkingDir is the directory the developer ran doctor in, which is
	// also the directory whose workspace resolution is being tested.
	WorkingDir string
	// WorkspaceFlag mirrors the server's --workspace, so doctor diagnoses
	// the same resolution the server would perform.
	WorkspaceFlag string
	// Home locates the user-scope Claude Code command directory.
	Home string

	// LookPath resolves a command name on $PATH (exec.LookPath).
	LookPath func(file string) (string, error)
	// Run executes a command in dir and returns its combined output. The
	// output is wanted even when err is non-nil: a CLI's failure message
	// is usually the diagnosis.
	Run func(ctx context.Context, dir, name string, args ...string) (string, error)
	// Handshake speaks MCP to the binary at self, started in dir, and
	// returns the tool names it advertises.
	Handshake func(ctx context.Context, self, dir string) ([]string, error)
	// OpenKnowledge opens an indexed workspace for the retrieval probe.
	OpenKnowledge func(dir string) (Knowledge, error)
	// Exists reports whether a path is present.
	Exists func(path string) bool
}

// Run performs every check in order and returns the results. It never
// returns an error: a check that could not be performed is a finding, not
// an abort, and the checks after it may still be informative.
func Run(ctx context.Context, env Env) []Check {
	var checks []Check
	add := func(c Check) { checks = append(checks, c) }

	add(checkServerBinary(env))
	add(checkEngCLI(ctx, env))

	ws, wsCheck := checkWorkspace(env)
	add(wsCheck)

	add(checkIndex(ctx, env, ws))
	add(checkKnowledge(ctx, env, ws))
	add(checkClaudeCode(ctx, env))
	add(checkHandshake(ctx, env))
	add(checkReviewCommand(env))

	return checks
}

// checkServerBinary answers "is Engineering MCP installed?" — and the
// sharper question behind it, which is whether the binary Claude Code
// would launch is the one that was just built. A stale copy on $PATH
// serves old tools and old bugs while the developer edits code that never
// runs.
func checkServerBinary(env Env) Check {
	const name = "engineering-mcp"

	onPath, err := env.LookPath(name)
	if err != nil {
		return Check{
			Name:   "Engineering MCP",
			Status: StatusWarn,
			Detail: fmt.Sprintf("running %s, but %q is not on $PATH", env.Self, name),
			Fix:    fmt.Sprintf("install it where Claude Code can find it:\n    go build -o ~/.local/bin/%s ./cmd/%s", name, name),
		}
	}

	if !samePath(onPath, env.Self) {
		return Check{
			Name:   "Engineering MCP",
			Status: StatusWarn,
			Detail: fmt.Sprintf("$PATH resolves %s to %s, which is not the binary running this check (%s)", name, onPath, env.Self),
			Fix:    "two builds are installed. Rebuild over the one on $PATH, or Claude Code will keep launching the other.",
		}
	}

	return Check{Name: "Engineering MCP", Status: StatusOK, Detail: onPath}
}

// checkEngCLI answers "is AI Memory available?". The library is linked
// into this binary and cannot be missing; the CLI can, and without it
// nothing can be indexed, so that is the question worth asking.
func checkEngCLI(ctx context.Context, env Env) Check {
	path, err := env.LookPath("eng")
	if err != nil {
		return Check{
			Name:   "AI Memory (eng)",
			Status: StatusFail,
			Detail: "not on $PATH — nothing can be indexed without it",
			Fix:    "go build -o ~/.local/bin/eng ../ai-memory/cmd/eng",
		}
	}

	out, err := env.Run(ctx, env.WorkingDir, path, "version")
	if err != nil {
		return Check{
			Name:   "AI Memory (eng)",
			Status: StatusFail,
			Detail: fmt.Sprintf("%s could not be run: %v\n%s", path, err, indent(out)),
			Fix:    "rebuild it: go build -o ~/.local/bin/eng ../ai-memory/cmd/eng",
		}
	}
	return Check{Name: "AI Memory (eng)", Status: StatusOK, Detail: fmt.Sprintf("%s (%s)", firstLine(out), path)}
}

// checkWorkspace answers "is the workspace valid?" by running the server's
// own resolver against the current directory — the same call, in the same
// order, that the server makes at startup. Its error text is already
// written for a developer, so it is passed through rather than restated.
func checkWorkspace(env Env) (workspace.Resolved, Check) {
	ws, err := workspace.Resolve(env.WorkspaceFlag, env.WorkingDir)
	if err != nil {
		return ws, Check{
			Name:   "Workspace",
			Status: StatusFail,
			Detail: err.Error(),
		}
	}
	return ws, Check{
		Name:   "Workspace",
		Status: StatusOK,
		Detail: fmt.Sprintf("%s (%s)", ws.Dir, ws.Source),
	}
}

// checkIndex answers "is the repository indexed?" — meaning this
// repository, not some repository. A workspace resolving successfully
// says nothing about whether the code being reviewed is in it, and the
// failure that produces is silent: retrieval simply returns other
// repositories' documents.
func checkIndex(ctx context.Context, env Env, ws workspace.Resolved) Check {
	const name = "Workspace index"
	if ws.Dir == "" {
		return Check{Name: name, Status: StatusFail, Detail: "skipped: no workspace resolved"}
	}
	eng, err := env.LookPath("eng")
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: "skipped: eng is not on $PATH"}
	}

	// Reported verbatim rather than parsed. `eng workspace list` is the
	// answer to this question and already prints it well; re-deriving the
	// inventory here would be a second implementation to drift.
	out, err := env.Run(ctx, env.WorkingDir, eng, "workspace", "list", ws.Dir)
	if err != nil {
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: fmt.Sprintf("eng workspace list failed: %v\n%s", err, indent(out)),
		}
	}

	repo := repositoryRoot(ctx, env)
	if repo != "" && !mentionsPath(out, repo) {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: fmt.Sprintf("%s is not attached to this workspace — reviews here will retrieve other repositories' documents\n%s", repo, trimBlank(out)),
			Fix:    fmt.Sprintf("eng workspace attach %s", repo),
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: trimBlank(out)}
}

// checkKnowledge asks the question a review asks: given real files from
// this repository, does the rulebook have anything to say? A workspace
// holding an application and no engineering repository answers "no
// engineering rule governs these files" with complete confidence, and
// that answer is indistinguishable from a correct one.
//
// The probe uses files taken from the repository rather than invented
// ones, because rules are selected by path scope — a made-up path would
// measure nothing.
func checkKnowledge(ctx context.Context, env Env, ws workspace.Resolved) Check {
	const name = "Engineering knowledge"
	if ws.Dir == "" {
		return Check{Name: name, Status: StatusFail, Detail: "skipped: no workspace resolved"}
	}

	paths := sampleFiles(ctx, env)
	if len(paths) == 0 {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: "skipped: no tracked files found here to probe with (not a git repository?)",
		}
	}

	k, err := env.OpenKnowledge(ws.Dir)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: fmt.Sprintf("could not open %s: %v", ws.Dir, err)}
	}
	defer k.Close()

	pkg, err := k.ContextFor(ctx, "review the changes on this branch", memory.ContextOptions{ChangedPaths: paths})
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: fmt.Sprintf("retrieval failed: %v", err)}
	}

	sampled := fmt.Sprintf("%d rule(s), %d ADR(s), %d related document(s) for %d sampled file(s)",
		len(pkg.Rules), len(pkg.ADRs), len(pkg.RelevantFiles), len(paths))

	if len(pkg.Rules) == 0 {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: sampled + " — no rule governs this repository's files",
			Fix: "if that is a surprise, this workspace probably holds no rulebook:\n" +
				"    eng workspace attach /path/to/your/engineering-rules-repo",
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: sampled}
}

// checkClaudeCode answers "is Claude Code connected?" using Claude Code's
// own report, including the trap where a server is registered and healthy
// but points at a different binary than the one being developed.
func checkClaudeCode(ctx context.Context, env Env) Check {
	const name = "Claude Code registration"
	claude, err := env.LookPath("claude")
	if err != nil {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: "the claude CLI is not on $PATH, so registration cannot be checked from here",
		}
	}

	out, err := env.Run(ctx, env.WorkingDir, claude, "mcp", "get", "engineering")
	if err != nil {
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: "no MCP server named 'engineering' is registered",
			Fix: "claude mcp add engineering --scope user \\\n" +
				"      -e ENGINEERING_WORKSPACE=/path/to/your/workspace-root \\\n" +
				"      -- " + env.Self,
		}
	}

	switch {
	case strings.Contains(out, "Failed to connect"), strings.Contains(out, "✘"):
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: trimBlank(out),
			Fix:    "the command above is registered but does not start. Run it directly to see why.",
		}
	case registeredCommand(out) != "" && !samePath(registeredCommand(out), env.Self):
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: fmt.Sprintf("Claude Code launches %s, not %s", registeredCommand(out), env.Self),
			Fix:    "re-register, or rebuild over the registered path — otherwise your changes never reach Claude Code.",
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: trimBlank(out)}
}

// checkHandshake answers "is the MCP server reachable?" the only way that
// proves it: by being a client. It starts the same binary in the same
// directory Claude Code would, and reads the tool list off the wire. This
// is the check that catches anything written to stdout, which corrupts
// the protocol without corrupting anything visible.
func checkHandshake(ctx context.Context, env Env) Check {
	const name = "MCP handshake"
	toolNames, err := env.Handshake(ctx, env.Self, env.WorkingDir)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: err.Error()}
	}
	if len(toolNames) == 0 {
		return Check{Name: name, Status: StatusFail, Detail: "the server answered but advertised no tools"}
	}
	return Check{
		Name:   name,
		Status: StatusOK,
		Detail: fmt.Sprintf("%d tools: %s", len(toolNames), strings.Join(toolNames, ", ")),
	}
}

// checkReviewCommand is not one of the six questions, and is here because
// of what docs/reports/TOOL_DISCOVERY_EXPERIMENT.md measured: on a machine
// with many MCP servers installed, tool schemas are deferred and a tool is
// uninvokable until something names it. The /review-branch command names
// these four. Without it, every check above can pass and Claude Code will
// still never call the server.
func checkReviewCommand(env Env) Check {
	const name = "/review-branch command"
	candidates := []string{
		filepath.Join(env.WorkingDir, ".claude", "commands", "review-branch.md"),
	}
	if env.Home != "" {
		candidates = append(candidates, filepath.Join(env.Home, ".claude", "commands", "review-branch.md"))
	}
	for _, c := range candidates {
		if env.Exists(c) {
			return Check{Name: name, Status: StatusOK, Detail: c}
		}
	}
	return Check{
		Name:   name,
		Status: StatusWarn,
		Detail: "not installed — on a machine with several MCP servers, tool schemas are deferred and these tools are never reached without it",
		Fix:    "cp integration/claude-code/review-branch.md ~/.claude/commands/",
	}
}

// Report writes the checks and returns whether anything failed. Warnings
// do not fail: they describe a system that works and may be answering the
// wrong question, which is for the developer to judge.
func Report(out io.Writer, checks []Check) (ok bool) {
	ok = true
	fmt.Fprintln(out, "Engineering OS doctor")
	fmt.Fprintln(out)
	for _, c := range checks {
		if c.Status == StatusFail {
			ok = false
		}
		fmt.Fprintf(out, "  %s  %s\n", marker(c.Status), c.Name)
		if c.Detail != "" {
			fmt.Fprintln(out, indentBy(c.Detail, "      "))
		}
		if c.Fix != "" {
			fmt.Fprintln(out, indentBy("→ "+c.Fix, "      "))
		}
		fmt.Fprintln(out)
	}
	if ok {
		fmt.Fprintln(out, "No failures.")
	} else {
		fmt.Fprintln(out, "Fix the first ✘ above; the checks below it are usually its symptoms.")
	}
	return ok
}

func marker(s Status) string {
	switch s {
	case StatusOK:
		return "✔"
	case StatusWarn:
		return "!"
	default:
		return "✘"
	}
}

// repositoryRoot is the git top level of the working directory, or empty
// when there is no repository — running outside one is legitimate and not
// worth an error.
func repositoryRoot(ctx context.Context, env Env) string {
	git, err := env.LookPath("git")
	if err != nil {
		return ""
	}
	out, err := env.Run(ctx, env.WorkingDir, git, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstLine(out))
}

// sampleFiles takes a handful of the repository's tracked files to probe
// retrieval with. Sorted-first rather than random, so two runs of doctor
// on an unchanged repository report the same thing.
func sampleFiles(ctx context.Context, env Env) []string {
	git, err := env.LookPath("git")
	if err != nil {
		return nil
	}
	out, err := env.Run(ctx, env.WorkingDir, git, "ls-files")
	if err != nil {
		return nil
	}
	const sample = 10
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		paths = append(paths, p)
		if len(paths) == sample {
			break
		}
	}
	return paths
}

// registeredCommand pulls the launched binary out of `claude mcp get`
// output. Empty when the line is absent, which callers treat as "cannot
// tell" rather than "mismatch".
func registeredCommand(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "Command:"); found {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// mentionsPath reports whether out names dir. Whole-field comparison, not
// substring: /work/api is not indexed merely because /work/api-gateway is.
func mentionsPath(out, dir string) bool {
	for _, field := range strings.Fields(out) {
		if samePath(field, dir) {
			return true
		}
	}
	return false
}

// samePath compares two filesystem paths after resolving symlinks, so
// /tmp and /private/tmp — or a Homebrew shim and its target — are not
// reported as two different installations.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ra == rb
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func indent(s string) string { return indentBy(trimBlank(s), "    ") }

// trimBlank strips leading and trailing blank lines and any trailing
// spaces a subprocess left behind, so Report's own indentation does not
// produce lines made entirely of whitespace.
func trimBlank(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func indentBy(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// Exists is the default Env.Exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
