// Package tools defines the Engineering OS capabilities this server
// exposes over MCP. Each one is a thin adapter over pkg/memory: argument
// validation, a call, and a rendering. No engineering logic lives here,
// and none should — a capability that needed logic in this package would
// be a capability the kernel does not actually have.
//
// Every tool satisfies engineering/KERNEL_POLICY.md Rule #6: it already
// exists as a validated capability inside a real consumer. See README.md
// for the two proposed tools that failed that test and are deliberately
// absent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/truelogics/ai-memory/pkg/memory"
	"github.com/truelogics/engineering-mcp/internal/mcp"
)

// KnowledgeSource is the subset of *memory.Memory these tools use —
// narrow so the package is testable without a workspace on disk, and so
// it is obvious at a glance that this server reads engineering knowledge
// and does nothing else. There is no Index, no Sync, no AddRepository:
// this server never writes.
type KnowledgeSource interface {
	Search(ctx context.Context, query string, opts memory.SearchOptions) ([]memory.SearchResult, error)
	ContextFor(ctx context.Context, task string, opts memory.ContextOptions) (memory.ContextPackage, error)
}

// All returns every tool this server exposes, bound to src.
func All(src KnowledgeSource) []mcp.Tool {
	return []mcp.Tool{
		searchMemory(src),
		getContext(src),
		findEngineeringRules(src),
		verifyEvidence(src),
	}
}

// verifyEvidence exposes ai-memory RFC-0006's Evidence capability,
// promoted into the kernel precisely so this tool could exist without
// duplicating AI Review's Validator. It is named for what it does —
// verifying a quote a client proposes — rather than "collect_evidence",
// which would imply the kernel searches for supporting text. It does
// not, and no consumer has asked it to (KERNEL_POLICY Rule #5).
//
// Where AI Review silently drops an unverifiable claim because a review
// is finished, this reports the failure: a model can revise, and being
// told a quote didn't check out is the most useful answer it can get.
func verifyEvidence(src KnowledgeSource) mcp.Tool {
	return mcp.Tool{
		Name: "verify_evidence",
		Description: "Check that a quote you are about to attribute to an engineering document really appears in it. " +
			"Use before writing 'per ADR-0003, ...' or citing a rule, so the citation is checkable rather than asserted. " +
			"Returns the verified quote with a confidence grade, or tells you the quote could not be verified.",
		InputSchema: object(map[string]any{
			"task":     stringProp("The task whose context contains the document, e.g. 'cache permission lookups'."),
			"document": stringProp("The document to verify against, ideally repository-qualified, e.g. 'engineering:rules/logging.md'."),
			"excerpt":  stringProp("The exact text you intend to quote from that document."),
			"changed_paths": stringArrayProp("The same files you passed to find_engineering_rules or get_context. " +
				"Required to verify a quote from a rule, because rules are selected by path scope and are absent from an unscoped context."),
		}, "task", "document", "excerpt"),
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Task         string   `json:"task"`
				Document     string   `json:"document"`
				Excerpt      string   `json:"excerpt"`
				ChangedPaths []string `json:"changed_paths"`
			}
			if err := decode(raw, &args); err != nil {
				return "", err
			}
			for name, v := range map[string]string{"task": args.Task, "document": args.Document, "excerpt": args.Excerpt} {
				if strings.TrimSpace(v) == "" {
					return "", fmt.Errorf("%s is required", name)
				}
			}

			// The same options the other tools use. Verifying against an
			// unscoped context was a silent trap: rules are selected by
			// path scope, so a rule find_engineering_rules had just
			// returned was absent here, and a verbatim quote from it came
			// back NOT VERIFIED. See docs/reports/SPRINT_11_VALIDATION.md.
			pkg, err := src.ContextFor(ctx, args.Task, memory.ContextOptions{ChangedPaths: args.ChangedPaths})
			if err != nil {
				return "", err
			}

			ev, ok := pkg.VerifyEvidence(args.Document, args.Excerpt)
			if !ok {
				hint := ""
				if len(args.ChangedPaths) == 0 {
					hint = "\n\nYou passed no changed_paths. If this quote comes from an engineering rule, pass the same files you gave " +
						"find_engineering_rules — rules are selected by path scope and are not in an unscoped context."
				}
				return fmt.Sprintf(
					"NOT VERIFIED. %q does not match any text retrieved for %s.\n\n"+
						"This means one of: the document wasn't among the context for this task, the quote isn't in the passage that was retrieved, "+
						"or the reference is ambiguous across repositories (qualify it as 'repository:path'). Do not present this as a citation.%s",
					args.Excerpt, args.Document, hint), nil
			}
			return fmt.Sprintf("VERIFIED (%s confidence)\n\nDocument: %s\nQuote: %q",
				ev.Confidence, ev.Qualified(), ev.Excerpt), nil
		},
	}
}

func searchMemory(src KnowledgeSource) mcp.Tool {
	return mcp.Tool{
		Name: "search_memory",
		Description: "Search the organization's indexed engineering knowledge — architecture documents, ADRs, RFCs, engineering rules, roadmaps — by keyword. " +
			"Returns ranked excerpts with the repository and path each came from. " +
			"Use this to find what has been written about a topic; use get_context when you have a task and want everything relevant to it.",
		InputSchema: object(map[string]any{
			"query": stringProp("Keywords to search for, e.g. 'kernel policy consumer driven'."),
			"limit": intProp("Maximum results to return (default 10)."),
		}, "query"),
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := decode(raw, &args); err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Query) == "" {
				return "", fmt.Errorf("query is required")
			}
			if args.Limit <= 0 {
				args.Limit = 10
			}

			results, err := src.Search(ctx, args.Query, memory.SearchOptions{Limit: args.Limit})
			if err != nil {
				return "", err
			}
			if len(results) == 0 {
				return fmt.Sprintf("No engineering knowledge matched %q.", args.Query), nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d result(s) for %q:\n\n", len(results), args.Query)
			for i, r := range results {
				fmt.Fprintf(&b, "%d. %s (score %.2f)\n   %s\n", i+1, qualify(r.Repository, r.Path), r.Score, clean(r.Snippet))
			}
			return b.String(), nil
		},
	}
}

func getContext(src KnowledgeSource) mcp.Tool {
	return mcp.Tool{
		Name: "get_context",
		Description: "Gather all engineering context relevant to a task — related documents, ADRs, and the engineering rules that govern it. " +
			"Describe the task in natural language. Pass changed_paths when the task concerns specific files, which scopes the rules to those that actually apply. " +
			"This is the broadest capability: prefer it when starting work on something.",
		InputSchema: object(map[string]any{
			"task":          stringProp("What you are about to do, e.g. 'add a caching layer to permission lookups'."),
			"changed_paths": stringArrayProp("Repository-relative files the task concerns, e.g. ['internal/authz/check.go']."),
		}, "task"),
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Task         string   `json:"task"`
				ChangedPaths []string `json:"changed_paths"`
			}
			if err := decode(raw, &args); err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Task) == "" {
				return "", fmt.Errorf("task is required")
			}

			pkg, err := src.ContextFor(ctx, args.Task, memory.ContextOptions{ChangedPaths: args.ChangedPaths})
			if err != nil {
				return "", err
			}
			return renderContext(args.Task, pkg), nil
		},
	}
}

func findEngineeringRules(src KnowledgeSource) mcp.Tool {
	return mcp.Tool{
		Name: "find_engineering_rules",
		Description: "Find the engineering rules that govern specific files. " +
			"Rules are selected by what they declare they govern (a rule's applies_to scope), not by keyword — a change rarely mentions the rule it breaks. " +
			"Use this before editing files, to learn the standards that apply to them.",
		InputSchema: object(map[string]any{
			"changed_paths": stringArrayProp("Repository-relative files you are about to change, e.g. ['internal/store/user.go']."),
			"task":          stringProp("Optional description of the change, used to order the rules by relevance."),
		}, "changed_paths"),
		Handler: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				ChangedPaths []string `json:"changed_paths"`
				Task         string   `json:"task"`
			}
			if err := decode(raw, &args); err != nil {
				return "", err
			}
			if len(args.ChangedPaths) == 0 {
				return "", fmt.Errorf("changed_paths is required and must not be empty")
			}

			task := args.Task
			if strings.TrimSpace(task) == "" {
				task = strings.Join(args.ChangedPaths, " ")
			}

			pkg, err := src.ContextFor(ctx, task, memory.ContextOptions{ChangedPaths: args.ChangedPaths})
			if err != nil {
				return "", err
			}
			if len(pkg.Rules) == 0 {
				return fmt.Sprintf("No engineering rule governs %s.\n\nThat is an answer, not an omission: no rule in this organization's rulebook declares it applies to these files.",
					strings.Join(args.ChangedPaths, ", ")), nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d rule(s) govern %s:\n\n", len(pkg.Rules), strings.Join(args.ChangedPaths, ", "))
			for _, r := range pkg.Rules {
				fmt.Fprintf(&b, "- %s\n  %s\n\n", qualify(r.Repository, r.Path), clean(r.Snippet))
			}
			return b.String(), nil
		},
	}
}

// renderContext formats a ContextPackage as text. Sections that are
// empty are named rather than omitted, because "no rule governs this"
// and "I didn't look" are different answers and a model cannot tell them
// apart from silence.
func renderContext(task string, pkg memory.ContextPackage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Engineering context for: %s\n", task)

	section := func(title string, files []memory.FileContext) {
		fmt.Fprintf(&b, "\n## %s\n", title)
		if len(files) == 0 {
			b.WriteString("(none)\n")
			return
		}
		for _, f := range files {
			fmt.Fprintf(&b, "- %s (score %.2f)\n  %s\n", qualify(f.Repository, f.Path), f.Score, clean(f.Snippet))
		}
	}

	section("Engineering rules governing these files", pkg.Rules)
	section("Architecture decision records", pkg.ADRs)
	section("Related documents", pkg.RelevantFiles)
	return b.String()
}

// qualify names a document as repository:path. A workspace holds several
// repositories and a path is unique only within one of them, so an
// unqualified "README.md" is ambiguous and uncitable.
func qualify(repository, path string) string {
	if repository == "" {
		return path
	}
	return repository + ":" + path
}

// clean strips the FTS5 highlight markers and line breaks a snippet
// carries for a terminal's benefit, which are noise in a model's
// context.
func clean(snippet string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(snippet, "**", "")), " ")
}

func decode(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return fmt.Errorf("arguments are required")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func object(props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func stringArrayProp(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": desc,
	}
}
