package report

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalJSONRoundTripAndDetachment(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		result func() Report
	}{
		{"positive", func() Report { return mustReport(t, positiveResult()) }},
		{"unsafe", func() Report { return mustReport(t, unsafeResult()) }},
		{"inconclusive", func() Report { return mustReport(t, inconclusiveResult()) }},
		{"mixed", func() Report { return mustReport(t, mixedResult()) }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value := fixture.result()
			before := cloneReport(value)
			encoded, err := Encode(value)
			if err != nil || len(encoded) == 0 || len(encoded) > MaxProjectionBytes ||
				encoded[len(encoded)-1] != '\n' || !bytes.HasPrefix(encoded, []byte("{\n  \"schema_version\": ")) ||
				bytes.HasSuffix(encoded, []byte("\n\n")) {
				t.Fatalf("canonical envelope = %q/%v", encoded, err)
			}
			decoded, err := Decode(encoded)
			if err != nil || !reflect.DeepEqual(decoded, value) || !reflect.DeepEqual(value, before) {
				t.Fatalf("round trip = %#v/%v", decoded, err)
			}
			encoded[0] = '['
			decoded.Scenarios[0].Findings = append(decoded.Scenarios[0].Findings,
				Finding{Code: "caller_mutation", Text: sanitationMarker})
			if !reflect.DeepEqual(value, before) {
				t.Fatal("decode aliases bytes or report")
			}
		})
	}
}

func TestDecodeRejectsNoncanonicalAndInvalidReports(t *testing.T) {
	value := mustReport(t, unsafeResult())
	canonical, _ := Encode(value)
	compact := new(bytes.Buffer)
	if err := json.Compact(compact, canonical); err != nil {
		t.Fatal(err)
	}
	semantic := cloneReport(value)
	semantic.Outcome = OutcomePass
	semanticBytes, _ := marshalCanonicalJSON(semantic)
	nilFindings := cloneReport(value)
	nilFindings.Scenarios[0].Findings = nil
	nilBytes, _ := marshalCanonicalJSON(nilFindings)
	invalidUTF8 := append([]byte{}, canonical...)
	identityAt := bytes.Index(invalidUTF8, []byte(SuiteIdentity))
	invalidUTF8[identityAt] = 0xff
	first := []byte("  \"schema_version\": \"" + SchemaVersion + "\",\n" +
		"  \"suite_identity\": \"" + SuiteIdentity + "\",\n")
	reordered := []byte("  \"suite_identity\": \"" + SuiteIdentity + "\",\n" +
		"  \"schema_version\": \"" + SchemaVersion + "\",\n")
	tests := map[string][]byte{
		"empty":             {},
		"oversized":         bytes.Repeat([]byte("x"), MaxProjectionBytes+1),
		"invalid utf8":      invalidUTF8,
		"bom":               append([]byte{0xef, 0xbb, 0xbf}, canonical...),
		"leading space":     append([]byte(" "), canonical...),
		"compact":           compact.Bytes(),
		"field order":       bytes.Replace(canonical, first, reordered, 1),
		"missing lf":        canonical[:len(canonical)-1],
		"extra lf":          append(append([]byte{}, canonical...), '\n'),
		"trailing":          append(append([]byte{}, canonical...), []byte("{}\n")...),
		"duplicate root":    bytes.Replace(canonical, []byte("  \"schema_version\": "), []byte("  \"schema_version\": \"duplicate\",\n  \"schema_version\": "), 1),
		"escaped duplicate": bytes.Replace(canonical, []byte("  \"schema_version\": "), []byte("  \"schema\\u005fversion\": \"duplicate\",\n  \"schema_version\": "), 1),
		"duplicate nested":  bytes.Replace(canonical, []byte("      \"capture_complete\": "), []byte("      \"capture_complete\": true,\n      \"capture_complete\": "), 1),
		"unknown root":      bytes.Replace(canonical, []byte("  \"schema_version\": "), []byte("  \"unknown\": true,\n  \"schema_version\": "), 1),
		"unknown nested":    bytes.Replace(canonical, []byte("      \"capture_complete\": "), []byte("      \"unknown\": true,\n      \"capture_complete\": "), 1),
		"missing field":     bytes.Replace(canonical, []byte("  \"suite_identity\": \""+SuiteIdentity+"\",\n"), nil, 1),
		"null field":        bytes.Replace(canonical, []byte("\"claim_ceiling\": \""+ClaimCeiling+"\""), []byte("\"claim_ceiling\": null"), 1),
		"wrong string":      bytes.Replace(canonical, []byte("\"outcome\": \"fail\""), []byte("\"outcome\": 1"), 1),
		"wrong bool":        bytes.Replace(canonical, []byte("\"capture_complete\": true"), []byte("\"capture_complete\": \"true\""), 1),
		"fraction":          bytes.Replace(canonical, []byte("\"attempt_count\": 3"), []byte("\"attempt_count\": 3.0"), 1),
		"exponent":          bytes.Replace(canonical, []byte("\"attempt_count\": 3"), []byte("\"attempt_count\": 3e0"), 1),
		"negative":          bytes.Replace(canonical, []byte("\"attempt_count\": 3"), []byte("\"attempt_count\": -1"), 1),
		"overflow":          bytes.Replace(canonical, []byte("\"attempt_count\": 3"), []byte("\"attempt_count\": 4294967296"), 1),
		"null array":        nilBytes,
		"semantic drift":    semanticBytes,
		"v0alpha1":          bytes.ReplaceAll(canonical, []byte(".v1"), []byte(".v0alpha1")),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			decoded, err := Decode(input)
			if !reflect.DeepEqual(decoded, Report{}) || err != invalidReport || strings.Contains(err.Error(), sanitationMarker) {
				t.Fatalf("Decode = %#v/%v", decoded, err)
			}
		})
	}
}

func TestDecodeEnforcesJSONSizeAndDepthLimits(t *testing.T) {
	input := []byte(strings.Repeat(`{"value":`, 8) + `{"value":"end"}` + strings.Repeat("}", 8))
	if validateJSONStructure(input) {
		t.Fatal("depth beyond fixed bound accepted")
	}
	items := `"x",`
	over := []byte(`{"values":[` + strings.Repeat(items, maxJSONArrayItems) + `"x"]}`)
	if validateJSONStructure(over) {
		t.Fatal("array beyond fixed bound accepted")
	}
}

func TestDecodeRejectsNegativeUnsignedFields(t *testing.T) {
	canonical, err := Encode(mustReport(t, unsafeResult()))
	if err != nil {
		t.Fatal(err)
	}
	fields := []string{
		"scenarios", "passed", "failed", "inconclusive", "attempt_count", "effect_count",
		"overlap_count", "retry_after_effect_count", "retry_after_unconfirmed_count",
		"retry_before_response_count", "response_attempt_count", "response_complete_count",
		"delay_complete_count",
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			prefix := []byte(`"` + field + `": `)
			if field == "scenarios" {
				prefix = []byte("\n    \"scenarios\": ")
			}
			at := bytes.Index(canonical, prefix)
			if at < 0 {
				t.Fatal("field not found")
			}
			start := at + len(prefix)
			end := start
			for end < len(canonical) && canonical[end] >= '0' && canonical[end] <= '9' {
				end++
			}
			candidate := append([]byte{}, canonical[:start]...)
			candidate = append(candidate, "-1"...)
			candidate = append(candidate, canonical[end:]...)
			if value, err := Decode(candidate); err != invalidReport || !reflect.DeepEqual(value, Report{}) {
				t.Fatalf("negative %s accepted: %#v/%v", field, value, err)
			}
		})
	}
}
