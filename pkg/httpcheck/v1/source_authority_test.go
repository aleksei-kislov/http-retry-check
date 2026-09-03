package httpcheck

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
	"time"
)

func TestProductionSourceAndPublicSurface(t *testing.T) {
	directory := sourceDirectory(t)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	production := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			if entry.Name() != "report" && entry.Name() != "testing" {
				t.Errorf("unexpected package directory %s", entry.Name())
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("unexpected package entry %s", entry.Name())
			continue
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			production = append(production, entry.Name())
		}
	}
	sort.Strings(production)
	for _, required := range []string{"doc.go", "evaluate.go", "explain.go", "options.go", "run.go", "types.go"} {
		if index := sort.SearchStrings(production, required); index == len(production) || production[index] != required {
			t.Errorf("missing required source %s", required)
		}
	}

	exported := make(map[string]bool)
	topLevelFunctions := make(map[string]bool)
	runErrorMethods := 0
	packageVariables := 0
	wantTopLevelFunctions := map[string]bool{
		"Run": true, "Validate": true, "ScenarioText": true,
		"AssessmentText": true, "FindingText": true, "WithScenarioTimeout": true,
		"WithConnectionTimeout": true, "WithQuietWindow": true, "WithAttemptLimit": true,
	}
	allowedImports := map[string]bool{
		"context":  true,
		"errors":   true,
		"net/http": true,
		"time":     true,
		"github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1": true,
	}
	seenImports := make(map[string]bool)
	for _, name := range production {
		path := filepath.Join(directory, name)
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, contents, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if parsed.Name.Name != "httpcheck" {
			t.Errorf("%s package = %s", name, parsed.Name.Name)
		}
		for _, imported := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil || !allowedImports[importPath] {
				t.Errorf("unexpected production import %q", imported.Path.Value)
				continue
			}
			seenImports[importPath] = true
		}
		for _, declaration := range parsed.Decls {
			switch current := declaration.(type) {
			case *ast.FuncDecl:
				if current.Recv != nil {
					if !exactRunErrorMethod(current) {
						t.Errorf("%s declares unexpected receiver method %s", name, current.Name.Name)
					} else {
						runErrorMethods++
					}
					if ast.IsExported(current.Name.Name) {
						exported[current.Name.Name] = true
					}
					continue
				}
				if current.Name.Name == "init" {
					t.Errorf("%s declares init", name)
				}
				if ast.IsExported(current.Name.Name) {
					exported[current.Name.Name] = true
					topLevelFunctions[current.Name.Name] = true
					if !wantTopLevelFunctions[current.Name.Name] {
						t.Errorf("%s declares unexpected exported function %s", name, current.Name.Name)
					}
				}
			case *ast.GenDecl:
				if current.Tok == token.VAR {
					packageVariables++
					t.Errorf("%s declares package-level variable state", name)
				}
				for _, specification := range current.Specs {
					switch item := specification.(type) {
					case *ast.TypeSpec:
						if item.Assign.IsValid() {
							t.Errorf("exported or internal type alias %s is forbidden", item.Name.Name)
						}
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
		source := string(contents)
		for _, forbidden := range []string{
			"reflect.", "unsafe.", "http.Default", "ProxyFromEnvironment", "net.Listen",
			"os.", "exec.", "fmt.", "log.", "testing/", "/report",
		} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s contains prohibited authority %q", name, forbidden)
			}
		}
	}
	for importPath := range allowedImports {
		if !seenImports[importPath] {
			t.Errorf("missing required production import %q", importPath)
		}
	}

	wantExports := map[string]bool{
		"Doer": true, "ScenarioID": true,
		"ScenarioAcceptThenDisconnect": true, "ScenarioDisconnectBeforeAcceptance": true,
		"ScenarioChangedBodyRetry": true, "ScenarioCrossOriginRedirectCredentials": true,
		"ScenarioRetryLimit": true, "ScenarioDelayedResponse": true,
		"Assessment": true, "AssessmentNoUnsafeBehaviorObserved": true,
		"AssessmentUnsafeBehaviorObserved": true, "AssessmentInconclusive": true,
		"CleanupState": true, "CleanupSucceeded": true, "CleanupFailed": true,
		"CredentialState": true, "CredentialNotObserved": true, "CredentialSourceOnly": true,
		"CredentialAbsentAtTarget": true, "CredentialExposedAtTarget": true, "CredentialMissing": true,
		"FindingCode": true, "FindingAttemptNotObserved": true, "FindingCaptureIncomplete": true,
		"FindingResponseIncomplete": true, "FindingDelayIncomplete": true,
		"FindingAttemptLimitExceeded": true, "FindingRetryBeforeResponse": true,
		"FindingRetryAfterAcceptedRequest": true, "FindingRetryAfterUnconfirmedAcceptance": true,
		"FindingMethodChanged": true, "FindingDestinationChanged": true, "FindingBodyChanged": true,
		"FindingCredentialNotObserved": true, "FindingCredentialMissing": true,
		"FindingCredentialExposedAtTarget": true, "FindingEffectNotObserved": true,
		"FindingEffectLimitExceeded": true, "FindingCleanupUnverified": true,
		"FindingScenarioIncomplete": true,
		"Observation":               true, "ScenarioResult": true, "Result": true, "RunError": true,
		"Option":         true,
		"ErrInvalidCall": true, "ErrSuiteUnavailable": true, "ErrInternalFailure": true,
		"ErrInvalidResult": true, "Error": true, "Run": true, "Validate": true,
		"ScenarioText": true, "AssessmentText": true, "FindingText": true,
		"WithScenarioTimeout": true, "WithConnectionTimeout": true,
		"WithQuietWindow": true, "WithAttemptLimit": true,
	}
	if !reflect.DeepEqual(exported, wantExports) {
		t.Fatalf("exported surface = %#v, want %#v", exported, wantExports)
	}
	if !reflect.DeepEqual(topLevelFunctions, wantTopLevelFunctions) {
		t.Fatalf("top-level functions = %#v, want %#v", topLevelFunctions, wantTopLevelFunctions)
	}
	if runErrorMethods != 1 || packageVariables != 0 {
		t.Fatalf("RunError methods/package variables = %d/%d, want 1/0", runErrorMethods, packageVariables)
	}
}

func TestFunctionAndStructSignaturesMatchExpectedAPI(t *testing.T) {
	doerType := reflect.TypeOf((*Doer)(nil)).Elem()
	requestType := reflect.TypeOf((*http.Request)(nil))
	responseType := reflect.TypeOf((*http.Response)(nil))
	method, ok := doerType.MethodByName("Do")
	if !ok || method.Type.NumIn() != 1 || method.Type.In(0) != requestType ||
		method.Type.NumOut() != 2 || method.Type.Out(0) != responseType ||
		method.Type.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatalf("Doer.Do signature = %v", method.Type)
	}
	runType := reflect.TypeOf(Run)
	if !runType.IsVariadic() || runType.NumIn() != 3 ||
		runType.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() ||
		runType.In(1) != doerType || runType.In(2) != reflect.TypeOf([]Option{}) || runType.NumOut() != 2 ||
		runType.Out(0) != reflect.TypeOf(Result{}) ||
		runType.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatalf("Run signature = %v", runType)
	}
	validateType := reflect.TypeOf(Validate)
	if validateType.NumIn() != 1 || validateType.In(0) != reflect.TypeOf(Result{}) ||
		validateType.NumOut() != 1 || validateType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatalf("Validate signature = %v", validateType)
	}
	optionType := reflect.TypeOf(Option(nil))
	if optionType.Kind() != reflect.Func || optionType.NumIn() != 1 ||
		optionType.In(0) != reflect.TypeOf((*options)(nil)) || optionType.NumOut() != 0 {
		t.Fatalf("Option signature = %v", optionType)
	}
	for name, function := range map[string]any{
		"WithScenarioTimeout":   WithScenarioTimeout,
		"WithConnectionTimeout": WithConnectionTimeout,
		"WithQuietWindow":       WithQuietWindow,
	} {
		functionType := reflect.TypeOf(function)
		if functionType.NumIn() != 1 || functionType.In(0) != reflect.TypeOf(time.Duration(0)) ||
			functionType.NumOut() != 1 || functionType.Out(0) != optionType {
			t.Errorf("%s signature = %v", name, functionType)
		}
	}
	attemptOptionType := reflect.TypeOf(WithAttemptLimit)
	if attemptOptionType.NumIn() != 1 || attemptOptionType.In(0) != reflect.TypeOf(uint32(0)) ||
		attemptOptionType.NumOut() != 1 || attemptOptionType.Out(0) != optionType {
		t.Errorf("WithAttemptLimit signature = %v", attemptOptionType)
	}
	assertFields(t, reflect.TypeOf(Observation{}), []fieldSpec{
		{"CaptureComplete", reflect.TypeOf(false)}, {"AttemptCount", reflect.TypeOf(uint32(0))},
		{"AttemptLimit", reflect.TypeOf(uint32(0))}, {"Protocol", reflect.TypeOf("")},
		{"EffectCount", reflect.TypeOf(uint64(0))}, {"OverlapCount", reflect.TypeOf(uint32(0))},
		{"RetryAfterEffectCount", reflect.TypeOf(uint32(0))},
		{"RetryAfterUnconfirmedCount", reflect.TypeOf(uint32(0))},
		{"RetryBeforeResponseCount", reflect.TypeOf(uint32(0))},
		{"ResponseAttemptCount", reflect.TypeOf(uint32(0))},
		{"ResponseCompleteCount", reflect.TypeOf(uint32(0))},
		{"FirstResponseComplete", reflect.TypeOf(false)},
		{"DelayCompleteCount", reflect.TypeOf(uint32(0))},
		{"MethodConsistent", reflect.TypeOf(false)}, {"DestinationConsistent", reflect.TypeOf(false)},
		{"BodyConsistent", reflect.TypeOf(false)}, {"Credential", reflect.TypeOf(CredentialState(""))},
		{"Cleanup", reflect.TypeOf(CleanupState(""))},
	})
	assertFields(t, reflect.TypeOf(ScenarioResult{}), []fieldSpec{
		{"Scenario", reflect.TypeOf(ScenarioID(""))}, {"Assessment", reflect.TypeOf(Assessment(""))},
		{"Observation", reflect.TypeOf(Observation{})}, {"Findings", reflect.TypeOf([]FindingCode{})},
	})
	assertFields(t, reflect.TypeOf(Result{}), []fieldSpec{
		{"Assessment", reflect.TypeOf(Assessment(""))}, {"Scenarios", reflect.TypeOf([]ScenarioResult{})},
	})
	assertExactReceiverMethodSets(t)
}

func exactRunErrorMethod(function *ast.FuncDecl) bool {
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 ||
		function.Name == nil || function.Name.Name != "Error" || function.Type == nil ||
		function.Type.TypeParams != nil ||
		(function.Type.Params != nil && len(function.Type.Params.List) != 0) ||
		function.Type.Results == nil || len(function.Type.Results.List) != 1 {
		return false
	}
	receiver, receiverOK := function.Recv.List[0].Type.(*ast.Ident)
	result, resultOK := function.Type.Results.List[0].Type.(*ast.Ident)
	return receiverOK && receiver.Name == "RunError" && resultOK && result.Name == "string"
}

func assertExactReceiverMethodSets(t *testing.T) {
	t.Helper()
	errorMethod, ok := reflect.TypeOf(RunError(0)).MethodByName("Error")
	if !ok || reflect.TypeOf(RunError(0)).NumMethod() != 1 ||
		reflect.TypeOf((*RunError)(nil)).NumMethod() != 1 || errorMethod.Type.NumIn() != 1 ||
		errorMethod.Type.In(0) != reflect.TypeOf(RunError(0)) || errorMethod.Type.NumOut() != 1 ||
		errorMethod.Type.Out(0) != reflect.TypeOf("") {
		t.Fatalf("RunError receiver method set = %v", reflect.TypeOf(RunError(0)))
	}
	withoutMethods := []reflect.Type{
		reflect.TypeOf(ScenarioID("")), reflect.TypeOf(Assessment("")),
		reflect.TypeOf(CleanupState("")), reflect.TypeOf(CredentialState("")),
		reflect.TypeOf(FindingCode("")), reflect.TypeOf(Option(nil)), reflect.TypeOf(Observation{}),
		reflect.TypeOf(ScenarioResult{}), reflect.TypeOf(Result{}),
	}
	for _, value := range withoutMethods {
		if value.NumMethod() != 0 || reflect.PointerTo(value).NumMethod() != 0 {
			t.Errorf("%s unexpectedly owns receiver methods", value)
		}
	}
}

type fieldSpec struct {
	name   string
	typeOf reflect.Type
}

func assertFields(t *testing.T, value reflect.Type, want []fieldSpec) {
	t.Helper()
	if value.NumField() != len(want) {
		t.Fatalf("%s field count = %d, want %d", value, value.NumField(), len(want))
	}
	for index, field := range want {
		observed := value.Field(index)
		if observed.Name != field.name || observed.Type != field.typeOf || !observed.IsExported() ||
			observed.Tag != "" {
			t.Errorf("%s field %d = %s %s %q, want %s %s", value, index, observed.Name,
				observed.Type, observed.Tag, field.name, field.typeOf)
		}
	}
}

func sourceDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Dir(filename)
}
