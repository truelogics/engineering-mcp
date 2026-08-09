package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/truelogics/ai-memory/pkg/memory"
)

type fakeSource struct {
	searchResults []memory.SearchResult
	pkg           memory.ContextPackage
	gotQuery      string
	gotTask       string
	gotPaths      []string
}

func (f *fakeSource) Search(ctx context.Context, query string, opts memory.SearchOptions) ([]memory.SearchResult, error) {
	f.gotQuery = query
	return f.searchResults, nil
}

func (f *fakeSource) ContextFor(ctx context.Context, task string, opts memory.ContextOptions) (memory.ContextPackage, error) {
	f.gotTask = task
	f.gotPaths = opts.ChangedPaths
	return f.pkg, nil
}

func toolNamed(t *testing.T, src KnowledgeSource, name string) func(context.Context, json.RawMessage) (string, error) {
	t.Helper()
	for _, tool := range All(src) {
		if tool.Name == name {
			return tool.Handler
		}
	}
	t.Fatalf("no tool named %q", name)
	return nil
}

func TestExposesOnlyRuleSixApprovedTools(t *testing.T) {
	var names []string
	for _, tool := range All(&fakeSource{}) {
		names = append(names, tool.Name)
	}
	want := []string{"search_memory", "get_context", "find_engineering_rules", "verify_evidence"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want exactly %v — a new tool needs a validated consumer capability behind it (KERNEL_POLICY Rule #6)", names, want)
	}
}

func TestEveryToolAdvertisesADescriptionAndSchema(t *testing.T) {
	for _, tool := range All(&fakeSource{}) {
		if len(tool.Description) < 40 {
			t.Errorf("%s: description is too thin for a model to choose the tool by", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("%s: inputSchema must be an object schema", tool.Name)
		}
		if _, ok := tool.InputSchema["required"]; !ok {
			t.Errorf("%s: inputSchema must state its required arguments", tool.Name)
		}
	}
}

func TestSearchMemoryQualifiesResultsByRepository(t *testing.T) {
	src := &fakeSource{searchResults: []memory.SearchResult{
		{Path: "README.md", Repository: "engineering", Score: 0.8, Snippet: "the **rules** index"},
		{Path: "README.md", Repository: "ai-review", Score: 0.4, Snippet: "the review engine"},
	}}
	got, err := toolNamed(t, src, "search_memory")(context.Background(), json.RawMessage(`{"query":"rules"}`))
	if err != nil {
		t.Fatalf("search_memory: %v", err)
	}
	for _, want := range []string{"engineering:README.md", "ai-review:README.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q — two repositories' README.md must be distinguishable:\n%s", want, got)
		}
	}
	if strings.Contains(got, "**rules**") {
		t.Error("FTS highlight markers should be stripped before reaching a model")
	}
}

func TestSearchMemoryRequiresAQuery(t *testing.T) {
	if _, err := toolNamed(t, &fakeSource{}, "search_memory")(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("want an error when query is missing")
	}
}

func TestGetContextPassesChangedPathsThrough(t *testing.T) {
	src := &fakeSource{}
	_, err := toolNamed(t, src, "get_context")(context.Background(),
		json.RawMessage(`{"task":"add caching","changed_paths":["internal/authz/check.go"]}`))
	if err != nil {
		t.Fatalf("get_context: %v", err)
	}
	if src.gotTask != "add caching" {
		t.Errorf("task = %q, want 'add caching'", src.gotTask)
	}
	if len(src.gotPaths) != 1 || src.gotPaths[0] != "internal/authz/check.go" {
		t.Errorf("changed_paths = %v, want the file list forwarded — it scopes which rules apply", src.gotPaths)
	}
}

// TestGetContextNamesEmptySections pins a deliberate choice: "no rule
// governs this" and "I didn't look" are different answers, and a model
// cannot tell them apart from silence.
func TestGetContextNamesEmptySections(t *testing.T) {
	got, err := toolNamed(t, &fakeSource{}, "get_context")(context.Background(), json.RawMessage(`{"task":"anything"}`))
	if err != nil {
		t.Fatalf("get_context: %v", err)
	}
	for _, want := range []string{"Engineering rules", "Architecture decision records", "(none)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestFindEngineeringRulesReturnsGoverningRules(t *testing.T) {
	src := &fakeSource{pkg: memory.ContextPackage{Rules: []memory.FileContext{
		{Path: "rules/no-silent-fallback.md", Repository: "engineering", Snippet: "Never silently fall back to a fake"},
	}}}
	got, err := toolNamed(t, src, "find_engineering_rules")(context.Background(),
		json.RawMessage(`{"changed_paths":["internal/provider/claude/claude.go"]}`))
	if err != nil {
		t.Fatalf("find_engineering_rules: %v", err)
	}
	if !strings.Contains(got, "engineering:rules/no-silent-fallback.md") {
		t.Errorf("output missing the governing rule:\n%s", got)
	}
}

func TestFindEngineeringRulesRequiresPaths(t *testing.T) {
	if _, err := toolNamed(t, &fakeSource{}, "find_engineering_rules")(context.Background(), json.RawMessage(`{"changed_paths":[]}`)); err == nil {
		t.Fatal("want an error when changed_paths is empty — without paths there is nothing to scope against")
	}
}

// TestFindEngineeringRulesSaysSoWhenNoneApply covers RFC-0005's third
// fallback surfacing to a client: an empty result is an answer.
func TestFindEngineeringRulesSaysSoWhenNoneApply(t *testing.T) {
	got, err := toolNamed(t, &fakeSource{}, "find_engineering_rules")(context.Background(),
		json.RawMessage(`{"changed_paths":["main.rb"]}`))
	if err != nil {
		t.Fatalf("find_engineering_rules: %v", err)
	}
	if !strings.Contains(got, "No engineering rule governs") {
		t.Errorf("output should state plainly that no rule applies:\n%s", got)
	}
	// Sprint 11: a workspace holding no rules at all returned this same
	// confident sentence. The two cases are indistinguishable to a
	// reader, so the message has to name the second one.
	if !strings.Contains(got, "eng workspace list") {
		t.Errorf("output must say how to check that a rulebook was actually consulted:\n%s", got)
	}
}

func TestFindEngineeringRulesFallsBackToPathsAsTask(t *testing.T) {
	src := &fakeSource{}
	if _, err := toolNamed(t, src, "find_engineering_rules")(context.Background(),
		json.RawMessage(`{"changed_paths":["a.go","b.go"]}`)); err != nil {
		t.Fatalf("find_engineering_rules: %v", err)
	}
	if src.gotTask != "a.go b.go" {
		t.Errorf("task = %q, want the paths used for ordering when no task is given", src.gotTask)
	}
}

func TestVerifyEvidenceConfirmsARealQuote(t *testing.T) {
	src := &fakeSource{pkg: memory.ContextPackage{Rules: []memory.FileContext{
		{Path: "rules/logging.md", Repository: "engineering", Snippet: "All logging goes through internal/log."},
	}}}
	got, err := toolNamed(t, src, "verify_evidence")(context.Background(),
		json.RawMessage(`{"task":"logging","document":"engineering:rules/logging.md","excerpt":"goes through internal/log"}`))
	if err != nil {
		t.Fatalf("verify_evidence: %v", err)
	}
	if !strings.Contains(got, "VERIFIED (high confidence)") {
		t.Errorf("output = %q, want a high-confidence verification", got)
	}
}

// TestVerifyEvidenceReportsFailureRatherThanDropping is the policy
// difference from AI Review: a review drops an unverifiable claim
// silently because it is finished; a model can revise, so it is told.
func TestVerifyEvidenceReportsFailureRatherThanDropping(t *testing.T) {
	src := &fakeSource{pkg: memory.ContextPackage{Rules: []memory.FileContext{
		{Path: "rules/logging.md", Repository: "engineering", Snippet: "All logging goes through internal/log."},
	}}}
	got, err := toolNamed(t, src, "verify_evidence")(context.Background(),
		json.RawMessage(`{"task":"logging","document":"engineering:rules/logging.md","excerpt":"logging must be disabled in production"}`))
	if err != nil {
		t.Fatalf("verify_evidence: %v", err)
	}
	if !strings.Contains(got, "NOT VERIFIED") {
		t.Errorf("output = %q, want an explicit non-verification", got)
	}
	if !strings.Contains(got, "Do not present this as a citation") {
		t.Errorf("output = %q, want the consequence stated", got)
	}
}

// TestVerifyEvidenceForwardsChangedPaths pins the Sprint 11 fix. Rules
// are selected by path scope, so verifying against an unscoped context
// rejected verbatim quotes from rules the server had just returned —
// a gate that fails closed on true citations teaches a reviewer to stop
// citing.
func TestVerifyEvidenceForwardsChangedPaths(t *testing.T) {
	src := &fakeSource{pkg: memory.ContextPackage{Rules: []memory.FileContext{
		{Path: "rules/logging.md", Repository: "engineering", Snippet: "All logging goes through internal/log."},
	}}}
	got, err := toolNamed(t, src, "verify_evidence")(context.Background(),
		json.RawMessage(`{"task":"logging","document":"engineering:rules/logging.md","excerpt":"goes through internal/log","changed_paths":["internal/log/log.go"]}`))
	if err != nil {
		t.Fatalf("verify_evidence: %v", err)
	}
	if len(src.gotPaths) != 1 || src.gotPaths[0] != "internal/log/log.go" {
		t.Errorf("changed_paths = %v, want them forwarded — a scoped rule is invisible to an unscoped context", src.gotPaths)
	}
	if !strings.Contains(got, "VERIFIED (high confidence)") {
		t.Errorf("output = %q, want a verification", got)
	}
}

// TestVerifyEvidenceSaysWhenScopeIsMissing: the likeliest cause of a
// failed verification is a caller who omitted the paths, and a failure
// message that does not name its likeliest cause is a dead end.
func TestVerifyEvidenceSaysWhenScopeIsMissing(t *testing.T) {
	got, err := toolNamed(t, &fakeSource{}, "verify_evidence")(context.Background(),
		json.RawMessage(`{"task":"logging","document":"engineering:rules/logging.md","excerpt":"anything"}`))
	if err != nil {
		t.Fatalf("verify_evidence: %v", err)
	}
	if !strings.Contains(got, "no changed_paths") {
		t.Errorf("output = %q, want the missing scope named as a likely cause", got)
	}
}

func TestVerifyEvidenceRequiresAllThreeArguments(t *testing.T) {
	for _, args := range []string{
		`{"document":"a.md","excerpt":"x"}`,
		`{"task":"t","excerpt":"x"}`,
		`{"task":"t","document":"a.md"}`,
	} {
		if _, err := toolNamed(t, &fakeSource{}, "verify_evidence")(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("args %s: want an error", args)
		}
	}
}
