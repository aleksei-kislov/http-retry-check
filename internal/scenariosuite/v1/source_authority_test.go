package scenariosuite_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	suite "github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1"
)

func TestProductionSourceUsesOnlyApprovedCapabilities(t *testing.T) {
	directory := sourceDirectory(t)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	production := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("unexpected package entry %s", entry.Name())
			continue
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			production = append(production, entry.Name())
		}
	}
	sort.Strings(production)
	for _, required := range []string{"types.go", "evaluate.go", "request.go", "run.go", "origin.go"} {
		if index := sort.SearchStrings(production, required); index == len(production) || production[index] != required {
			t.Errorf("missing required source %s", required)
		}
	}

	allowedImports := map[string]bool{
		"bufio": true, "bytes": true, "context": true, "errors": true, "io": true,
		"net": true, "net/http": true, "net/netip": true, "sync": true, "time": true,
	}
	for _, name := range production {
		contents, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), name, contents, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, imported := range parsed.Imports {
			path, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil || !allowedImports[path] {
				t.Errorf("%s imports prohibited package %q", name, imported.Path.Value)
			}
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if ok && general.Tok == token.VAR {
				t.Errorf("%s declares package-global mutable state", name)
			}
		}
		for _, forbidden := range []string{
			"http.DefaultClient", "http.DefaultTransport", "http.ProxyFromEnvironment",
			"os.", "exec.", "tls.", "log.", "fmt.", "json.", "xml.",
		} {
			if strings.Contains(string(contents), forbidden) {
				t.Errorf("%s contains prohibited authority %q", name, forbidden)
			}
		}
	}
}

func TestPublicSurfaceMatchesExpectedAPI(t *testing.T) {
	directory := sourceDirectory(t)
	wantTypes := map[string]bool{
		"Doer": true, "ScenarioID": true, "Assessment": true, "CleanupState": true,
		"CredentialState": true, "FindingCode": true, "Observation": true,
		"ScenarioResult": true, "Result": true, "RunError": true,
	}
	wantFunctions := map[string]bool{"EvaluateObservation": true, "Run": true, "Validate": true}
	wantMethods := map[string]bool{"RunError.Error": true}
	wantValues := map[string]bool{
		"ScenarioAcceptThenDisconnect": true, "ScenarioDisconnectBeforeAcceptance": true,
		"ScenarioChangedBodyRetry": true, "ScenarioCrossOriginRedirectCredentials": true,
		"ScenarioRetryLimit": true, "ScenarioDelayedResponse": true,
		"AssessmentNoUnsafeBehaviorObserved": true, "AssessmentUnsafeBehaviorObserved": true,
		"AssessmentInconclusive": true, "CleanupSucceeded": true, "CleanupFailed": true,
		"CredentialNotObserved": true, "CredentialSourceOnly": true,
		"CredentialAbsentAtTarget": true, "CredentialExposedAtTarget": true,
		"CredentialMissing":         true,
		"FindingAttemptNotObserved": true, "FindingCaptureIncomplete": true,
		"FindingResponseIncomplete": true, "FindingDelayIncomplete": true,
		"FindingAttemptLimitExceeded": true, "FindingRetryBeforeResponse": true,
		"FindingRetryAfterAcceptedRequest":       true,
		"FindingRetryAfterUnconfirmedAcceptance": true, "FindingMethodChanged": true,
		"FindingDestinationChanged": true, "FindingBodyChanged": true,
		"FindingCredentialNotObserved": true, "FindingCredentialMissing": true,
		"FindingCredentialExposedAtTarget": true, "FindingEffectNotObserved": true,
		"FindingEffectLimitExceeded": true, "FindingCleanupUnverified": true,
		"FindingScenarioIncomplete": true, "ErrInvalidCall": true,
		"ErrSuiteUnavailable": true, "ErrInternalFailure": true, "ErrInvalidResult": true,
	}
	seenTypes := make(map[string]bool)
	seenFunctions := make(map[string]bool)
	seenMethods := make(map[string]bool)
	seenValues := make(map[string]bool)
	for _, name := range productionSourceFiles(t, directory) {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			switch current := declaration.(type) {
			case *ast.FuncDecl:
				if !ast.IsExported(current.Name.Name) {
					continue
				}
				if current.Recv == nil {
					seenFunctions[current.Name.Name] = true
					continue
				}
				receiver := receiverTypeName(current.Recv.List[0].Type)
				if ast.IsExported(receiver) {
					seenMethods[receiver+"."+current.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, specification := range current.Specs {
					switch item := specification.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(item.Name.Name) {
							seenTypes[item.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, identifier := range item.Names {
							if ast.IsExported(identifier.Name) {
								seenValues[identifier.Name] = true
							}
						}
					}
				}
			}
		}
	}
	assertExactNames(t, "types", wantTypes, seenTypes)
	assertExactNames(t, "functions", wantFunctions, seenFunctions)
	assertExactNames(t, "methods", wantMethods, seenMethods)
	assertExactNames(t, "values", wantValues, seenValues)

	assertStructFields(t, reflect.TypeOf(suite.Observation{}), []string{
		"CaptureComplete", "AttemptCount", "EffectCount", "OverlapCount",
		"RetryAfterEffectCount", "RetryAfterUnconfirmedCount", "RetryBeforeResponseCount",
		"ResponseAttemptCount", "ResponseCompleteCount", "FirstResponseComplete",
		"DelayCompleteCount", "MethodConsistent", "DestinationConsistent",
		"BodyConsistent", "Credential", "Cleanup",
	})
	assertStructFieldTypes(t, reflect.TypeOf(suite.Observation{}), []reflect.Type{
		reflect.TypeOf(false), reflect.TypeOf(uint32(0)), reflect.TypeOf(uint64(0)),
		reflect.TypeOf(uint32(0)), reflect.TypeOf(uint32(0)), reflect.TypeOf(uint32(0)),
		reflect.TypeOf(uint32(0)), reflect.TypeOf(uint32(0)), reflect.TypeOf(uint32(0)),
		reflect.TypeOf(false), reflect.TypeOf(uint32(0)), reflect.TypeOf(false),
		reflect.TypeOf(false), reflect.TypeOf(false), reflect.TypeOf(suite.CredentialState("")),
		reflect.TypeOf(suite.CleanupState("")),
	})
	assertStructFields(t, reflect.TypeOf(suite.ScenarioResult{}), []string{
		"Scenario", "Assessment", "Observation", "Findings",
	})
	assertStructFieldTypes(t, reflect.TypeOf(suite.ScenarioResult{}), []reflect.Type{
		reflect.TypeOf(suite.ScenarioID("")), reflect.TypeOf(suite.Assessment("")),
		reflect.TypeOf(suite.Observation{}), reflect.TypeOf([]suite.FindingCode{}),
	})
	assertStructFields(t, reflect.TypeOf(suite.Result{}), []string{"Assessment", "Scenarios"})
	assertStructFieldTypes(t, reflect.TypeOf(suite.Result{}), []reflect.Type{
		reflect.TypeOf(suite.Assessment("")), reflect.TypeOf([]suite.ScenarioResult{}),
	})
	for name, value := range map[string]reflect.Type{
		"ScenarioID":      reflect.TypeOf(suite.ScenarioID("")),
		"Assessment":      reflect.TypeOf(suite.Assessment("")),
		"CleanupState":    reflect.TypeOf(suite.CleanupState("")),
		"CredentialState": reflect.TypeOf(suite.CredentialState("")),
		"FindingCode":     reflect.TypeOf(suite.FindingCode("")),
	} {
		if value.Kind() != reflect.String {
			t.Fatalf("%s underlying kind = %v", name, value.Kind())
		}
	}
	if reflect.TypeOf(suite.RunError(0)).Kind() != reflect.Uint8 {
		t.Fatalf("RunError underlying kind = %v", reflect.TypeOf(suite.RunError(0)).Kind())
	}

	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	doerType := reflect.TypeOf((*suite.Doer)(nil)).Elem()
	requestType := reflect.TypeOf((*http.Request)(nil))
	responseType := reflect.TypeOf((*http.Response)(nil))
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	method := doerType.Method(0)
	if doerType.NumMethod() != 1 || method.Name != "Do" || method.Type.NumIn() != 1 ||
		method.Type.In(0) != requestType || method.Type.NumOut() != 2 ||
		method.Type.Out(0) != responseType || method.Type.Out(1) != errorType {
		t.Fatalf("Doer shape = %v", doerType)
	}
	runType := reflect.TypeOf(suite.Run)
	if runType.NumIn() != 2 || runType.In(0) != contextType || runType.In(1) != doerType ||
		runType.NumOut() != 2 || runType.Out(0) != reflect.TypeOf(suite.Result{}) ||
		runType.Out(1) != errorType {
		t.Fatalf("Run signature = %v", runType)
	}
	validateType := reflect.TypeOf(suite.Validate)
	if validateType.NumIn() != 1 || validateType.In(0) != reflect.TypeOf(suite.Result{}) ||
		validateType.NumOut() != 1 || validateType.Out(0) != errorType {
		t.Fatalf("Validate signature = %v", validateType)
	}
	evaluateType := reflect.TypeOf(suite.EvaluateObservation)
	if evaluateType.NumIn() != 2 || evaluateType.In(0) != reflect.TypeOf(suite.ScenarioID("")) ||
		evaluateType.In(1) != reflect.TypeOf(suite.Observation{}) || evaluateType.NumOut() != 2 ||
		evaluateType.Out(0) != reflect.TypeOf(suite.ScenarioResult{}) || evaluateType.Out(1).Kind() != reflect.Bool {
		t.Fatalf("EvaluateObservation signature = %v", evaluateType)
	}
	runErrorType := reflect.TypeOf(suite.RunError(0))
	errorMethod, ok := runErrorType.MethodByName("Error")
	if !ok || runErrorType.NumMethod() != 1 || errorMethod.Type.NumIn() != 1 ||
		errorMethod.Type.In(0) != runErrorType || errorMethod.Type.NumOut() != 1 ||
		errorMethod.Type.Out(0).Kind() != reflect.String {
		t.Fatalf("RunError method set = %v", runErrorType)
	}
}

func receiverTypeName(expression ast.Expr) string {
	switch current := expression.(type) {
	case *ast.Ident:
		return current.Name
	case *ast.StarExpr:
		return receiverTypeName(current.X)
	default:
		return ""
	}
}

func sourceDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate source directory")
	}
	return filepath.Dir(filename)
}

func productionSourceFiles(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") &&
			!strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files
}

func assertExactNames(t *testing.T, kind string, expected, actual map[string]bool) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("exported %s = %v, want %v", kind, actual, expected)
	}
}

func assertStructFields(t *testing.T, value reflect.Type, fields []string) {
	t.Helper()
	if value.NumField() != len(fields) {
		t.Fatalf("%s field count = %d", value.Name(), value.NumField())
	}
	for index, name := range fields {
		field := value.Field(index)
		if field.Name != name || field.Tag != "" {
			t.Fatalf("%s field %d = %s/%q", value.Name(), index, field.Name, field.Tag)
		}
	}
}

func assertStructFieldTypes(t *testing.T, value reflect.Type, fields []reflect.Type) {
	t.Helper()
	if value.NumField() != len(fields) {
		t.Fatalf("%s type field count = %d", value.Name(), value.NumField())
	}
	for index, expected := range fields {
		if value.Field(index).Type != expected {
			t.Fatalf("%s field %d type = %v, want %v",
				value.Name(), index, value.Field(index).Type, expected)
		}
	}
}
