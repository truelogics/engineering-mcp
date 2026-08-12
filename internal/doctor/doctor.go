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

	"github.com/truelogics/engineering-kernel/pkg/memory"
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
	// Handshake speaks MCP to the binary at self, started in dir with
	// ENGINEERING_WORKSPACE set to workspaceEnv (empty to inherit), and
	// returns the tool names it advertises.
	Handshake func(ctx context.Context, self, dir, workspaceEnv string) ([]string, error)
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

	// Read first, report later. What Claude Code registered decides which
	// workspace the server will actually serve, so the checks below it
	// have to know before they run — even though the registration is
	// reported further down, where it belongs in the layering.
	reg := readRegistration(ctx, env)

	add(checkServerBinary(env))
	add(checkEngCLI(ctx, env))

	ws, wsCheck := checkWorkspace(env, reg)
	add(wsCheck)

	add(checkIndex(ctx, env, ws))
	add(checkKnowledge(ctx, env, ws))
	add(checkClaudeCode(env, reg))
	add(checkHandshake(ctx, env, ws))
	add(checkReviewCommand(env))

	return checks
}

// registration is what `claude mcp get engineering` reports: the binary
// Claude Code launches and the environment it launches it with.
type registration struct {
	// Found is false when the claude CLI is absent, which is a different
	// answer from a server that is not registered.
	Found     bool
	Err       error
	Raw       string
	Command   string
	Workspace string
}

func readRegistration(ctx context.Context, env Env) registration {
	claude, err := env.LookPath("claude")
	if err != nil {
		return registration{}
	}
	out, err := env.Run(ctx, env.WorkingDir, claude, "mcp", "get", "engineering")
	reg := registration{Found: true, Err: err, Raw: out}
	if err != nil {
		return reg
	}
	reg.Command = fieldAfter(out, "Command:")
	reg.Workspace = fieldAfter(out, EnvAssignmentPrefix)
	return reg
}

// EnvAssignmentPrefix is how `claude mcp get` prints the workspace
// variable inside its Environment block.
const EnvAssignmentPrefix = workspace.EnvVar + "="

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
			Fix:    fmt.Sprintf("install it where Claude Code can find it:\n    go install github.com/truelogics/engineering-mcp/cmd/%s@latest", name),
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

// checkEngCLI answers "is the Engineering Kernel available?". The library is linked
// into this binary and cannot be missing; the CLI can, and without it
// nothing can be indexed, so that is the question worth asking.
func checkEngCLI(ctx context.Context, env Env) Check {
	path, err := env.LookPath("eng")
	if err != nil {
		return Check{
			Name:   "Engineering Kernel (eng)",
			Status: StatusFail,
			Detail: "not on $PATH — nothing can be indexed without it",
			Fix:    "go install github.com/truelogics/engineering-kernel/cmd/eng@latest",
		}
	}

	out, err := env.Run(ctx, env.WorkingDir, path, "version")
	if err != nil {
		return Check{
			Name:   "Engineering Kernel (eng)",
			Status: StatusFail,
			Detail: fmt.Sprintf("%s could not be run: %v\n%s", path, err, indent(out)),
			Fix:    "reinstall it: go install github.com/truelogics/engineering-kernel/cmd/eng@latest",
		}
	}
	return Check{Name: "Engineering Kernel (eng)", Status: StatusOK, Detail: fmt.Sprintf("%s (%s)", firstLine(out), path)}
}

// sourceRegistration is not one of workspace.Resolve's own sources. It is
// how doctor records that the workspace came from the Claude Code
// registration rather than from doctor's own environment.
const sourceRegistration workspace.Source = "$" + workspace.EnvVar + " in the Claude Code registration"

// checkWorkspace answers "is the workspace valid?" by running the server's
// own resolver against the current directory — the same call, in the same
// order, that the server makes at startup. Its error text is already
// written for a developer, so it is passed through rather than restated.
//
// With one addition the server does not need. The documented install sets
// ENGINEERING_WORKSPACE inside `claude mcp add -e ...`, which puts it in
// the environment of the server Claude Code spawns and nowhere else — so a
// developer who followed the instructions exactly has it unset in their
// own shell. Resolving from doctor's environment alone reported four
// failures on a correctly installed machine, one line below "Claude Code
// registration: ✔ Connected", and its advice to run `eng workspace create
// .` inside the application would have created precisely the nested .eng/
// the install guide warns about. The diagnostic manufactured the
// misconfiguration it exists to catch. Found reviewing this file.
func checkWorkspace(env Env, reg registration) (workspace.Resolved, Check) {
	const name = "Workspace"

	ws, err := workspace.Resolve(env.WorkspaceFlag, env.WorkingDir)
	if err == nil {
		return ws, Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%s (%s)", ws.Dir, ws.Source)}
	}

	if reg.Workspace != "" && workspace.IsIndexed(reg.Workspace) {
		ws = workspace.Resolved{Dir: reg.Workspace, Source: sourceRegistration}
		return ws, Check{
			Name:   name,
			Status: StatusOK,
			Detail: fmt.Sprintf("%s (%s)\nThis directory is not itself inside a workspace, so that is what Claude Code serves here.", ws.Dir, ws.Source),
			// eng does not read ENGINEERING_WORKSPACE — it has no
			// os.Getenv call at all, and every workspace subcommand takes
			// a positional path defaulting to the current directory. An
			// earlier version of this advised exporting the variable,
			// which changes nothing, and a developer who followed it
			// would have concluded the install was broken.
			Fix: fmt.Sprintf("eng resolves the workspace from the directory it runs in, so run it there:\n    cd %s", ws.Dir),
		}
	}

	detail := err.Error()
	if reg.Workspace != "" {
		// Registered but unusable is a sharper failure than unregistered,
		// and its fix is different.
		detail = fmt.Sprintf("the Claude Code registration points at %s, which holds no index.\n\n%s", reg.Workspace, detail)
	}
	return workspace.Resolved{}, Check{Name: name, Status: StatusFail, Detail: detail}
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
		// The path, not just "eng". A stale copy earlier on $PATH — an old
		// `go install …@latest` shadowing a current build — fails here
		// with a usage message that looks like a bug in this check rather
		// than like two installations. Observed on the author's machine.
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: fmt.Sprintf("%s workspace list failed: %v\n%s", eng, err, indent(out)),
			Fix:    fmt.Sprintf("if that output looks like an older eng, %s is being shadowed on $PATH by an earlier copy.", eng),
		}
	}

	repo := repositoryRoot(ctx, env)
	if repo != "" && !mentionsPath(out, repo) {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: fmt.Sprintf("%s is not attached to this workspace — reviews here will retrieve other repositories' documents\n%s", repo, trimBlank(out)),
			// `cd` first, and not as politeness. `eng workspace attach`
			// has no way to be told which workspace to attach to: it
			// operates on the current directory's, and refuses to create
			// one. Run bare from the repository this warning is about, it
			// fails with "run `eng init` first" — whose advice creates the
			// nested .eng/ the install guide warns against.
			Fix: fmt.Sprintf("cd %s && eng workspace attach %s", ws.Dir, repo),
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
				fmt.Sprintf("    cd %s && eng workspace attach /path/to/your/engineering-rules-repo", ws.Dir),
		}
	}
	return Check{Name: name, Status: StatusOK, Detail: sampled}
}

// checkClaudeCode answers "is Claude Code connected?" using Claude Code's
// own report, including the trap where a server is registered and healthy
// but points at a different binary than the one being developed.
func checkClaudeCode(env Env, reg registration) Check {
	const name = "Claude Code registration"
	if !reg.Found {
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: "the claude CLI is not on $PATH, so registration cannot be checked from here",
		}
	}

	if reg.Err != nil {
		// The CLI's own message, not a guess at what it meant. It fails
		// for reasons other than an unregistered server, and reporting
		// "not registered" for all of them sends the reader to fix
		// something that is not broken.
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: trimBlank(reg.Raw),
			// One command rather than the three-line `claude mcp add`
			// invocation this used to print, whose two absolute paths a
			// developer had to supply from memory. `install` derives
			// both. Spelled as this binary's own path rather than as a
			// bare name, because the check above it is the one that
			// establishes whether the bare name resolves here at all.
			Fix: env.Self + " install",
		}
	}

	switch {
	case strings.Contains(reg.Raw, "Failed to connect"), strings.Contains(reg.Raw, "✘"):
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: trimBlank(reg.Raw),
			Fix:    "the command above is registered but does not start. Run it directly to see why.",
		}
	case reg.Command != "" && !samePath(reg.Command, env.Self):
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: fmt.Sprintf("Claude Code launches %s, not %s", reg.Command, env.Self),
			Fix:    env.Self + " install     # repoints Claude Code at this binary",
		}
	}
	// The trailing "To remove this server, run: ..." hint is help for a
	// different question than the one being asked.
	return Check{Name: name, Status: StatusOK, Detail: firstParagraph(reg.Raw)}
}

// checkHandshake answers "is the MCP server reachable?" the only way that
// proves it: by being a client. It starts the same binary in the same
// directory Claude Code would, and reads the tool list off the wire. This
// is the check that catches anything written to stdout, which corrupts
// the protocol without corrupting anything visible.
func checkHandshake(ctx context.Context, env Env, ws workspace.Resolved) Check {
	const name = "MCP handshake"
	// Skipped rather than run, for the same reason as the two checks
	// above: with no workspace the server exits on startup, and this
	// would report the workspace error a second time as if it were an
	// independent fourth failure — fifteen duplicated lines under a
	// heading that says the transport is broken.
	if ws.Dir == "" {
		return Check{Name: name, Status: StatusFail, Detail: "skipped: no workspace resolved"}
	}
	// The resolved workspace is passed through, so this starts the server
	// the way Claude Code does rather than the way doctor's shell happens
	// to be configured.
	toolNames, err := env.Handshake(ctx, env.Self, env.WorkingDir, ws.Dir)
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
		Fix:    "engineering-mcp install     # writes it from the copy embedded in this binary",
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
// retrieval with. Deterministic rather than random, so two runs of doctor
// on an unchanged repository report the same thing.
//
// Spread across the listing rather than taken from the front. git's sort
// order puts root-level markdown first, so the first ten files of this
// repository are its documentation and none of its Go — and a rulebook
// scoped to `**/*.go` would have been probed with nothing it governs, and
// reported as having nothing to say.
func sampleFiles(ctx context.Context, env Env) []string {
	git, err := env.LookPath("git")
	if err != nil {
		return nil
	}
	// --full-name because rules are matched against repository-relative
	// paths. Without it, running doctor from a subdirectory yields paths
	// relative to that subdirectory, every applies_to glob misses, and
	// the check reports "no rule governs this repository" for a
	// repository that is governed perfectly well.
	out, err := env.Run(ctx, env.WorkingDir, git, "ls-files", "--full-name")
	if err != nil {
		return nil
	}
	var tracked []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			tracked = append(tracked, p)
		}
	}

	const sample = 10
	if len(tracked) <= sample {
		return tracked
	}
	stride := len(tracked) / sample
	paths := make([]string, 0, sample)
	for i := 0; i < len(tracked) && len(paths) < sample; i += stride {
		paths = append(paths, tracked[i])
	}
	return paths
}

// fieldAfter returns the remainder of the first line carrying prefix.
// Empty when no line does, which callers treat as "cannot tell" rather
// than as a mismatch.
func fieldAfter(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), prefix); found {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// mentionsPath reports whether out names dir.
//
// Line-wise and suffix-anchored rather than field-wise. Splitting on
// whitespace shreds `/Users/x/My Projects/api` into two fragments, and
// doctor then reports a perfectly well attached repository as missing —
// on macOS, where directory names with spaces are ordinary. The suffix
// still has to fall on a field boundary, so /work/api does not match a
// line ending in /work/my-api.
func mentionsPath(out, dir string) bool {
	targets := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		targets = append(targets, real)
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, " \t\r")
		for _, t := range targets {
			if line == t {
				return true
			}
			if strings.HasSuffix(line, t) {
				if c := line[len(line)-len(t)-1]; c == ' ' || c == '\t' {
					return true
				}
			}
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

// firstParagraph keeps everything up to the first blank line, which is
// how the CLIs doctor shells out to separate their answer from the advice
// they offer afterwards.
func firstParagraph(s string) string {
	s = trimBlank(s)
	if i := strings.Index(s, "\n\n"); i >= 0 {
		return s[:i]
	}
	return s
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
