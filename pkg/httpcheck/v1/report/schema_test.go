package report

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalReportsAndManifestSatisfyV1Schemas(t *testing.T) {
	reportSchema := loadSchema(t, "http-retry-check-report.schema.json")
	manifestSchema := loadSchema(t, "http-retry-check-artifact-manifest.schema.json")
	if reportSchema["$id"] != SchemaID ||
		manifestSchema["$id"] != "urn:http-retry-check:schema:artifact-manifest:v1" {
		t.Fatal("schema identity drift")
	}
	for _, result := range []sourceResult{positiveResult(), unsafeResult(), inconclusiveResult(), mixedResult()} {
		value := mustReport(t, result)
		reportBytes, _ := Encode(value)
		var reportJSON any
		if err := json.Unmarshal(reportBytes, &reportJSON); err != nil || !schemaValid(reportSchema, reportSchema, reportJSON) {
			t.Fatalf("canonical report rejected by schema: %v", err)
		}
		files, _ := BuildArtifact(value)
		var manifestJSON any
		if err := json.Unmarshal(files[0].Contents, &manifestJSON); err != nil ||
			!schemaValid(manifestSchema, manifestSchema, manifestJSON) {
			t.Fatalf("canonical manifest rejected by schema: %v", err)
		}
	}
}

func TestSchemasRejectStructuralChangesAndNativeValidationChecksSemantics(t *testing.T) {
	schema := loadSchema(t, "http-retry-check-report.schema.json")
	encoded, _ := Encode(mustReport(t, unsafeResult()))
	var base map[string]any
	if err := json.Unmarshal(encoded, &base); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(map[string]any)
	}{
		{"unknown root", func(v map[string]any) { v["unknown"] = true }},
		{"missing root", func(v map[string]any) { delete(v, "suite_identity") }},
		{"identity", func(v map[string]any) { v["schema_version"] = sanitationMarker }},
		{"outcome", func(v map[string]any) { v["outcome"] = "success" }},
		{"negative summary", func(v map[string]any) { v["summary"].(map[string]any)["passed"] = float64(-1) }},
		{"negative observation", func(v map[string]any) {
			v["scenarios"].([]any)[0].(map[string]any)["observation"].(map[string]any)["attempt_count"] = float64(-1)
		}},
		{"scenario order", func(v map[string]any) {
			rows := v["scenarios"].([]any)
			rows[0], rows[1] = rows[1], rows[0]
		}},
		{"extra scenario", func(v map[string]any) {
			rows := v["scenarios"].([]any)
			v["scenarios"] = append(rows, cloneJSON(rows[0]))
		}},
		{"unknown observation", func(v map[string]any) {
			v["scenarios"].([]any)[0].(map[string]any)["observation"].(map[string]any)["unknown"] = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneJSON(base).(map[string]any)
			test.edit(candidate)
			if schemaValid(schema, schema, candidate) {
				t.Fatal("schema accepted structural drift")
			}
		})
	}

	semantic := cloneJSON(base).(map[string]any)
	semantic["summary"].(map[string]any)["passed"] = float64(4)
	if !schemaValid(schema, schema, semantic) {
		t.Fatal("structural schema unexpectedly derived full summary semantics")
	}
	semanticBytes, err := json.MarshalIndent(semantic, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	semanticBytes = append(semanticBytes, '\n')
	if _, err := Decode(semanticBytes); err != invalidReport {
		t.Fatalf("native decode accepted schema-only semantic drift: %v", err)
	}
}

func loadSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate schema test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
	contents, err := os.ReadFile(filepath.Join(root, "schemas", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(contents, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func schemaValid(root, schema any, value any) bool {
	if boolean, ok := schema.(bool); ok {
		return boolean
	}
	node, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	if reference, exists := node["$ref"].(string); exists {
		const prefix = "#/$defs/"
		definitions, definitionsOK := root.(map[string]any)["$defs"].(map[string]any)
		if !definitionsOK || !strings.HasPrefix(reference, prefix) ||
			!schemaValid(root, definitions[strings.TrimPrefix(reference, prefix)], value) {
			return false
		}
	}
	if constant, exists := node["const"]; exists && !reflect.DeepEqual(constant, value) {
		return false
	}
	if enum, exists := node["enum"].([]any); exists {
		matched := false
		for _, candidate := range enum {
			matched = matched || reflect.DeepEqual(candidate, value)
		}
		if !matched {
			return false
		}
	}
	if typeName, exists := node["type"].(string); exists && !schemaTypeMatches(typeName, value) {
		return false
	}
	if text, isString := value.(string); isString {
		if minimum, exists := node["minLength"].(float64); exists && len([]rune(text)) < int(minimum) {
			return false
		}
		if maximum, exists := node["maxLength"].(float64); exists && len([]rune(text)) > int(maximum) {
			return false
		}
		if pattern, exists := node["pattern"].(string); exists {
			compiled, err := regexp.Compile(pattern)
			if err != nil || !compiled.MatchString(text) {
				return false
			}
		}
	}
	if object, isObject := value.(map[string]any); isObject {
		properties, _ := node["properties"].(map[string]any)
		if node["additionalProperties"] == false {
			for key := range object {
				if _, known := properties[key]; !known {
					return false
				}
			}
		}
		if required, exists := node["required"].([]any); exists {
			for _, key := range required {
				if _, present := object[key.(string)]; !present {
					return false
				}
			}
		}
		for key, childSchema := range properties {
			if child, present := object[key]; present && !schemaValid(root, childSchema, child) {
				return false
			}
		}
	}
	if array, isArray := value.([]any); isArray {
		if minimum, exists := node["minItems"].(float64); exists && len(array) < int(minimum) {
			return false
		}
		if maximum, exists := node["maxItems"].(float64); exists && len(array) > int(maximum) {
			return false
		}
		prefix, _ := node["prefixItems"].([]any)
		for index, item := range array {
			if index < len(prefix) {
				if !schemaValid(root, prefix[index], item) {
					return false
				}
				continue
			}
			if items, exists := node["items"]; exists && !schemaValid(root, items, item) {
				return false
			}
		}
		if node["uniqueItems"] == true {
			for left := range array {
				for right := left + 1; right < len(array); right++ {
					if reflect.DeepEqual(array[left], array[right]) {
						return false
					}
				}
			}
		}
	}
	if number, isNumber := value.(float64); isNumber {
		if minimum, exists := node["minimum"].(float64); exists && number < minimum {
			return false
		}
		if maximum, exists := node["maximum"].(float64); exists && number > maximum {
			return false
		}
	}
	if children, exists := node["allOf"].([]any); exists {
		for _, child := range children {
			if !schemaValid(root, child, value) {
				return false
			}
		}
	}
	if children, exists := node["oneOf"].([]any); exists {
		matches := 0
		for _, child := range children {
			if schemaValid(root, child, value) {
				matches++
			}
		}
		if matches != 1 {
			return false
		}
	}
	if condition, exists := node["if"]; exists && schemaValid(root, condition, value) {
		if then, exists := node["then"]; exists && !schemaValid(root, then, value) {
			return false
		}
	}
	return true
}

func schemaTypeMatches(typeName string, value any) bool {
	switch typeName {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && math.Trunc(number) == number
	default:
		return false
	}
}

func cloneJSON(value any) any {
	encoded, _ := json.Marshal(value)
	var cloned any
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}
