package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/pkg/memory"
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

// testEnv is a machine where everything works. Tests break one thing.
func testEnv(t *testing.T) (Env, *fakeKnowledge) {
	t.Helper()
	dir := t.TempDir()
	self := filepath.Join(dir, "engineering-mcp")
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	commands := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(commands, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commands, "review-branch.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	knowledge := &fakeKnowledge{pkg: memory.ContextPackage{
		Rules: []memory.FileContext{{Repository: "engineering", Path: "rules/a.md"}},
	}}

	return Env{
		Self:       self,
		WorkingDir: dir,
		Home:       dir,
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
		Handshake: func(context.Context, string, string) ([]string, error) {
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
	if err := os.MkdirAll(filepath.Join(env.WorkingDir, ".eng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.WorkingDir, ".eng", "memory.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	for _, name := range []string{"Workspace index", "Engineering knowledge"} {
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

	c := byName(t, Run(context.Background(), env), "AI Memory (eng)")
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
}

// TestNoRulesWarnsRatherThanPasses is the failure the whole platform is
// most vulnerable to: a workspace that resolves, opens and answers, and
// holds no rulebook. Every mechanical check passes and every review is
// told nothing governs anything.
func TestNoRulesWarnsRatherThanPasses(t *testing.T) {
	env, knowledge := testEnv(t)
	knowledge.pkg = memory.ContextPackage{}
	if err := os.MkdirAll(filepath.Join(env.WorkingDir, ".eng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.WorkingDir, ".eng", "memory.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if err := os.MkdirAll(filepath.Join(env.WorkingDir, ".eng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.WorkingDir, ".eng", "memory.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	env.Handshake = func(context.Context, string, string) ([]string, error) {
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
