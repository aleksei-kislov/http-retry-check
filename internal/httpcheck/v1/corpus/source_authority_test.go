package corpus

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestCorpusAuditDoesNotUseRuntimeOrReportingPackages(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(path, "github.com/aleksei-kislov/http-retry-check/") ||
				path == "net/http" || path == "net" || path == "os/exec" || path == "plugin" || path == "unsafe" {
				t.Fatalf("neutral audit %s imports forbidden authority %q", entry.Name(), path)
			}
		}
	}
}

func TestOfflineGeneratorRequiresBothExplicitGates(t *testing.T) {
	contents, err := os.ReadFile("generate_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(contents, []byte("//go:build corpusgen\n")) ||
		!bytes.Contains(contents, []byte(`os.Getenv("HTTP_RETRY_CHECK_REGENERATE_CORPUS") != "1"`)) ||
		!bytes.Contains(contents, []byte(`os.Getenv("HTTP_RETRY_CHECK_CORPUS_OUTPUT")`)) ||
		!bytes.Contains(contents, []byte(`os.Lstat(root)`)) ||
		bytes.Contains(contents, []byte("exec.Command")) || bytes.Contains(contents, []byte("http.")) ||
		bytes.Contains(contents, []byte("net.Dial")) {
		t.Fatal("offline generator gate or authority drift")
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "generate_test.go", contents, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "init" || function.Name.Name == "TestMain") {
			t.Fatalf("offline generator declares forbidden %s", function.Name.Name)
		}
	}
}

func TestLogicalCaseIDsContainNoProducerOrRuntimeValue(t *testing.T) {
	rows, _ := loadRows(t)
	results, _ := loadResults(t, rows)
	ids := make([]string, 0, len(rows)+len(results)+160)
	for id := range rows {
		ids = append(ids, id)
	}
	for id := range results {
		ids = append(ids, id)
	}
	for _, path := range []string{"invalid/semantic.json", "invalid/report-model.json", "invalid/canonical-json.json",
		"invalid/artifact.json", "invalid/sanitation.json", "invalid/authority.json"} {
		var bundle invalidBundle
		readCanonical(t, path, &bundle)
		for _, candidate := range bundle.Cases {
			ids = append(ids, candidate.ID)
		}
	}
	for _, id := range ids {
		for _, part := range strings.Split(id, "_") {
			switch part {
			case "go", "csharp", "client", "host", "path", "timing", "windows", "linux", "darwin", "macos":
				t.Fatalf("logical case ID %q contains forbidden producer/platform/runtime-value vocabulary", id)
			}
		}
	}
}

func TestCorpusManifestSchemaDefinesTheExpectedShape(t *testing.T) {
	path := filepath.Join(corpusRoot, "..", "..", "..", "schemas", "v1", "http-retry-check-conformance-corpus-manifest.schema.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil {
		t.Fatal(err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" ||
		schema["$id"] != "urn:http-retry-check:schema:conformance-corpus-manifest:v1" ||
		schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("corpus manifest schema root drift: %#v", schema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 6 {
		t.Fatalf("corpus manifest schema properties drift: %#v", schema["properties"])
	}
	consts := map[string]string{
		"schema_version": manifestIdentity, "product_identity": productIdentity,
		"suite_identity": suiteIdentity, "report_identity": reportIdentity,
		"conformance_identity": conformanceIdentity,
	}
	for name, value := range consts {
		property, ok := properties[name].(map[string]any)
		if !ok || property["const"] != value {
			t.Fatalf("schema property %s does not own %q: %#v", name, value, property)
		}
	}
	files, ok := properties["files"].(map[string]any)
	if !ok || files["type"] != "array" || files["minItems"] != json.Number("1") {
		t.Fatalf("schema files property drift: %#v", properties["files"])
	}
	items, ok := files["items"].(map[string]any)
	if !ok || items["$ref"] != "#/$defs/file" {
		t.Fatalf("schema files items drift: %#v", files["items"])
	}
	definitions, ok := schema["$defs"].(map[string]any)
	if !ok || len(definitions) != 1 {
		t.Fatalf("schema definitions drift: %#v", schema["$defs"])
	}
	file, ok := definitions["file"].(map[string]any)
	if !ok || file["type"] != "object" || file["additionalProperties"] != false {
		t.Fatalf("schema file descriptor drift: %#v", file)
	}
	required, ok := file["required"].([]any)
	if !ok || len(required) != 3 || required[0] != "path" || required[1] != "size" || required[2] != "sha256" {
		t.Fatalf("schema file descriptor requirements drift: %#v", file["required"])
	}
	fileProperties, ok := file["properties"].(map[string]any)
	if !ok || len(fileProperties) != 3 {
		t.Fatalf("schema file descriptor properties drift: %#v", file["properties"])
	}
	pathProperty, pathOK := fileProperties["path"].(map[string]any)
	sizeProperty, sizeOK := fileProperties["size"].(map[string]any)
	hashProperty, hashOK := fileProperties["sha256"].(map[string]any)
	pathNot, notOK := pathProperty["not"].(map[string]any)
	if !pathOK || !sizeOK || !hashOK || !notOK ||
		pathProperty["type"] != "string" || pathProperty["minLength"] != json.Number("1") ||
		pathProperty["pattern"] != "^[A-Za-z0-9._-]*[A-Za-z0-9_-](?:/[A-Za-z0-9._-]*[A-Za-z0-9_-])*$" ||
		pathNot["pattern"] != `(^|/)([Cc][Oo][Nn]|[Pp][Rr][Nn]|[Aa][Uu][Xx]|[Nn][Uu][Ll]|[Cc][Oo][Mm][1-9]|[Ll][Pp][Tt][1-9])(\.[A-Za-z0-9._-]*)?($|/)` ||
		sizeProperty["type"] != "integer" || sizeProperty["minimum"] != json.Number("1") ||
		hashProperty["type"] != "string" || hashProperty["pattern"] != "^[0-9a-f]{64}$" {
		t.Fatalf("schema file descriptor constraint drift: %#v", fileProperties)
	}
	pathPattern := regexp.MustCompile(pathProperty["pattern"].(string))
	reservedPattern := regexp.MustCompile(pathNot["pattern"].(string))
	for _, value := range []string{"invalid/semantic.json", ".hidden/file", "A_b-1.2"} {
		if !pathPattern.MatchString(value) || reservedPattern.MatchString(value) {
			t.Fatalf("schema path constraints reject portable value %q", value)
		}
	}
	for _, value := range []string{"", "/absolute", "trailing/", "double//segment", ".", "..", "...", "a/../b", "a/.../b", "trailing.", "dir/trailing.", "CON", "aux.txt", "dir/NUL", "dir/COM1.log", "dir/lpt9", `a\\b`, "a:b", "é", "e\u0301", "control\nname"} {
		if pathPattern.MatchString(value) && !reservedPattern.MatchString(value) {
			t.Fatalf("schema path constraints admit ambiguous value %q", value)
		}
	}
}

func TestPortableCorpusPathRejectsFilesystemAmbiguity(t *testing.T) {
	for _, value := range []string{"invalid/semantic.json", ".hidden/file", "A_b-1.2"} {
		if !portableCorpusPath(value) {
			t.Fatalf("portable corpus path rejected: %q", value)
		}
	}
	for _, value := range []string{"", "/absolute", "trailing/", "double//segment", ".", "..", "...", "a/../b", "a/.../b", "trailing.", "dir/trailing.", "CON", "aux.txt", "dir/NUL", "dir/COM1.log", "dir/lpt9", `a\\b`, "a:b", "é", "e\u0301", "control\nname"} {
		if portableCorpusPath(value) {
			t.Fatalf("ambiguous corpus path admitted: %q", value)
		}
	}
	upper, upperOK := portableCorpusPathKey("Results/Case.json")
	lower, lowerOK := portableCorpusPathKey("results/case.json")
	if !upperOK || !lowerOK || upper != lower {
		t.Fatalf("case-fold aliases are not bound to one key: %q %q", upper, lower)
	}
}
