// Command engineering-mcp exposes the Engineering OS's proven knowledge
// capabilities to an MCP client over stdio.
//
// It reads. It never indexes, never writes, and never reviews: the
// client does the reasoning, this server answers questions about
// engineering knowledge. See README.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/truelogics/engineering-kernel/pkg/memory"
	"github.com/truelogics/engineering-mcp/internal/doctor"
	"github.com/truelogics/engineering-mcp/internal/install"
	"github.com/truelogics/engineering-mcp/internal/mcp"
	"github.com/truelogics/engineering-mcp/internal/tools"
	"github.com/truelogics/engineering-mcp/internal/workspace"
)

const (
	serverName = "engineering-mcp"
	version    = "0.1.0-alpha"
)

func main() {
	// Subcommands are consumed before flag parsing so they can carry the
	// same flags the server takes, and so an unrecognised subcommand
	// still reaches flag's own error handling rather than being
	// swallowed here.
	args := os.Args[1:]
	subcommand := ""
	if len(args) > 0 && (args[0] == "doctor" || args[0] == "install") {
		subcommand, args = args[0], args[1:]
	}

	// ContinueOnError, not ExitOnError: with ExitOnError, Parse never
	// returns and the error branch below is dead code that looks handled.
	fs := flag.NewFlagSet(serverName, flag.ContinueOnError)
	workspaceFlag := fs.String("workspace", "",
		"path to the indexed Engineering Kernel workspace (default: the workspace containing the working directory, then $"+workspace.EnvVar+")")
	showVersion := fs.Bool("version", false, "print version and exit")
	force := fs.Bool("force", false, "install: overwrite an existing /review-branch command")
	fs.Usage = usage(fs)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	// Without this, a mistyped subcommand is a positional argument, which
	// flag ignores — and the server starts and waits on stdin, looking
	// for all the world like a command that hung. `engineering-mcp doctr`
	// should say so.
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "%s: unknown argument %q\n\n", serverName, fs.Arg(0))
		fs.Usage()
		os.Exit(2)
	}

	// The subcommand outranks --version. The other order made
	// `engineering-mcp doctor --version` print a version string and never
	// run the diagnostic it was asked for.
	switch subcommand {
	case "doctor":
		if !runDoctor(*workspaceFlag) {
			os.Exit(1)
		}
		return
	case "install":
		if !runInstall(*workspaceFlag, *force) {
			os.Exit(1)
		}
		return
	}

	if *showVersion {
		fmt.Println(serverName + " " + version)
		return
	}

	if err := run(*workspaceFlag); err != nil {
		// stderr, never stdout: stdout is the JSON-RPC channel, and a
		// stray line on it corrupts the protocol for the client.
		fmt.Fprintln(os.Stderr, serverName+":", err)
		os.Exit(1)
	}
}

func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `%s - Engineering OS knowledge over the Model Context Protocol

Usage:
  %s [--workspace <dir>]    serve MCP over stdin/stdout (what Claude Code runs)
  %s install                register with Claude Code and install /review-branch
  %s doctor                 check whether this machine is set up correctly
  %s --version

`+"`eng setup`"+` runs install for you, along with everything before it.

Flags:
`, serverName, serverName, serverName, serverName, serverName)
		fs.PrintDefaults()
	}
}

// runDoctor reports to stdout, unlike the server, which must keep stdout
// clear for JSON-RPC. Nobody is speaking a protocol to doctor.
func runDoctor(workspaceFlag string) bool {
	cwd, self, ok := location(false)
	if !ok {
		return false
	}
	ctx := context.Background()
	env := doctor.System(self, cwd, workspaceFlag)
	return doctor.Report(os.Stdout, doctor.Run(ctx, env))
}

// runInstall registers this binary with Claude Code and installs the
// /review-branch command. Reported to stdout for the same reason as
// doctor.
func runInstall(workspaceFlag string, force bool) bool {
	// Fatal here, unlike in doctor. Doctor's own path being unknown
	// makes three checks unreliable; install's own path being unknown
	// makes it write a registration pointing at the string
	// "engineering-mcp", which Claude Code resolves against whatever is
	// on $PATH at session start — a plausible-looking entry that launches
	// something other than what was installed.
	cwd, self, ok := location(true)
	if !ok {
		return false
	}
	ctx := context.Background()
	env := install.System(self, cwd, workspaceFlag)
	return install.Report(os.Stdout, install.Run(ctx, env, force))
}

// location returns the working directory and this binary's own path.
//
// When the executable path cannot be determined and fatal is false, the
// bare command name is substituted and the substitution is announced.
// Doing it silently made three of doctor's checks compare a real path
// against it and report "two builds are installed" and "Claude Code
// launches X, not engineering-mcp" — the shape
// engineering:rules/no-silent-fallback.md exists to prevent, where the
// output of a substitution is indistinguishable from the real thing.
func location(fatal bool) (cwd, self string, ok bool) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, serverName+":", err)
		return "", "", false
	}
	self, err = os.Executable()
	if err != nil {
		if fatal {
			fmt.Fprintf(os.Stderr, "%s: could not determine this binary's own path (%v),\n"+
				"and a registration must name it absolutely. Nothing was changed.\n", serverName, err)
			return "", "", false
		}
		fmt.Fprintf(os.Stderr, "%s: could not determine this binary's own path (%v).\n"+
			"Checks that compare installed binaries are unreliable in this run.\n\n", serverName, err)
		self = serverName
	}
	return cwd, self, true
}

func run(workspaceFlag string) error {
	// The working directory is the project the client opened. Resolving
	// per start, rather than pinning a path at registration, is what
	// lets one installed server answer for every repository.
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	ws, err := workspace.Resolve(workspaceFlag, cwd)
	if err != nil {
		return err
	}

	// stderr, never stdout. Which knowledge base answered is the first
	// thing to check when a review cites something unexpected, and a
	// server that resolved somewhere surprising should say so rather
	// than let the reader assume.
	fmt.Fprintf(os.Stderr, "%s: serving %s (%s)\n", serverName, ws.Dir, ws.Source)

	m, err := memory.Open(ws.Dir)
	if err != nil {
		return err
	}
	defer m.Close()

	server := mcp.NewServer(serverName, version)
	for _, t := range tools.All(m, ws.Dir) {
		server.Register(t)
	}

	return server.Serve(context.Background(), os.Stdin, os.Stdout)
}
