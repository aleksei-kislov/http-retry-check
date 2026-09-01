package report

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const rootPackagePath = "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"

func TestPublicSurfaceAndProductionDependencies(t *testing.T) {
	directory := reportSourceDirectory(t)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := map[string]bool{
		"artifact.go": true, "artifact_test.go": true, "github.go": true,
		"corpus_test.go": true,
		"json.go":        true, "json_fuzz_test.go": true, "json_test.go": true,
		"junit.go": true, "projection_test.go": true, "report.go": true,
		"report_test.go": true, "schema_test.go": true, "source_authority_test.go": true,
		"test_helpers_test.go": true, "types.go": true,
	}
	production := make([]string, 0, 6)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || !wantFiles[entry.Name()] {
			t.Errorf("unexpected package entry %s", entry.Name())
			continue
		}
		delete(wantFiles, entry.Name())
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			production = append(production, entry.Name())
		}
	}
	for missing := range wantFiles {
		t.Errorf("missing package entry %s", missing)
	}
	sort.Strings(production)
	if !reflect.DeepEqual(production, []string{"artifact.go", "github.go", "json.go", "junit.go", "report.go", "types.go"}) {
		t.Fatalf("production files = %v", production)
	}

	wantFunctions := map[string]bool{
		"New": true, "Validate": true, "Encode": true, "Decode": true,
		"JUnit": true, "GitHubSummary": true, "BuildArtifact": true, "ValidateArtifact": true,
	}
	wantTypes := map[string]bool{
		"Outcome": true, "Summary": true, "Observation": true, "Finding": true,
		"Scenario": true, "Report": true, "ArtifactFile": true,
	}
	wantConstants := map[string]bool{
		"SchemaVersion": true, "SchemaID": true, "SuiteIdentity": true,
		"ExplanationIdentity": true, "ClaimCeiling": true, "MaxProjectionBytes": true,
		"MaxArtifactBytes": true, "OutcomePass": true, "OutcomeFail": true,
		"OutcomeInconclusive": true,
	}
	seenFunctions := make(map[string]bool)
	seenTypes := make(map[string]bool)
	seenConstants := make(map[string]bool)
	seenPrivateErrorMethod := false
	allowedImports := map[string]bool{
		"bytes": true, "crypto/sha256": true, "encoding/binary": true,
		"encoding/hex": true, "encoding/json": true, "encoding/xml": true,
		"errors": true, "io": true, "strconv": true, "strings": true,
		rootPackagePath: true,
	}
	for _, name := range production {
		contents, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, contents, 0)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Name.Name != "report" {
			t.Errorf("%s package = %s", name, parsed.Name.Name)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil || !allowedImports[path] {
				t.Errorf("%s imports unapproved authority %q", name, path)
			}
		}
		for _, declaration := range parsed.Decls {
			switch current := declaration.(type) {
			case *ast.FuncDecl:
				if current.Recv == nil && current.Name.Name == "init" {
					t.Errorf("%s declares init", name)
				}
				if current.Recv == nil && ast.IsExported(current.Name.Name) {
					seenFunctions[current.Name.Name] = true
					if !wantFunctions[current.Name.Name] {
						t.Errorf("unexpected exported function %s", current.Name.Name)
					}
				}
				if current.Recv != nil && ast.IsExported(current.Name.Name) {
					validPrivateError := current.Name.Name == "Error" && len(current.Recv.List) == 1 &&
						identifierName(current.Recv.List[0].Type) == "packageError" &&
						(current.Type.Params == nil || len(current.Type.Params.List) == 0) &&
						current.Type.Results != nil && len(current.Type.Results.List) == 1 &&
						identifierName(current.Type.Results.List[0].Type) == "string"
					if !validPrivateError || seenPrivateErrorMethod {
						t.Errorf("unexpected exported receiver method %s", current.Name.Name)
					} else {
						seenPrivateErrorMethod = true
					}
				}
			case *ast.GenDecl:
				if current.Tok == token.VAR {
					t.Errorf("%s declares package-global mutable state", name)
				}
				for _, raw := range current.Specs {
					switch specification := raw.(type) {
					case *ast.TypeSpec:
						if specification.Assign.IsValid() {
							t.Errorf("%s declares type alias %s", name, specification.Name.Name)
						}
						if ast.IsExported(specification.Name.Name) {
							seenTypes[specification.Name.Name] = true
							if !wantTypes[specification.Name.Name] {
								t.Errorf("unexpected exported type %s", specification.Name.Name)
							}
						}
					case *ast.ValueSpec:
						for _, valueName := range specification.Names {
							if !ast.IsExported(valueName.Name) {
								continue
							}
							if current.Tok != token.CONST {
								t.Errorf("unexpected exported non-constant %s", valueName.Name)
							}
							seenConstants[valueName.Name] = true
							if !wantConstants[valueName.Name] {
								t.Errorf("unexpected exported constant %s", valueName.Name)
							}
							isOutcome := strings.HasPrefix(valueName.Name, "Outcome")
							if isOutcome && identifierName(specification.Type) != "Outcome" {
								t.Errorf("outcome %s is not explicitly typed", valueName.Name)
							}
							if !isOutcome && specification.Type != nil {
								t.Errorf("constant %s unexpectedly typed", valueName.Name)
							}
						}
					}
				}
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if _, ok := node.(*ast.GoStmt); ok {
				t.Errorf("%s starts a goroutine", name)
			}
			return true
		})
		for _, forbidden := range []string{
			"context.", "os.", "filepath.", "net.", "http.", "time.", "runtime.",
			"exec.", "syscall.", "unsafe.", "log.", "fmt.", "sync.", "reflect.",
			"internal/scenariosuite/", "v0alpha1", "v0alpha2", "trusted_same_process", "isolated",
		} {
			if strings.Contains(string(contents), forbidden) {
				t.Errorf("%s contains prohibited authority %q", name, forbidden)
			}
		}
	}
	if !reflect.DeepEqual(seenFunctions, wantFunctions) || !reflect.DeepEqual(seenTypes, wantTypes) ||
		!reflect.DeepEqual(seenConstants, wantConstants) || !seenPrivateErrorMethod {
		t.Fatalf("surface = functions %v, types %v, constants %v", seenFunctions, seenTypes, seenConstants)
	}
}

func TestPublicSignaturesConstantsAndStructShapes(t *testing.T) {
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	reportType := reflect.TypeOf(Report{})
	bytesType := reflect.TypeOf([]byte{})
	artifactType := reflect.TypeOf([]ArtifactFile{})
	rootResultType := reflect.TypeOf(New).In(0)
	if rootResultType.PkgPath() != rootPackagePath || rootResultType.Name() != "Result" {
		t.Fatalf("New root input = %v", rootResultType)
	}
	tests := []struct {
		name     string
		function any
		inputs   []reflect.Type
		outputs  []reflect.Type
	}{
		{"New", New, []reflect.Type{rootResultType}, []reflect.Type{reportType, errorType}},
		{"Validate", Validate, []reflect.Type{reportType}, []reflect.Type{errorType}},
		{"Encode", Encode, []reflect.Type{reportType}, []reflect.Type{bytesType, errorType}},
		{"Decode", Decode, []reflect.Type{bytesType}, []reflect.Type{reportType, errorType}},
		{"JUnit", JUnit, []reflect.Type{reportType}, []reflect.Type{bytesType, errorType}},
		{"GitHubSummary", GitHubSummary, []reflect.Type{reportType}, []reflect.Type{bytesType, errorType}},
		{"BuildArtifact", BuildArtifact, []reflect.Type{reportType}, []reflect.Type{artifactType, errorType}},
		{"ValidateArtifact", ValidateArtifact, []reflect.Type{artifactType}, []reflect.Type{errorType}},
	}
	for _, test := range tests {
		typeOf := reflect.TypeOf(test.function)
		if typeOf.NumIn() != len(test.inputs) || typeOf.NumOut() != len(test.outputs) {
			t.Fatalf("%s signature = %v", test.name, typeOf)
		}
		for index, input := range test.inputs {
			if typeOf.In(index) != input {
				t.Fatalf("%s input %d = %v", test.name, index, typeOf.In(index))
			}
		}
		for index, output := range test.outputs {
			if typeOf.Out(index) != output {
				t.Fatalf("%s output %d = %v", test.name, index, typeOf.Out(index))
			}
		}
	}
	if SchemaVersion != "http_retry_check.report.v1" ||
		SchemaID != "urn:http-retry-check:schema:report:v1" ||
		SuiteIdentity != "http_retry_check.scenario_suite.v1" ||
		ExplanationIdentity != "http_retry_check.scenario_explanations.v1" ||
		ClaimCeiling != "This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations." ||
		MaxProjectionBytes != 262144 || MaxArtifactBytes != 1048576 || maxFindings != 18 ||
		maxJSONDepth != 8 || maxJSONObjectKeys != 32 || maxJSONArrayItems != 32 ||
		OutcomePass != "pass" || OutcomeFail != "fail" || OutcomeInconclusive != "inconclusive" ||
		reflect.TypeOf(Outcome("")).Kind() != reflect.String || reflect.TypeOf(Outcome("")).NumMethod() != 0 {
		t.Fatal("public constants or Outcome drift")
	}

	stringType := reflect.TypeOf("")
	uint32Type := reflect.TypeOf(uint32(0))
	uint64Type := reflect.TypeOf(uint64(0))
	boolType := reflect.TypeOf(false)
	assertExactStruct(t, reflect.TypeOf(Summary{}), []expectedField{
		{"Scenarios", uint32Type, "scenarios"}, {"Passed", uint32Type, "passed"},
		{"Failed", uint32Type, "failed"}, {"Inconclusive", uint32Type, "inconclusive"},
	})
	observationType := reflect.TypeOf(Observation{})
	assertExactStruct(t, observationType, []expectedField{
		{"CaptureComplete", boolType, "capture_complete"}, {"AttemptCount", uint32Type, "attempt_count"},
		{"EffectCount", uint64Type, "effect_count"}, {"OverlapCount", uint32Type, "overlap_count"},
		{"RetryAfterEffectCount", uint32Type, "retry_after_effect_count"},
		{"RetryAfterUnconfirmedCount", uint32Type, "retry_after_unconfirmed_count"},
		{"RetryBeforeResponseCount", uint32Type, "retry_before_response_count"},
		{"ResponseAttemptCount", uint32Type, "response_attempt_count"},
		{"ResponseCompleteCount", uint32Type, "response_complete_count"},
		{"FirstResponseComplete", boolType, "first_response_complete"},
		{"DelayCompleteCount", uint32Type, "delay_complete_count"},
		{"MethodConsistent", boolType, "method_consistent"},
		{"DestinationConsistent", boolType, "destination_consistent"},
		{"BodyConsistent", boolType, "body_consistent"},
		{"Credential", rootNamedType(t, "CredentialState"), "credential"},
		{"Cleanup", rootNamedType(t, "CleanupState"), "cleanup"},
	})
	assertExactStruct(t, reflect.TypeOf(Finding{}), []expectedField{
		{"Code", rootNamedType(t, "FindingCode"), "code"}, {"Text", stringType, "text"},
	})
	assertExactStruct(t, reflect.TypeOf(Scenario{}), []expectedField{
		{"Scenario", rootNamedType(t, "ScenarioID"), "scenario"}, {"ScenarioText", stringType, "scenario_text"},
		{"Assessment", rootNamedType(t, "Assessment"), "assessment"}, {"AssessmentText", stringType, "assessment_text"},
		{"Observation", observationType, "observation"}, {"Findings", reflect.TypeOf([]Finding{}), "findings"},
	})
	assertExactStruct(t, reportType, []expectedField{
		{"SchemaVersion", stringType, "schema_version"}, {"SuiteIdentity", stringType, "suite_identity"},
		{"ExplanationIdentity", stringType, "explanation_identity"}, {"ClaimCeiling", stringType, "claim_ceiling"},
		{"Assessment", rootNamedType(t, "Assessment"), "assessment"}, {"AssessmentText", stringType, "assessment_text"},
		{"Outcome", reflect.TypeOf(Outcome("")), "outcome"}, {"Summary", reflect.TypeOf(Summary{}), "summary"},
		{"Scenarios", reflect.TypeOf([]Scenario{}), "scenarios"},
	})
	assertExactStruct(t, reflect.TypeOf(ArtifactFile{}), []expectedField{
		{"Name", stringType, ""}, {"MediaType", stringType, ""}, {"Contents", bytesType, ""},
	})
}

func reportSourceDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate report source")
	}
	return filepath.Dir(filename)
}

type expectedField struct {
	name   string
	typeOf reflect.Type
	json   string
}

func assertExactStruct(t *testing.T, typeOf reflect.Type, expected []expectedField) {
	t.Helper()
	if typeOf.Kind() != reflect.Struct || typeOf.NumField() != len(expected) || typeOf.NumMethod() != 0 {
		t.Fatalf("%s shape or methods drift", typeOf.Name())
	}
	for index, want := range expected {
		field := typeOf.Field(index)
		wantTag := ""
		if want.json != "" {
			wantTag = `json:"` + want.json + `"`
		}
		if field.Name != want.name || field.Type != want.typeOf || string(field.Tag) != wantTag || field.PkgPath != "" {
			t.Fatalf("%s field %d = %s/%v/%q", typeOf.Name(), index, field.Name, field.Type, field.Tag)
		}
	}
}

func rootNamedType(t *testing.T, name string) reflect.Type {
	t.Helper()
	types := []reflect.Type{
		reflect.TypeOf(Report{}).Field(4).Type,
		reflect.TypeOf(Scenario{}).Field(0).Type,
		reflect.TypeOf(Finding{}).Field(0).Type,
		reflect.TypeOf(Observation{}).Field(14).Type,
		reflect.TypeOf(Observation{}).Field(15).Type,
	}
	for _, typeOf := range types {
		if typeOf.PkgPath() == rootPackagePath && typeOf.Name() == name {
			return typeOf
		}
	}
	t.Fatalf("root type %s not found", name)
	return nil
}

func identifierName(expression ast.Expr) string {
	if value, ok := expression.(*ast.Ident); ok {
		return value.Name
	}
	return ""
}
