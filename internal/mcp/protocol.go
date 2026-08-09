// Package mcp implements the Model Context Protocol wire layer —
// JSON-RPC 2.0 over newline-delimited stdio — and nothing else. It knows
// how to speak the protocol and how to route a tool call; it knows
// nothing about engineering knowledge. That separation is the whole
// point of this repository: transports change (MCP today, HTTP or gRPC
// later), capabilities don't.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the MCP revision this server implements.
const ProtocolVersion = "2025-06-18"

// Request is an incoming JSON-RPC 2.0 message. A message with no ID is a
// notification and gets no response.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is an outgoing JSON-RPC 2.0 message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC 2.0 reserved error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Tool is one capability exposed over the protocol.
//
// Handler returns human-readable text — MCP's content model — rather
// than a typed struct, because the consumer on the other end is a model
// deciding what to do next, not a program unmarshalling a contract.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(ctx context.Context, args json.RawMessage) (string, error)
}

// Server routes JSON-RPC messages to registered Tools.
type Server struct {
	Name    string
	Version string

	mu    sync.RWMutex
	tools []Tool
}

// NewServer returns a Server with no tools registered.
func NewServer(name, version string) *Server {
	return &Server{Name: name, Version: version}
}

// Register adds a tool. Registering the same name twice is a programmer
// error and panics: a server that silently shadowed a capability would
// be indistinguishable from one that exposed the intended one.
func (s *Server) Register(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tools {
		if existing.Name == t.Name {
			panic("mcp: duplicate tool registered: " + t.Name)
		}
	}
	s.tools = append(s.tools, t)
}

// Tools returns the registered tools, in registration order.
func (s *Server) Tools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Tool(nil), s.tools...)
}

// Serve reads newline-delimited JSON-RPC messages from in and writes
// responses to out until in is exhausted.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// A ContextPackage rendered as text runs well past bufio's 64KB
	// default; a truncated line would be a parse error that looks like a
	// malformed client.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := encoder.Encode(errorResponse(nil, CodeParseError, "parse error")); err != nil {
				return err
			}
			continue
		}

		resp, ok := s.handle(ctx, req)
		if !ok {
			continue // a notification: no reply
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// handle dispatches one request, reporting whether a response is owed.
func (s *Server) handle(ctx context.Context, req Request) (Response, bool) {
	if req.ID == nil {
		// Notification. `notifications/initialized` is the only one this
		// server expects; unknown notifications are ignored rather than
		// answered, per JSON-RPC.
		return Response{}, false
	}

	switch req.Method {
	case "initialize":
		return okResponse(req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}), true

	case "ping":
		return okResponse(req.ID, map[string]any{}), true

	case "tools/list":
		tools := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.Tools() {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return okResponse(req.ID, map[string]any{"tools": tools}), true

	case "tools/call":
		return s.callTool(ctx, req), true

	default:
		return errorResponse(req.ID, CodeMethodNotFound, "unknown method: "+req.Method), true
	}
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// callTool runs one tool. A tool that fails returns its error as tool
// *content* with isError set, not as a JSON-RPC error: a failed
// retrieval is a result the model should see and reason about, whereas a
// protocol error means the client itself is broken.
func (s *Server) callTool(ctx context.Context, req Request) Response {
	var params callParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
	}

	var tool *Tool
	for _, t := range s.Tools() {
		if t.Name == params.Name {
			candidate := t
			tool = &candidate
			break
		}
	}
	if tool == nil {
		return errorResponse(req.ID, CodeInvalidParams, "unknown tool: "+params.Name)
	}

	text, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		return okResponse(req.ID, toolResult(fmt.Sprintf("%s failed: %v", tool.Name, err), true))
	}
	return okResponse(req.ID, toolResult(text, false))
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func okResponse(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, message string) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
}

// ErrNoWorkspace is returned when the configured workspace is missing.
var ErrNoWorkspace = errors.New("mcp: no indexed workspace")
