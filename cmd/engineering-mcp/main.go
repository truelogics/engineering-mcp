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

	"github.com/truelogics/ai-memory/pkg/memory"
	"github.com/truelogics/engineering-mcp/internal/mcp"
	"github.com/truelogics/engineering-mcp/internal/tools"
	"github.com/truelogics/engineering-mcp/internal/workspace"
)

const (
	serverName = "engineering-mcp"
	version    = "0.1.0-alpha"
)

func main() {
	workspaceFlag := flag.String("workspace", "",
		"path to the indexed AI Memory workspace (default: the workspace containing the working directory, then $"+workspace.EnvVar+")")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

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
