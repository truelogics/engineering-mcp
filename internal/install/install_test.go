package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truelogics/engineering-mcp/integration"
)

// fake is an Env recording what install did to the machine, so the
// tests assert on the change rather than on the wording of the report.
type fake struct {
	env      Env
	commands [][]string
	files    map[string]string
	dirs     []string
	// failAdd makes `claude mcp add` fail, the way it does when the
	// registration is malformed.
	failAdd bool
	// noClaude removes the claude CLI from $PATH.
	noClaude bool
	// registered is what `claude mcp get` reports, empty for none.
	registered string
}

func newFake(t *testing.T, workspaceDir string) *fake {
	t.Helper()
	home := t.TempDir()
	f := &fake{files: map[string]string{}}
	f.env = Env{
		Self:          "/usr/local/bin/engineering-mcp",
		WorkingDir:    workspaceDir,
		WorkspaceFlag: workspaceDir,
		Home:          home,
		LookPath: func(file string) (string, error) {
			if file == "claude" && f.noClaude {
				return "", fmt.Errorf("not found")
			}
			return "/usr/local/bin/" + file, nil
		},
		Run: func(_ context.Context, _, name string, args ...string) (string, error) {
			f.commands = append(f.commands, append([]string{name}, args...))
			switch {
			case len(args) > 1 && args[1] == "get":
				if f.registered == "" {
					return "", fmt.Errorf("no such server")
				}
				return "engineering:\n  Command: " + f.registered + "\n", nil
			case len(args) > 1 && args[1] == "add" && f.failAdd:
				return "error: could not add server", fmt.Errorf("exit status 1")
			}
			return "Added server engineering", nil
		},
		MkdirAll: func(dir string) error { f.dirs = append(f.dirs, dir); return nil },
		ReadFile: func(path string) (string, error) {
			content, ok := f.files[path]
			if !ok {
				return "", os.ErrNotExist
			}
			return content, nil
		},
		WriteFile: func(path, content string) error { f.files[path] = content; return nil },
		Exists:    func(path string) bool { _, ok := f.files[path]; return ok },
	}
	return f
}

func (f *fake) ran(sub ...string) bool {
	for _, cmd := range f.commands {
		joined := strings.Join(cmd, " ")
		all := true
		for _, s := range sub {
			if !strings.Contains(joined, s) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func (f *fake) commandPath() string {
	return filepath.Join(f.env.Home, ".claude", "commands", integration.ReviewBranchFilename)
}

func step(t *testing.T, steps []Step, name string) Step {
	t.Helper()
	for _, s := range steps {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no step named %q in %v", name, steps)
	return Step{}
}

// indexedWorkspace makes dir resolvable by workspace.Resolve, which
// looks for .eng/memory.db. Nothing reads the file, so an empty one is
// enough to test what install does with the path.
func indexedWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".eng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".eng", "memory.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallRegistersAndWritesTheCommand(t *testing.T) {
	ws := indexedWorkspace(t)
	f := newFake(t, ws)

	steps := Run(context.Background(), f.env, false)
	if !Report(&strings.Builder{}, steps) {
		t.Fatalf("Report said the install failed: %+v", steps)
	}

	if !f.ran("mcp", "add", "engineering", "--scope", "user") {
		t.Errorf("did not register at user scope; ran %v", f.commands)
	}
	if !f.ran("ENGINEERING_WORKSPACE=" + ws) {
		t.Errorf("registration did not carry the workspace; ran %v", f.commands)
	}
	if !f.ran(f.env.Self) {
		t.Errorf("registration did not name this binary absolutely; ran %v", f.commands)
	}
	if got := f.files[f.commandPath()]; got != integration.ReviewBranchCommand {
		t.Errorf("/review-branch was not installed at %s", f.commandPath())
	}
}

// The command file is what makes the tools reachable at all on a machine
// with several MCP servers — it must be embedded in the binary, not read
// from a clone, because a `go install` user has no clone.
func TestReviewBranchCommandIsEmbedded(t *testing.T) {
	if !strings.Contains(integration.ReviewBranchCommand, "mcp__engineering__find_engineering_rules") {
		t.Fatal("the embedded command does not name the MCP tools, which is the whole reason it exists")
	}
}

// `claude mcp add` refuses a name that already exists. Without the
// remove first, the command whose job is to repoint an installation at a
// rebuilt binary would fail on every machine that already had one.
func TestInstallReplacesAnExistingRegistration(t *testing.T) {
	ws := indexedWorkspace(t)
	f := newFake(t, ws)
	f.registered = "/old/path/engineering-mcp"

	steps := Run(context.Background(), f.env, false)

	if !f.ran("mcp", "remove", "engineering") {
		t.Errorf("did not remove the old registration; ran %v", f.commands)
	}
	s := step(t, steps, "Claude Code registration")
	if !strings.Contains(s.Detail, "/old/path/engineering-mcp") {
		t.Errorf("did not say what it replaced: %q", s.Detail)
	}
}

// A developer who edited the command has customised how their reviews
// work. Replacing that silently is the kind of overwrite noticed three
// reviews later.
func TestInstallDoesNotOverwriteAnEditedCommand(t *testing.T) {
	ws := indexedWorkspace(t)
	f := newFake(t, ws)
	mine := "---\ndescription: my own review\n---\n"
	f.files[f.commandPath()] = mine

	steps := Run(context.Background(), f.env, false)
	if f.files[f.commandPath()] != mine {
		t.Fatal("overwrote a command the developer had edited")
	}
	s := step(t, steps, "/review-branch command")
	if s.Status != StatusSkip || s.Fix == "" {
		t.Errorf("want a skip with a way out, got %+v", s)
	}

	// --force is the way out, and it must work.
	steps = Run(context.Background(), f.env, true)
	if f.files[f.commandPath()] != integration.ReviewBranchCommand {
		t.Fatal("--force did not replace the command")
	}
	if s := step(t, steps, "/review-branch command"); s.Status != StatusDone {
		t.Errorf("want done after --force, got %+v", s)
	}
}

// An unchanged file is not a conflict. Re-running install is normal, and
// reporting a skip every time would teach the reader to ignore the step.
func TestInstallRecognisesItsOwnCommandFile(t *testing.T) {
	ws := indexedWorkspace(t)
	f := newFake(t, ws)
	f.files[f.commandPath()] = integration.ReviewBranchCommand

	s := step(t, Run(context.Background(), f.env, false), "/review-branch command")
	if s.Status != StatusDone || !strings.Contains(s.Detail, "already current") {
		t.Errorf("want done/already current, got %+v", s)
	}
}

// Registering a server that exits at startup gives the developer a
// Claude Code entry that fails every session, while `claude mcp list`
// reports it as configured. Not registering is the honest outcome.
func TestInstallRefusesWithoutAWorkspace(t *testing.T) {
	f := newFake(t, t.TempDir()) // no .eng/memory.db anywhere
	f.env.WorkspaceFlag = ""
	t.Setenv("ENGINEERING_WORKSPACE", "")

	steps := Run(context.Background(), f.env, false)
	if Report(&strings.Builder{}, steps) {
		t.Fatal("reported success with no workspace")
	}
	if f.ran("mcp", "add") {
		t.Errorf("registered a server that cannot start; ran %v", f.commands)
	}
	if s := step(t, steps, "Workspace"); s.Fix == "" {
		t.Error("failed without telling the developer how to fix it")
	}
	// The command file is still worth installing: it is independent of
	// the workspace, and the developer will run install again.
	if s := step(t, steps, "/review-branch command"); s.Status != StatusDone {
		t.Errorf("want the command installed anyway, got %+v", s)
	}
}

// A machine without Claude Code is a legitimate machine — the server
// speaks plain MCP. Skipping is not failing, but it must say what to do
// by hand.
func TestInstallSkipsWhenClaudeCodeIsAbsent(t *testing.T) {
	ws := indexedWorkspace(t)
	f := newFake(t, ws)
	f.noClaude = true

	steps := Run(context.Background(), f.env, false)
	if !Report(&strings.Builder{}, steps) {
		t.Fatal("a machine without Claude Code should not be a failed install")
	}
	s := step(t, steps, "Claude Code registration")
	if s.Status != StatusSkip {
		t.Errorf("want skip, got %+v", s)
	}
	if !strings.Contains(s.Fix, f.env.Self) || !strings.Contains(s.Fix, ws) {
		t.Errorf("the manual instructions must carry both paths: %q", s.Fix)
	}
}

// `claude mcp add`'s own message is the diagnosis. Reporting "it failed"
// and discarding the output leaves nothing to act on.
func TestInstallReportsWhatClaudeSaid(t *testing.T) {
	ws := indexedWorkspace(t)
	f := newFake(t, ws)
	f.failAdd = true

	steps := Run(context.Background(), f.env, false)
	if Report(&strings.Builder{}, steps) {
		t.Fatal("reported success after claude mcp add failed")
	}
	if s := step(t, steps, "Claude Code registration"); !strings.Contains(s.Detail, "could not add server") {
		t.Errorf("swallowed the CLI's message: %q", s.Detail)
	}
}
