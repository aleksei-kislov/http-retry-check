package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPCLIUsesOnlyEvidenceScaffoldAndFilesystemPackages(t *testing.T) {
	httpAssertCLIHasOnlyStaticEvidenceScaffoldAndFilesystemAuthority(t)
}

func httpAssertCLIHasOnlyStaticEvidenceScaffoldAndFilesystemAuthority(t *testing.T) {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "http.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	wantImports := []string{
		"errors",
		"io",
		"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence",
		"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/localfs",
	}
	gotImports := make([]string, 0, len(parsed.Imports))
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		gotImports = append(gotImports, path)
	}
	if !reflect.DeepEqual(gotImports, wantImports) {
		t.Fatalf("HTTP CLI imports = %#v, want %#v", gotImports, wantImports)
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.GoStmt:
			t.Error("HTTP CLI starts a goroutine")
		case *ast.ChanType:
			t.Error("HTTP CLI gains channel authority")
		case *ast.FuncLit:
			t.Error("HTTP CLI gains callback authority")
		case *ast.TypeAssertExpr:
			t.Error("HTTP CLI gains type-assertion authority")
		}
		return true
	})
	contents, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"pkg/httpcheck", "internal/scenariosuite", "internal/application", "internal/origin", "internal/runner",
		`"context"`, `"net"`, `"net/http"`, `"os"`, `"os/exec"`, `"reflect"`, `"runtime"`,
		`"syscall"`, `"unsafe"`, "exec.Command", "Getenv", "LookupEnv", "http run",
	} {
		if strings.Contains(string(contents), forbidden) {
			t.Fatalf("HTTP CLI contains forbidden runtime marker %q", forbidden)
		}
	}

	cliContents, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(cliContents), `case "http":`) != 1 ||
		strings.Count(string(cliContents), "return httpCommand(args, stdin, stdout, stderr)") != 1 {
		t.Fatal("HTTP CLI does not have exactly one focused static route")
	}
}
