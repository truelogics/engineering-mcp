package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// indexed creates a directory that looks like an indexed workspace.
func indexed(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".eng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".eng", "memory.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolvePrefersAnExplicitFlag(t *testing.T) {
	flagged := indexed(t, filepath.Join(t.TempDir(), "flagged"))
	cwd := indexed(t, filepath.Join(t.TempDir(), "cwd"))

	got, err := Resolve(flagged, cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Dir != flagged {
		t.Errorf("Dir = %q, want the flagged workspace %q — an explicit instruction outranks discovery", got.Dir, flagged)
	}
	if got.Source != SourceFlag {
		t.Errorf("Source = %q, want %q", got.Source, SourceFlag)
	}
}

// TestResolveFindsTheWorkspaceContainingTheProject is the capability
// this package exists for: one registered server, every repository.
func TestResolveFindsTheWorkspaceContainingTheProject(t *testing.T) {
	root := indexed(t, t.TempDir())
	deep := filepath.Join(root, "repo", "internal", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve("", deep)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Dir != root {
		t.Errorf("Dir = %q, want %q", got.Dir, root)
	}
	if got.Source != SourceWalkUp {
		t.Errorf("Source = %q, want %q", got.Source, SourceWalkUp)
	}
}

// TestResolvePrefersTheNearestWorkspace: a repository with its own
// workspace inside a larger one is a more specific answer than the
// workspace that happens to contain it.
func TestResolvePrefersTheNearestWorkspace(t *testing.T) {
	outer := indexed(t, t.TempDir())
	inner := indexed(t, filepath.Join(outer, "project"))

	got, err := Resolve("", inner)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Dir != inner {
		t.Errorf("Dir = %q, want the nearer workspace %q", got.Dir, inner)
	}
}

func TestResolveFallsBackToTheConfiguredWorkspace(t *testing.T) {
	configured := indexed(t, filepath.Join(t.TempDir(), "org"))
	elsewhere := t.TempDir()
	t.Setenv(EnvVar, configured)

	got, err := Resolve("", elsewhere)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Dir != configured || got.Source != SourceEnv {
		t.Errorf("got %+v, want %q from %q", got, configured, SourceEnv)
	}
}

// TestResolveFailsRatherThanServeNothing pins
// engineering:rules/no-silent-fallback.md. An empty workspace answers
// every question with "nothing found", which reads exactly like a
// knowledge base that is simply quiet.
func TestResolveFailsRatherThanServeNothing(t *testing.T) {
	t.Setenv(EnvVar, "")
	_, err := Resolve("", t.TempDir())
	if err == nil {
		t.Fatal("want an error when no workspace can be found")
	}
	for _, want := range []string{"eng workspace create", EnvVar} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should tell the developer how to fix it, missing %q: %v", want, err)
		}
	}
}

func TestResolveRejectsAnUnindexedExplicitWorkspace(t *testing.T) {
	if _, err := Resolve(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("want an error — a named workspace that is not indexed is a mistake, not a default")
	}
}

func TestResolveDoesNotTreatADirectoryAsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".eng", "memory.db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if IsIndexed(dir) {
		t.Error("a directory named memory.db is not an indexed workspace")
	}
}
