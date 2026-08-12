// Package workspace finds the indexed Engineering Kernel workspace a server
// should answer from.
//
// This exists so one installed server works in every repository. A
// server pinned to an absolute path at registration time serves the
// project it was configured for and quietly serves the wrong knowledge
// everywhere else, which is worse than serving none.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar names a workspace to fall back to when the current directory is
// not inside one. It is opt-in on purpose: a developer who sets it has
// asked for the organization's rulebook to follow them into projects
// that are not themselves indexed. Substituting one without being asked
// would be a silent fallback (engineering:rules/no-silent-fallback.md).
const EnvVar = "ENGINEERING_WORKSPACE"

// Source records how a workspace was found, so the server can say so.
type Source string

const (
	SourceFlag    Source = "--workspace flag"
	SourceWalkUp  Source = "found by searching upward from the working directory"
	SourceEnv     Source = "$" + EnvVar
	sourceUnfound Source = ""
)

// Resolved is a workspace and the reason it was chosen.
type Resolved struct {
	Dir    string
	Source Source
}

// Resolve picks the workspace to serve, in order of how specific the
// instruction was: an explicit flag, then the workspace containing the
// working directory, then a configured default.
//
// flagValue is empty when the operator gave no --workspace.
func Resolve(flagValue, workingDir string) (Resolved, error) {
	if strings.TrimSpace(flagValue) != "" {
		dir, err := filepath.Abs(flagValue)
		if err != nil {
			return Resolved{}, fmt.Errorf("workspace: --workspace %q: %w", flagValue, err)
		}
		if !IsIndexed(dir) {
			return Resolved{}, notIndexed(dir, SourceFlag)
		}
		return Resolved{Dir: dir, Source: SourceFlag}, nil
	}

	// Resolved here rather than inside findUpward: an unreadable working
	// directory is its own failure, and reporting it as "no workspace
	// found" would send the developer to `eng workspace create` for a
	// problem that has nothing to do with indexing.
	start, err := filepath.Abs(workingDir)
	if err != nil {
		return Resolved{}, fmt.Errorf("workspace: working directory %q: %w", workingDir, err)
	}
	if dir, ok := findUpward(start); ok {
		return Resolved{Dir: dir, Source: SourceWalkUp}, nil
	}

	if env := strings.TrimSpace(os.Getenv(EnvVar)); env != "" {
		dir, err := filepath.Abs(env)
		if err != nil {
			return Resolved{}, fmt.Errorf("workspace: $%s=%q: %w", EnvVar, env, err)
		}
		if !IsIndexed(dir) {
			return Resolved{}, notIndexed(dir, SourceEnv)
		}
		return Resolved{Dir: dir, Source: SourceEnv}, nil
	}

	return Resolved{}, fmt.Errorf(
		"workspace: no indexed workspace found at or above %s, and %s is not set.\n\n"+
			"To index this project:\n"+
			"    cd %s\n"+
			"    eng workspace create .\n"+
			"    eng workspace attach .\n"+
			"    eng workspace attach /path/to/your/engineering-rules-repo\n\n"+
			"Or to serve one workspace everywhere, set %s to its path.",
		workingDir, EnvVar, workingDir, EnvVar)
}

// findUpward walks from dir, which must be absolute, toward the
// filesystem root looking for an indexed workspace. The nearest one
// wins: a workspace inside a monorepo is a more specific answer than one
// containing it.
func findUpward(dir string) (string, bool) {
	for {
		if IsIndexed(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// IsIndexed reports whether dir holds an indexed workspace. The database
// must already exist: memory.Open would create an empty one, and a
// server answering every question with "nothing found" is
// indistinguishable from one whose knowledge base is simply quiet.
func IsIndexed(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".eng", "memory.db"))
	return err == nil && !info.IsDir()
}

func notIndexed(dir string, src Source) error {
	return fmt.Errorf("workspace: no indexed workspace at %s (from %s) — run `eng workspace create .` and `eng workspace attach <repo>` there first", dir, src)
}
