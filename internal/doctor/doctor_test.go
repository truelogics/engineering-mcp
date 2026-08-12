package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/truelogics/engineering-kernel/pkg/memory"
)

// fakeKnowledge is a workspace that returns whatever the test says it
// holds, so the retrieval probe can be exercised without a database.
type fakeKnowledge struct {
	pkg    memory.ContextPackage
	err    error
	closed bool
}

func (f *fakeKnowledge) ContextFor(context.Context, string, memory.ContextOptions) (memory.ContextPackage, error) {
	return f.pkg, f.err
}
func (f *fakeKnowledge) Close() error { f.closed = true; return nil }

// writeCommand installs review-branch.md under root's .claude/commands.
func writeCommand(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "review-branch.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// indexWorkspace makes dir look like an indexed workspace, which is the
// one thing the fakes cannot supply: workspace.Resolve reads the real
// filesystem.
func indexWorkspace(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".eng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".eng", "memory.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testEnv is a machine where everything works. Tests break one thing.
func testEnv(t *testing.T) (Env, *fakeKnowledge) {
	t.Helper()
	dir := t.TempDir()
	// Distinct from WorkingDir. Sharing one directory made the
	// project-scope and user-scope lookups for /review-branch the same
	// path, so dropping either one kept every test green — and the
	// user-scope one is what INSTALL.md actually tells people to use.
	home := t.TempDir()

	self := filepath.Join(dir, "engineering-mcp")
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCommand(t, home)

	knowledge := &fakeKnowledge{pkg: memory.ContextPackage{
		Rules: []memory.FileContext{{Repository: "engineering", Path: "rules/a.md"}},
	}}

	return Env{
		Self:       self,
		WorkingDir: dir,
		Home:       home,
		LookPath: func(file string) (string, error) {
			switch file {
			case "engineering-mcp":
				return self, nil
			case "eng", "git", "claude":
				return "/usr/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, _, name string, args ...string) (string, error) {
			switch {
			case strings.HasSuffix(name, "eng") && args[0] == "version":
				return "eng version 0.1.0-dev\n", nil
			case strings.HasSuffix(name, "eng") && args[0] == "workspace":
				return "Workspace: " + dir + "/.eng/memory.db\n\n  engineering  39 documents  " + dir + "\n", nil
			case strings.HasSuffix(name, "git") && args[0] == "rev-parse":
				return dir + "\n", nil
			case strings.HasSuffix(name, "git") && args[0] == "ls-files":
				return "README.md\ninternal/a.go\n", nil
			case strings.HasSuffix(name, "claude"):
				return "engineering:\n  Scope: User config\n  Status: ✔ Connected\n  Command: " + self + "\n", nil
			}
			return "", fmt.Errorf("unexpected command %s %v", name, args)
		},
		Handshake: func(context.Context, string, string, string) ([]string, error) {
			return []string{"search_memory", "get_context", "find_engineering_rules", "verify_evidence"}, nil
		},
		OpenKnowledge: func(string) (Knowledge, error) { return knowledge, nil },
		Exists:        Exists,
	}, knowledge
}

func byName(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no check named %q in %v", name, checks)
	return Check{}
}

func TestHealthyMachinePassesEveryCheck(t *testing.T) {
	env, _ := testEnv(t)
	// The workspace check is the one thing the fakes cannot supply: it
	// reads the real filesystem, and t.TempDir() holds no .eng/memory.db.
	indexWorkspace(t, env.WorkingDir)

	checks := Run(context.Background(), env)
	for _, c := range checks {
		if c.Status != StatusOK {
			t.Errorf("%s: %s\n%s", c.Name, c.Status, c.Detail)
		}
	}
	if ok := Report(io.Discard, checks); !ok {
		t.Error("Report said a healthy machine failed")
	}
}

// TestMissingWorkspaceFailsAndSkipsWhatDependsOnIt: the checks are
// ordered so the first failure is the cause. A missing workspace must not
// produce three more failures that read like independent problems.
func TestMissingWorkspaceFailsAndSkipsWhatDependsOnIt(t *testing.T) {
	env, _ := testEnv(t)
	t.Setenv("ENGINEERING_WORKSPACE", "")

	checks := Run(context.Background(), env)
	if got := byName(t, checks, "Workspace").Status; got != StatusFail {
		t.Errorf("Workspace status = %q, want fail", got)
	}
	// The handshake belongs in this list too: with no workspace the child
	// server exits on startup, and running it anyway reported the same
	// error a second time as an independent fourth failure.
	for _, name := range []string{"Workspace index", "Engineering knowledge", "MCP handshake"} {
		c := byName(t, checks, name)
		if !strings.HasPrefix(c.Detail, "skipped:") {
			t.Errorf("%s should say it was skipped, said: %s", name, c.Detail)
		}
	}
}

// TestEngMissingIsFatal: everything downstream of indexing depends on the
// CLI, and no amount of correct configuration substitutes for it.
func TestEngMissingIsFatal(t *testing.T) {
	env, _ := testEnv(t)
	inner := env.LookPath
	env.LookPath = func(file string) (string, error) {
		if file == "eng" {
			return "", errors.New("not found")
		}
		return inner(file)
	}

	c := byName(t, Run(context.Background(), env), "Engineering Kernel (eng)")
	if c.Status != StatusFail {
		t.Errorf("status = %q, want fail", c.Status)
	}
	if !strings.Contains(c.Fix, "cmd/eng") {
		t.Errorf("the fix should say how to build it, said: %q", c.Fix)
	}
}

// TestRegisteredBinaryMismatchIsReported is the trap this check exists
// for: everything is registered, connected and green, and Claude Code is
// launching a binary that predates the developer's changes.
func TestRegisteredBinaryMismatchIsReported(t *testing.T) {
	env, _ := testEnv(t)
	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "claude") {
			return "engineering:\n  Status: ✔ Connected\n  Command: /somewhere/else/engineering-mcp\n", nil
		}
		return inner(ctx, dir, name, args...)
	}

	c := byName(t, Run(context.Background(), env), "Claude Code registration")
	if c.Status != StatusWarn {
		t.Errorf("status = %q, want warn", c.Status)
	}
	if !strings.Contains(c.Detail, "/somewhere/else/engineering-mcp") {
		t.Errorf("the detail should name both binaries, said: %q", c.Detail)
	}
}

func TestUnregisteredServerFailsWithTheCommandToRun(t *testing.T) {
	env, _ := testEnv(t)
	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "claude") {
			return "No MCP server found with name: engineering\n", errors.New("exit status 1")
		}
		return inner(ctx, dir, name, args...)
	}

	c := byName(t, Run(context.Background(), env), "Claude Code registration")
	if c.Status != StatusFail {
		t.Errorf("status = %q, want fail", c.Status)
	}
	if !strings.Contains(c.Fix, "claude mcp add engineering") || !strings.Contains(c.Fix, env.Self) {
		t.Errorf("the fix should be the exact command, with this binary's path: %q", c.Fix)
	}
	// The CLI fails for reasons other than an unregistered server, so its
	// own message is reported rather than a guess at what it meant.
	if !strings.Contains(c.Detail, "No MCP server found with name") {
		t.Errorf("the detail should be the CLI's own message, was: %q", c.Detail)
	}
}

// TestSampleFilesAreRepositoryRelative: rules are selected by
// applies_to globs against repository-relative paths. `git ls-files` run
// from a subdirectory answers relative to that subdirectory, so without
// --full-name every glob misses and doctor reports "no rule governs this
// repository" for a repository that is governed perfectly well.
func TestSampleFilesAreRepositoryRelative(t *testing.T) {
	env, _ := testEnv(t)
	var lsArgs []string
	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "git") && args[0] == "ls-files" {
			lsArgs = args
		}
		return inner(ctx, dir, name, args...)
	}

	sampleFiles(context.Background(), env)
	if !slices.Contains(lsArgs, "--full-name") {
		t.Errorf("git ls-files %v — want --full-name", lsArgs)
	}
}

// TestNoRulesWarnsRatherThanPasses is the failure the whole platform is
// most vulnerable to: a workspace that resolves, opens and answers, and
// holds no rulebook. Every mechanical check passes and every review is
// told nothing governs anything.
func TestNoRulesWarnsRatherThanPasses(t *testing.T) {
	env, knowledge := testEnv(t)
	knowledge.pkg = memory.ContextPackage{}
	indexWorkspace(t, env.WorkingDir)

	c := byName(t, Run(context.Background(), env), "Engineering knowledge")
	if c.Status != StatusWarn {
		t.Errorf("status = %q, want warn — a rulebook-less workspace is not a healthy one", c.Status)
	}
	if !strings.Contains(c.Fix, "workspace attach") {
		t.Errorf("the fix should be attaching a rules repository, said: %q", c.Fix)
	}
	if !knowledge.closed {
		t.Error("the probe leaked its workspace handle")
	}
}

// TestThisRepositoryNotAttachedIsCaught: a workspace can be perfectly
// healthy and contain everything except the code being reviewed, which
// retrieval cannot report because it has other repositories to answer
// from.
func TestThisRepositoryNotAttachedIsCaught(t *testing.T) {
	env, _ := testEnv(t)
	indexWorkspace(t, env.WorkingDir)
	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "eng") && args[0] == "workspace" {
			return "  engineering  39 documents  /elsewhere/engineering\n", nil
		}
		return inner(ctx, dir, name, args...)
	}

	c := byName(t, Run(context.Background(), env), "Workspace index")
	if c.Status != StatusWarn {
		t.Errorf("status = %q, want warn", c.Status)
	}
	if !strings.Contains(c.Fix, "eng workspace attach "+env.WorkingDir) {
		t.Errorf("the fix should attach this repository, said: %q", c.Fix)
	}
}

// TestMentionsPathComparesWholePaths: substring matching would report
// /work/api as attached because /work/api-gateway is.
func TestMentionsPathComparesWholePaths(t *testing.T) {
	listing := "  api-gateway  12 documents  /work/api-gateway\n"
	if mentionsPath(listing, "/work/api") {
		t.Error("/work/api must not match /work/api-gateway")
	}
	if !mentionsPath(listing, "/work/api-gateway") {
		t.Error("the path that is actually listed should match")
	}
}

func TestReviewCommandMissingWarnsWithTheReason(t *testing.T) {
	env, _ := testEnv(t)
	if err := os.Remove(filepath.Join(env.Home, ".claude", "commands", "review-branch.md")); err != nil {
		t.Fatal(err)
	}

	c := byName(t, Run(context.Background(), env), "/review-branch command")
	if c.Status != StatusWarn {
		t.Errorf("status = %q, want warn", c.Status)
	}
	if !strings.Contains(c.Detail, "deferred") {
		t.Errorf("the detail should say why it matters, not just that it is absent: %q", c.Detail)
	}
}

// TestHandshakeFailureIsFatal: every other check can pass while the
// server is unreachable, because every other check reads configuration
// rather than speaking the protocol.
func TestHandshakeFailureIsFatal(t *testing.T) {
	env, _ := testEnv(t)
	env.Handshake = func(context.Context, string, string, string) ([]string, error) {
		return nil, errors.New("the server wrote a line that is not JSON-RPC")
	}

	checks := Run(context.Background(), env)
	if got := byName(t, checks, "MCP handshake").Status; got != StatusFail {
		t.Errorf("status = %q, want fail", got)
	}
	if Report(io.Discard, checks) {
		t.Error("Report said the machine was healthy with an unreachable server")
	}
}

// TestReadToolListRejectsStrayStdout is the exact corruption the
// handshake check exists to catch: a debug line printed to stdout leaves
// the tool registry correct and the transport unusable.
func TestReadToolListRejectsStrayStdout(t *testing.T) {
	_, err := readToolList(strings.NewReader("serving /some/workspace\n"))
	if err == nil {
		t.Fatal("want an error for a non-JSON line on stdout")
	}
	if !strings.Contains(err.Error(), "serving /some/workspace") {
		t.Errorf("the error should quote the offending line: %v", err)
	}
}

func TestReadToolListSkipsTheInitializeResponse(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search_memory"},{"name":"get_context"}]}}` + "\n"
	names, err := readToolList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "search_memory,get_context" {
		t.Errorf("names = %v", names)
	}
}

// TestWorkspaceComesFromTheRegistrationWhenTheShellHasNone is the
// blocking finding from Sprint 15's review of this file.
//
// INSTALL.md sets ENGINEERING_WORKSPACE inside `claude mcp add -e ...`,
// which puts it in the spawned server's environment and nowhere else. A
// developer who followed the documentation exactly, standing in an
// application that is not itself inside a workspace, got four ✘ one line
// below "Claude Code registration: ✔ Connected" — and advice that would
// have created the nested .eng/ the same document warns against.
func TestWorkspaceComesFromTheRegistrationWhenTheShellHasNone(t *testing.T) {
	env, _ := testEnv(t)
	t.Setenv("ENGINEERING_WORKSPACE", "")

	// The workspace is elsewhere; the working directory is not in one.
	elsewhere := t.TempDir()
	indexWorkspace(t, elsewhere)
	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "claude") {
			return "engineering:\n  Status: ✔ Connected\n  Command: " + env.Self +
				"\n  Environment:\n    ENGINEERING_WORKSPACE=" + elsewhere + "\n", nil
		}
		return inner(ctx, dir, name, args...)
	}

	checks := Run(context.Background(), env)
	c := byName(t, checks, "Workspace")
	if c.Status != StatusOK {
		t.Fatalf("status = %q, want ok — this is what Claude Code will serve:\n%s", c.Status, c.Detail)
	}
	if !strings.Contains(c.Detail, elsewhere) {
		t.Errorf("the detail should name the registered workspace: %q", c.Detail)
	}
	// eng has no os.Getenv call at all, so telling the reader to export
	// the variable would be advice that changes nothing.
	if strings.Contains(c.Fix, "export") {
		t.Errorf("eng does not read the environment; this advice does nothing: %q", c.Fix)
	}
	if !strings.Contains(c.Fix, "cd "+elsewhere) {
		t.Errorf("the reader should be sent to the workspace directory: %q", c.Fix)
	}
	if Report(io.Discard, checks) == false {
		t.Error("a correctly installed machine must not report failures")
	}
}

// TestRegisteredWorkspaceThatIsNotIndexedFailsSharply: registered but
// unusable has a different fix from unregistered, and the reader needs to
// be told which one they have.
func TestRegisteredWorkspaceThatIsNotIndexedFailsSharply(t *testing.T) {
	env, _ := testEnv(t)
	t.Setenv("ENGINEERING_WORKSPACE", "")
	empty := t.TempDir() // no .eng/

	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "claude") {
			return "engineering:\n  Command: " + env.Self +
				"\n  Environment:\n    ENGINEERING_WORKSPACE=" + empty + "\n", nil
		}
		return inner(ctx, dir, name, args...)
	}

	c := byName(t, Run(context.Background(), env), "Workspace")
	if c.Status != StatusFail {
		t.Errorf("status = %q, want fail", c.Status)
	}
	if !strings.Contains(c.Detail, empty) || !strings.Contains(c.Detail, "holds no index") {
		t.Errorf("the detail should say the registration points somewhere unindexed: %q", c.Detail)
	}
}

// TestMentionsPathSurvivesSpaces: strings.Fields shredded
// "/Users/x/My Projects/api" into two fragments, so an attached
// repository on an ordinary macOS path was reported as missing.
func TestMentionsPathSurvivesSpaces(t *testing.T) {
	listing := "  api  12 documents  /Users/x/My Projects/api\n"
	if !mentionsPath(listing, "/Users/x/My Projects/api") {
		t.Error("a path containing a space is still the path that is listed")
	}
	if mentionsPath(listing, "/Users/x/My Projects/other") {
		t.Error("a path that is not listed must not match")
	}
}

// TestMentionsPathRequiresAFieldBoundary: suffix matching must not make
// /work/api match a line ending in /work/my-api.
func TestMentionsPathRequiresAFieldBoundary(t *testing.T) {
	if mentionsPath("  x  1 documents  /work/my-api\n", "/work/api") {
		t.Error("/work/api must not match /work/my-api")
	}
	if mentionsPath("  x  1 documents  /work/api-gateway\n", "/work/api") {
		t.Error("/work/api must not match /work/api-gateway")
	}
}

// TestReviewCommandFoundAtEitherScope: both lookups must be live. With
// Home and WorkingDir sharing a directory, dropping either kept the suite
// green.
func TestReviewCommandFoundAtEitherScope(t *testing.T) {
	t.Run("user scope", func(t *testing.T) {
		env, _ := testEnv(t) // testEnv installs it under Home
		c := byName(t, Run(context.Background(), env), "/review-branch command")
		if c.Status != StatusOK || !strings.HasPrefix(c.Detail, env.Home) {
			t.Errorf("status %q detail %q — want the Home copy found", c.Status, c.Detail)
		}
	})

	t.Run("project scope", func(t *testing.T) {
		env, _ := testEnv(t)
		if err := os.RemoveAll(filepath.Join(env.Home, ".claude")); err != nil {
			t.Fatal(err)
		}
		want := writeCommand(t, env.WorkingDir)
		c := byName(t, Run(context.Background(), env), "/review-branch command")
		if c.Status != StatusOK || c.Detail != want {
			t.Errorf("status %q detail %q — want the project copy found at %s", c.Status, c.Detail, want)
		}
	})
}

// TestReadToolListReportsAnEmptyRegistry: a server that answers
// tools/list with no tools has a registry problem, not a transport
// problem. Skipping empty results made it fall through to EOF and report
// that the connection died, sending the reader to the wrong layer.
func TestReadToolListReportsAnEmptyRegistry(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}` + "\n"
	names, err := readToolList(strings.NewReader(in))
	if err != nil {
		t.Fatalf("an empty tool list is an answer, not a transport failure: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("names = %v", names)
	}

	env, _ := testEnv(t)
	indexWorkspace(t, env.WorkingDir)
	env.Handshake = func(context.Context, string, string, string) ([]string, error) { return nil, nil }
	c := byName(t, Run(context.Background(), env), "MCP handshake")
	if !strings.Contains(c.Detail, "advertised no tools") {
		t.Errorf("detail = %q, want it to name the registry rather than the connection", c.Detail)
	}
}

// TestSampleFilesSpreadAcrossTheRepository: git's sort order puts
// root-level markdown first, so taking the first ten probed this
// repository with its documentation and none of its Go — and a rulebook
// scoped to **/*.go would have been reported as having nothing to say.
func TestSampleFilesSpreadAcrossTheRepository(t *testing.T) {
	env, _ := testEnv(t)
	var listing strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&listing, "docs/%02d.md\n", i)
	}
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&listing, "internal/%02d.go\n", i)
	}
	inner := env.Run
	env.Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
		if strings.HasSuffix(name, "git") && args[0] == "ls-files" {
			return listing.String(), nil
		}
		return inner(ctx, dir, name, args...)
	}

	paths := sampleFiles(context.Background(), env)
	var goFiles int
	for _, p := range paths {
		if strings.HasSuffix(p, ".go") {
			goFiles++
		}
	}
	if goFiles == 0 {
		t.Errorf("sampled %v — every file is documentation, so a rule scoped to Go is never probed", paths)
	}
}

// TestFirstParagraphKeepsTheAnswerAndDropsTheAdvice pins the shape of
// `claude mcp get` output this depends on. Without it, a CLI format
// change could silently truncate the Command and Environment lines out of
// the registration detail — which is where the workspace fallback reads
// from — with nothing going red.
func TestFirstParagraphKeepsTheAnswerAndDropsTheAdvice(t *testing.T) {
	const real = `engineering:
  Scope: User config (available in all your projects)
  Status: ✔ Connected
  Type: stdio
  Command: /Users/x/.local/bin/engineering-mcp
  Args:
  Environment:
    ENGINEERING_WORKSPACE=/Users/x/engineering-os

To remove this server, run: claude mcp remove engineering -s user
`
	got := firstParagraph(real)
	for _, want := range []string{"Status:", "Command:", "ENGINEERING_WORKSPACE="} {
		if !strings.Contains(got, want) {
			t.Errorf("dropped %q, which the workspace fallback reads:\n%s", want, got)
		}
	}
	if strings.Contains(got, "To remove this server") {
		t.Errorf("kept advice for a different question:\n%s", got)
	}
}

// TestRegistrationParsesCommandAndWorkspace: both are read out of the
// same block, and both change what doctor concludes.
func TestRegistrationParsesCommandAndWorkspace(t *testing.T) {
	const out = "engineering:\n  Command: /bin/emcp\n  Environment:\n    ENGINEERING_WORKSPACE=/ws\n"
	if got := fieldAfter(out, "Command:"); got != "/bin/emcp" {
		t.Errorf("Command = %q", got)
	}
	if got := fieldAfter(out, EnvAssignmentPrefix); got != "/ws" {
		t.Errorf("workspace = %q", got)
	}
	if got := fieldAfter(out, "Nothing:"); got != "" {
		t.Errorf("an absent field must be empty, not %q — callers read it as \"cannot tell\"", got)
	}
}
