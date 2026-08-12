package doctor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/truelogics/engineering-kernel/pkg/memory"
	"github.com/truelogics/engineering-mcp/internal/workspace"
)

// commandTimeout bounds every external command. A doctor that hangs is
// worse than one that reports a failure, because the developer cannot
// tell which check it stopped on.
const commandTimeout = 30 * time.Second

// System returns an Env wired to the real machine. Self should be the
// path of the running binary.
func System(self, workingDir, workspaceFlag string) Env {
	return Env{
		Self:          self,
		WorkingDir:    workingDir,
		WorkspaceFlag: workspaceFlag,
		Home:          os.Getenv("HOME"),
		LookPath:      exec.LookPath,
		Run:           runCommand,
		Handshake:     handshake,
		OpenKnowledge: openKnowledge,
		Exists:        Exists,
	}
}

// runCommand returns combined output whether or not the command
// succeeded: a CLI's own failure message is usually the diagnosis, and
// discarding it would leave doctor reporting "it failed" and nothing else.
func runCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func openKnowledge(dir string) (Knowledge, error) {
	return memory.Open(dir)
}

// handshake starts the server binary the way an MCP client does — same
// working directory, stdio transport — and reads the tool list back off
// the wire.
//
// Nothing here shortcuts to the in-process tool registry. The failure
// this check exists to catch is a stray line on stdout, which leaves the
// registry perfectly correct and the protocol unparseable.
func handshake(ctx context.Context, self, dir, workspaceEnv string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, self)
	cmd.Dir = dir
	// The workspace the earlier checks resolved, which may have come from
	// the Claude Code registration rather than from this shell. Without
	// it, the handshake fails on a correctly installed machine whose
	// developer never exported the variable — see checkWorkspace.
	if workspaceEnv != "" {
		cmd.Env = append(os.Environ(), workspace.EnvVar+"="+workspaceEnv)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("doctor: stdin pipe for %s: %w", self, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("doctor: stdout pipe for %s: %w", self, err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("doctor: could not start %s: %w", self, err)
	}
	// The server exits when stdin closes, which is how this returns
	// rather than waiting out the timeout.
	defer func() {
		stdin.Close()
		_ = cmd.Wait()
	}()

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"engineering-mcp doctor","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}
	for _, r := range requests {
		if _, err := io.WriteString(stdin, r+"\n"); err != nil {
			return nil, withStderr("could not send a request", err, stderr.String())
		}
	}

	names, err := readToolList(stdout)
	if err != nil {
		return nil, withStderr("could not read the tool list", err, stderr.String())
	}
	return names, nil
}

// readToolList reads responses until the one answering request id 2.
//
// Matched by id rather than by "the response that has tools in it". A
// server that advertises none is a real failure with its own message, and
// skipping empty results made that case fall through to EOF and report
// that the transport died — sending a developer whose registry failed to
// populate to debug the wrong layer.
const toolListID = "2"

func readToolList(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var resp struct {
			ID     json.RawMessage `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			// Not a protocol error on the server's part but a fatal one
			// for any client: name the offending line, because that is
			// the whole content of the diagnosis.
			return nil, fmt.Errorf("the server wrote a line that is not JSON-RPC, which corrupts the protocol: %q", truncate(line, 200))
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("the server returned an error: %s", resp.Error.Message)
		}
		if strings.TrimSpace(string(resp.ID)) != toolListID {
			continue // the initialize response
		}
		names := make([]string, 0, len(resp.Result.Tools))
		for _, t := range resp.Result.Tools {
			names = append(names, t.Name)
		}
		return names, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading the server's output: %w", err)
	}
	return nil, fmt.Errorf("the server closed the connection without answering tools/list")
}

// withStderr is the package boundary for handshake's failures, so the
// `doctor:` prefix goes on here rather than on every message readToolList
// produces — engineering:rules/go-wrap-errors.md asks for one prefix
// naming the package, not one per layer.
func withStderr(what string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("doctor: %s: %w", what, err)
	}
	return fmt.Errorf("doctor: %s: %w\n%s", what, err, indent(stderr))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
