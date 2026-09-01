package httpchecktest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
)

func TestExactTestingChildSourceAndPublicSurface(t *testing.T) {
	directory := testingSourceDirectory(t)
	wantFiles := map[string]bool{
		"doc.go": true, "check.go": true, "check_test.go": true, "source_authority_test.go": true,
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || !wantFiles[entry.Name()] {
			t.Errorf("unexpected package entry %s", entry.Name())
			continue
		}
		delete(wantFiles, entry.Name())
	}
	for name := range wantFiles {
		t.Errorf("missing required source %s", name)
	}

	exported := make(map[string]bool)
	wantImports := map[string]bool{
		"testing": true,
		"github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1": true,
	}
	for _, name := range []string{"doc.go", "check.go"} {
		path := filepath.Join(directory, name)
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, contents, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if parsed.Name.Name != "httpchecktest" {
			t.Errorf("%s package = %s", name, parsed.Name.Name)
		}
		for _, imported := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil || !wantImports[importPath] {
				t.Errorf("unexpected production import %q", imported.Path.Value)
				continue
			}
			delete(wantImports, importPath)
		}
		for _, declaration := range parsed.Decls {
			switch current := declaration.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(current.Name.Name) {
					exported[current.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, specification := range current.Specs {
					switch item := specification.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(item.Name.Name) {
							exported[item.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, identifier := range item.Names {
							if ast.IsExported(identifier.Name) {
								exported[identifier.Name] = true
							}
						}
					}
				}
			}
		}
		for _, forbidden := range []string{
			"fmt.", "log.", "os.", "net/http", "context.", "Error()", "%v", "%#v",
			"http.Default", "ProxyFromEnvironment", "/report",
		} {
			if strings.Contains(string(contents), forbidden) {
				t.Errorf("%s contains prohibited authority %q", name, forbidden)
			}
		}
	}
	for importPath := range wantImports {
		t.Errorf("missing production import %q", importPath)
	}
	if !reflect.DeepEqual(exported, map[string]bool{"Check": true}) {
		t.Fatalf("exported surface = %#v", exported)
	}
	checkType := reflect.TypeOf(Check)
	if checkType.NumIn() != 2 || checkType.In(0) != reflect.TypeOf((*testing.T)(nil)) ||
		checkType.In(1) != reflect.TypeOf((*httpcheck.Doer)(nil)).Elem() || checkType.NumOut() != 0 {
		t.Fatalf("Check signature = %v", checkType)
	}
}

func TestCheckLifecycleAndDiagnosticsStayExact(t *testing.T) {
	path := filepath.Join(testingSourceDirectory(t), "check.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	mutableGlobals := 0
	goStatements := 0
	var checkFunction *ast.FuncDecl
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.FuncDecl:
			if current.Name.Name == "Check" {
				checkFunction = current
			}
		case *ast.GenDecl:
			if current.Tok == token.VAR {
				mutableGlobals++
			}
		case *ast.GoStmt:
			goStatements++
		case *ast.CallExpr:
			selector, ok := current.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			name := owner.Name + "." + selector.Sel.Name
			counts[name]++
			if owner.Name == "t" &&
				(selector.Sel.Name == "Fatal" || selector.Sel.Name == "Error" ||
					selector.Sel.Name == "Log") && len(current.Args) != 1 {
				t.Errorf("%s has %d arguments", name, len(current.Args))
			}
		}
		return true
	})
	want := map[string]int{
		"httpcheck.Run": 1, "httpcheck.Validate": 1, "t.Helper": 1, "t.Context": 1,
		"t.Fatal": 3, "t.Error": 1, "t.Log": 1,
	}
	for name, count := range want {
		if counts[name] != count {
			t.Errorf("%s call count = %d, want %d", name, counts[name], count)
		}
	}
	if mutableGlobals != 0 || goStatements != 0 {
		t.Fatalf("mutable globals/go statements = %d/%d", mutableGlobals, goStatements)
	}
	if checkFunction == nil || checkFunction.Body == nil || len(checkFunction.Body.List) == 0 {
		t.Fatal("Check body is missing")
	}
	first, ok := checkFunction.Body.List[0].(*ast.ExprStmt)
	if !ok {
		t.Fatal("Check does not begin with t.Helper")
	}
	call, ok := first.X.(*ast.CallExpr)
	if !ok {
		t.Fatal("Check does not begin with t.Helper")
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatal("Check does not begin with exact t.Helper call")
	}
	owner, ownerOK := selector.X.(*ast.Ident)
	if !ownerOK || owner.Name != "t" || selector.Sel.Name != "Helper" || len(call.Args) != 0 {
		t.Fatal("Check does not begin with exact t.Helper call")
	}
	contents, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	source := string(contents)
	for _, required := range []string{
		`runFailureText     = "HTTP Retry Check scenario suite could not run."`,
		`invalidResultText  = "HTTP Retry Check scenario suite returned an invalid result."`,
		`positiveLinePrefix = "HTTP Retry Check PASS "`,
		`unsafeLinePrefix   = "HTTP Retry Check UNSAFE "`,
		`inconclusivePrefix = "HTTP Retry Check INCONCLUSIVE "`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("check source is missing fixed diagnostic %q", required)
		}
	}
}

func testingSourceDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Dir(filename)
}
