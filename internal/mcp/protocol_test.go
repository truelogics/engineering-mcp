package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func echoTool(name string) Tool {
	return Tool{
		Name:        name,
		Description: "echoes",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "echo: " + string(args), nil
		},
	}
}

// roundTrip feeds lines to Serve and returns the decoded responses.
func roundTrip(t *testing.T, s *Server, lines ...string) []Response {
	t.Helper()
	var out strings.Builder
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var responses []Response
	dec := json.NewDecoder(strings.NewReader(out.String()))
	for dec.More() {
		var r Response
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("decode response: %v (raw: %s)", err, out.String())
		}
		responses = append(responses, r)
	}
	return responses
}

func TestInitializeAnnouncesToolCapability(t *testing.T) {
	s := NewServer("test", "0.0.1")
	got := roundTrip(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}
	result, _ := got[0].Result.(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %v", result["protocolVersion"], ProtocolVersion)
	}
	caps, _ := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities = %v, want a tools capability", caps)
	}
}

// TestNotificationGetsNoResponse pins a protocol requirement that is easy
// to get wrong and breaks a client hard: a JSON-RPC message with no id
// must not be answered.
func TestNotificationGetsNoResponse(t *testing.T) {
	s := NewServer("test", "0.0.1")
	got := roundTrip(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(got) != 0 {
		t.Fatalf("a notification got %d responses, want 0: %+v", len(got), got)
	}
}

func TestToolsListReturnsRegisteredTools(t *testing.T) {
	s := NewServer("test", "0.0.1")
	s.Register(echoTool("alpha"))
	s.Register(echoTool("beta"))

	got := roundTrip(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result, _ := got[0].Result.(map[string]any)
	list, _ := result["tools"].([]any)
	if len(list) != 2 {
		t.Fatalf("tools = %v, want 2", list)
	}
	first, _ := list[0].(map[string]any)
	if first["name"] != "alpha" {
		t.Errorf("first tool = %v, want alpha (registration order)", first["name"])
	}
	if _, ok := first["inputSchema"]; !ok {
		t.Error("every tool must advertise an inputSchema")
	}
}

func TestToolsCallRunsTheHandler(t *testing.T) {
	s := NewServer("test", "0.0.1")
	s.Register(echoTool("alpha"))

	got := roundTrip(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"alpha","arguments":{"x":1}}}`)
	result, _ := got[0].Result.(map[string]any)
	if result["isError"] != false {
		t.Errorf("isError = %v, want false", result["isError"])
	}
	content, _ := result["content"].([]any)
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(text, `"x":1`) {
		t.Errorf("tool text = %q, want the arguments echoed", text)
	}
}

// TestToolFailureIsContentNotProtocolError pins the distinction the
// server draws deliberately: a tool that fails is a result the model
// should see and reason about; a JSON-RPC error means the client is
// broken. Reporting the first as the second would make a missing
// document look like a transport fault.
func TestToolFailureIsContentNotProtocolError(t *testing.T) {
	s := NewServer("test", "0.0.1")
	s.Register(Tool{
		Name:        "boom",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "", context.DeadlineExceeded
		},
	})

	got := roundTrip(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom","arguments":{}}}`)
	if got[0].Error != nil {
		t.Fatalf("a failing tool produced a JSON-RPC error: %+v", got[0].Error)
	}
	result, _ := got[0].Result.(map[string]any)
	if result["isError"] != true {
		t.Errorf("isError = %v, want true", result["isError"])
	}
}

func TestUnknownToolIsInvalidParams(t *testing.T) {
	s := NewServer("test", "0.0.1")
	got := roundTrip(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	if got[0].Error == nil || got[0].Error.Code != CodeInvalidParams {
		t.Fatalf("error = %+v, want CodeInvalidParams", got[0].Error)
	}
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	s := NewServer("test", "0.0.1")
	got := roundTrip(t, s, `{"jsonrpc":"2.0","id":6,"method":"resources/list"}`)
	if got[0].Error == nil || got[0].Error.Code != CodeMethodNotFound {
		t.Fatalf("error = %+v, want CodeMethodNotFound", got[0].Error)
	}
}

func TestMalformedJSONIsParseError(t *testing.T) {
	s := NewServer("test", "0.0.1")
	got := roundTrip(t, s, `{not json`)
	if got[0].Error == nil || got[0].Error.Code != CodeParseError {
		t.Fatalf("error = %+v, want CodeParseError", got[0].Error)
	}
}

func TestDuplicateToolRegistrationPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate tool name should panic — a silently shadowed capability is indistinguishable from the intended one")
		}
	}()
	s := NewServer("test", "0.0.1")
	s.Register(echoTool("alpha"))
	s.Register(echoTool("alpha"))
}
