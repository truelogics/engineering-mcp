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
	"github.com/truelogics/engineering-mcp/internal/mcp"
	"github.com/truelogics/engineering-mcp/internal/tools"
	"github.com/truelogics/engineering-mcp/internal/workspace"
)

const (
	serverName = "engineering-mcp"
	version    = "0.1.0-alpha"
)

func main() {
	// `doctor` is consumed before flag parsing so it can carry the same
	// flags the server takes, and so an unrecognised subcommand still
	// reaches flag's own error handling rather than being swallowed here.
	args := os.Args[1:]
	subcommand := ""
	if len(args) > 0 && args[0] == "doctor" {
		subcommand, args = args[0], args[1:]
	}

	// ContinueOnError, not ExitOnError: with ExitOnError, Parse never
	// returns and the error branch below is dead code that looks handled.
	fs := flag.NewFlagSet(serverName, flag.ContinueOnError)
	workspaceFlag := fs.String("workspace", "",
		"path to the indexed Engineering Kernel workspace (default: the workspace containing the working directory, then $"+workspace.EnvVar+")")
	showVersion := fs.Bool("version", false, "print version and exit")
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
	if subcommand == "doctor" {
		if !runDoctor(*workspaceFlag) {
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
  %s doctor                 check whether this machine is set up correctly
  %s --version

Flags:
`, serverName, serverName, serverName, serverName)
		fs.PrintDefaults()
	}
}

// runDoctor reports to stdout, unlike the server, which must keep stdout
// clear for JSON-RPC. Nobody is speaking a protocol to doctor.
func runDoctor(workspaceFlag string) bool {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, serverName+":", err)
		return false
	}
	self, err := os.Executable()
	if err != nil {
		// Not fatal — the rest of the checks are still worth running —
		// but said out loud. Substituting the bare name silently made
		// three checks compare a real path against it and report "two
		// builds are installed" and "Claude Code launches X, not
		// engineering-mcp", which is the shape
		// engineering:rules/no-silent-fallback.md exists to prevent: a
		// substitution whose output is indistinguishable from the real
		// thing.
		fmt.Fprintf(os.Stderr, "%s: could not determine this binary's own path (%v).\n"+
			"Checks that compare installed binaries are unreliable in this run.\n\n", serverName, err)
		self = serverName
	}

	ctx := context.Background()
	env := doctor.System(self, cwd, workspaceFlag)
	return doctor.Report(os.Stdout, doctor.Run(ctx, env))
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
