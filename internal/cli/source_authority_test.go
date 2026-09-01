package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestFocusedCLITopologyAndImports(t *testing.T) {
	wantFiles := map[string]bool{
		"cli.go": true, "cli_test.go": true,
		"help.go": true, "help_test.go": true,
		"http.go": true, "http_corpus_test.go": true,
		"http_source_authority_test.go": true, "http_test.go": true,
		"source_authority_test.go": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || !wantFiles[entry.Name()] {
			t.Errorf("unexpected CLI package entry %s", entry.Name())
			continue
		}
		delete(wantFiles, entry.Name())
	}
	for missing := range wantFiles {
		t.Errorf("missing CLI package entry %s", missing)
	}

	wantImports := map[string][]string{
		"cli.go":  {"context", "io"},
		"help.go": {"io", "regexp", "runtime/debug", "strings"},
		"http.go": {
			"errors", "io",
			"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence",
			"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/localfs",
		},
	}
	for name, want := range wantImports {
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, parser.AllErrors)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		got := make([]string, 0, len(parsed.Imports))
		for _, imported := range parsed.Imports {
			path, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				t.Fatal(unquoteErr)
			}
			got = append(got, path)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s imports = %q, want %q", name, got, want)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.GoStmt:
				t.Errorf("%s starts a goroutine", name)
			case *ast.ChanType:
				t.Errorf("%s declares a channel", name)
			}
			return true
		})
	}
}

func TestFocusedCLIRoutesAndHelpTopics(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "cli.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	run := cliFindFunction(t, parsed, "Run")
	routes := make([]string, 0, 6)
	ast.Inspect(run.Body, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expression := range clause.List {
			literal, literalOK := expression.(*ast.BasicLit)
			if !literalOK || literal.Kind != token.STRING {
				t.Error("top-level CLI route is not a string literal")
				continue
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				t.Fatal(unquoteErr)
			}
			routes = append(routes, value)
		}
		return false
	})
	wantRoutes := []string{"help", "-h", "--help", "version", "--version", "http"}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("top-level CLI routes = %q, want %q", routes, wantRoutes)
	}

	for _, name := range []string{"cli.go", "help.go"} {
		contents, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, removed := range []string{
			`case "doctor"`, `case "run"`, `case "explain"`, `case "minimize"`,
			`case "compare"`, `case "bind-report"`, `case "validate"`,
			`case "normalize"`, `case "migrate"`, `case "diagnose"`,
			`case "pack-validate"`, `case "suite-validate"`, `case "suite-run"`,
			`case "init"`, `case "pack-run"`,
		} {
			if strings.Contains(string(contents), removed) {
				t.Errorf("%s retains removed route marker %q", name, removed)
			}
		}
	}

	if _, ok := helpTopic("help"); !ok {
		t.Error("help topic is missing")
	}
	if _, ok := helpTopic("version"); !ok {
		t.Error("version topic is missing")
	}
	if _, ok := helpTopic("http"); !ok {
		t.Error("HTTP topic is missing")
	}
	for _, removed := range []string{"doctor", "run", "pack-run", "init", "validate"} {
		if topic, ok := helpTopic(removed); ok || topic != "" {
			t.Errorf("removed help topic %q = %q/%t", removed, topic, ok)
		}
	}
}

func cliFindFunction(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}
