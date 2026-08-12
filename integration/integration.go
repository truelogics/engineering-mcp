// Package integration carries the client-side files Engineering OS
// installs onto a machine, compiled into the binary.
//
// Embedded rather than copied from the repository, and that is the whole
// point of the package. The documented install said `cp
// integration/claude-code/review-branch.md ~/.claude/commands/`, which
// requires a clone — so a developer who installed the binaries with `go
// install` had no such directory, and the one step the tool-discovery
// experiment showed to be load-bearing was the one step they could not
// perform. A file the binary needs at install time belongs inside the
// binary.
package integration

import _ "embed"

// ReviewBranchCommand is the /review-branch slash command. Without it,
// on a machine with several MCP servers installed, Claude Code defers
// this server's tool schemas and never calls it — measured in
// docs/reports/TOOL_DISCOVERY_EXPERIMENT.md.
//
//go:embed claude-code/review-branch.md
var ReviewBranchCommand string

// ReviewBranchFilename is the name it must be installed under: Claude
// Code derives the command name from the filename, so `/review-branch`
// exists only if the file is called this.
const ReviewBranchFilename = "review-branch.md"

// ProjectRegistration is the project-scope .mcp.json template, for
// teams who would rather commit the registration than run `claude mcp
// add` on every machine.
//
//go:embed claude-code/mcp.json.example
var ProjectRegistration string
