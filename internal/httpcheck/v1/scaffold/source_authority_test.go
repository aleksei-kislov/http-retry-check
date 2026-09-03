package scaffold

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestScaffoldPackageUsesOnlyEmbeddingAndFixedErrors(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "scaffold.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"embed": true, "errors": true}
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil || !allowed[path] {
			t.Fatalf("scaffold owner gained import %q", path)
		}
	}
	if len(parsed.Imports) != 2 {
		t.Fatalf("scaffold imports = %d", len(parsed.Imports))
	}
}
