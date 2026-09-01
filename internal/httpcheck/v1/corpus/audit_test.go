package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var corpusRoot = filepath.Join("..", "..", "..", "..", "conformance", "http-retry-check", "v1")

func TestRootManifestRecursivelyBindsCorpus(t *testing.T) {
	var manifest corpusManifest
	manifestBytes := readCanonical(t, "manifest.json", &manifest)
	if len(manifestBytes) == 0 || manifest.SchemaVersion != manifestIdentity ||
		manifest.ProductIdentity != productIdentity || manifest.SuiteIdentity != suiteIdentity ||
		manifest.ReportIdentity != reportIdentity || manifest.ConformanceIdentity != conformanceIdentity ||
		manifest.Files == nil || len(manifest.Files) != 30 {
		t.Fatalf("root manifest identity/shape drift: %#v", manifest)
	}

	actual := make(map[string]corpusFile, len(manifest.Files))
	err := filepath.WalkDir(corpusRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in corpus")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("non-regular corpus entry")
		}
		relative, err := filepath.Rel(corpusRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "manifest.json" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(contents)
		actual[relative] = corpusFile{Path: relative, Size: uint64(len(contents)), SHA256: hex.EncodeToString(digest[:])}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(manifest.Files) {
		t.Fatalf("manifest has %d descriptors for %d regular files", len(manifest.Files), len(actual))
	}
	previous := ""
	pathKeys := make(map[string]string, len(manifest.Files))
	for index, descriptor := range manifest.Files {
		pathKey, portable := portableCorpusPathKey(descriptor.Path)
		if !portable || descriptor.Size == 0 || !lowerSHA256(descriptor.SHA256) ||
			(index != 0 && !(previous < descriptor.Path)) {
			t.Fatalf("descriptor %d is not canonical: %#v", index, descriptor)
		}
		if other, exists := pathKeys[pathKey]; exists {
			t.Fatalf("descriptor paths %q and %q are filesystem aliases", other, descriptor.Path)
		}
		pathKeys[pathKey] = descriptor.Path
		if got, ok := actual[descriptor.Path]; !ok || got != descriptor {
			t.Fatalf("descriptor %q does not bind the current regular file: got %#v", descriptor.Path, got)
		}
		previous = descriptor.Path
	}
}

func TestCaptureCorpusIsCanonicalAndBounded(t *testing.T) {
	var bundle captureBundle
	contents := readCanonical(t, "capture/request-wires.json", &bundle)
	if len(contents) == 0 || bundle.SchemaVersion != captureBundleIdentity ||
		bundle.Endpoint != "127.0.0.1:41001" || bundle.Cases == nil || len(bundle.Cases) != 53 {
		t.Fatalf("capture corpus identity/shape drift: %#v", bundle)
	}
	seen := make(map[string]bool, len(bundle.Cases))
	for _, candidate := range bundle.Cases {
		if !closedID(candidate.ID) || seen[candidate.ID] || candidate.WireBase64 == "" {
			t.Fatalf("invalid capture case descriptor: %#v", candidate)
		}
		seen[candidate.ID] = true
		wire, err := base64.StdEncoding.Strict().DecodeString(candidate.WireBase64)
		if err != nil || len(wire) == 0 || len(wire) > maxCaptureCorpusWireBytes {
			t.Fatalf("capture case %q wire is invalid or unbounded: %v/%d", candidate.ID, err, len(wire))
		}
		clear(wire)
	}
	for _, required := range []string{
		"marker_in_malformed_head", "marker_in_malformed_raw_target", "marker_in_truncated_head", "marker_in_body_only",
		"raw_target_invalid_percent", "raw_target_absolute_form", "raw_target_delete_control",
		"content_length_leading_zero", "content_length_lexical_duplicate",
		"body_changed_then_truncated",
		"chunk_trailer_value_delete_control",
		"transfer_encoding_unicode_kelvin",
		"chunk_extension_quoted_valid", "chunk_extension_bare_semicolon", "chunk_extension_name_malformed",
		"chunk_extension_value_delete_control", "chunk_extension_quoted_unclosed", "chunk_extension_empty_token_value",
	} {
		if !seen[required] {
			t.Fatalf("capture corpus missing required case %q", required)
		}
	}
}

func TestSemanticCorpusContainsOnlyExpectedCases(t *testing.T) {
	rows, rowCategories := loadRows(t)
	results, resultCategories := loadResults(t, rows)
	if len(rows) != 42 {
		t.Fatalf("row count = %d, want 42", len(rows))
	}
	if len(results) != 14 {
		t.Fatalf("result count = %d, want 14", len(results))
	}

	positiveTuples := 0
	seenFindings := make(map[string]bool)
	seenCredentials := make(map[string]bool)
	seenCleanup := make(map[string]bool)
	seenCoverage := make(map[string]bool)
	unsafeScenario := make(map[string]bool)
	inconclusiveScenario := make(map[string]bool)
	for id, row := range rows {
		if !closedID(id) || row.ID != id || !equalStrings(row.Representability, []string{"go", "csharp"}) ||
			row.Findings == nil || row.Coverage == nil || len(row.Coverage) == 0 || !uniqueStrings(row.Coverage) {
			t.Fatalf("row metadata is not closed for %q: %#v", id, row)
		}
		if !validObservation(row.Scenario, row.Observation) {
			t.Fatalf("row %q has an invalid observation", id)
		}
		assessment, findings := assess(row.Scenario, row.Observation)
		if row.Assessment != assessment || !equalStrings(row.Findings, findings) {
			t.Fatalf("row %q derived %q/%v, fixture has %q/%v", id, assessment, findings, row.Assessment, row.Findings)
		}
		if row.Assessment == assessmentPositive {
			positiveTuples++
			if rowCategories[id] != "positive" || len(row.Findings) != 0 {
				t.Fatalf("positive row %q is misclassified", id)
			}
		} else if rowCategories[id] == "positive" {
			t.Fatalf("non-positive row %q is in the positive bundle", id)
		}
		for _, finding := range row.Findings {
			seenFindings[finding] = true
		}
		seenCredentials[row.Observation.Credential] = true
		seenCleanup[row.Observation.Cleanup] = true
		for _, coverage := range row.Coverage {
			seenCoverage[coverage] = true
		}
	}
	if positiveTuples != 9 {
		t.Fatalf("positive tuple count = %d, want 9", positiveTuples)
	}
	for _, finding := range findingOrder[:17] {
		if !seenFindings[finding] {
			t.Errorf("reachable finding %q lacks a valid row", finding)
		}
	}
	if seenFindings[findingOrder[17]] {
		t.Error("scenario_incomplete was manufactured as a reachable valid row")
	}
	for _, state := range []string{"not_observed", "source_only", "absent_at_target", "exposed_at_target", "missing"} {
		if !seenCredentials[state] {
			t.Errorf("credential state %q lacks a reachable row", state)
		}
	}
	for _, state := range []string{"succeeded", "failed"} {
		if !seenCleanup[state] {
			t.Errorf("cleanup state %q lacks a reachable row", state)
		}
	}

	for id, result := range results {
		if !closedID(id) || result.ID != id || !equalStrings(result.Representability, []string{"go", "csharp"}) ||
			len(result.Rows) != len(scenarios) || result.Coverage == nil || len(result.Coverage) == 0 ||
			!uniqueStrings(result.Rows) || !uniqueStrings(result.Coverage) {
			t.Fatalf("result metadata is not closed for %q: %#v", id, result)
		}
		aggregate := assessmentPositive
		for index, rowID := range result.Rows {
			row, ok := rows[rowID]
			if !ok || row.Scenario != scenarios[index] {
				t.Fatalf("result %q row %d has invalid reference %q", id, index, rowID)
			}
			aggregate = combineAssessment(aggregate, row.Assessment)
		}
		if result.Assessment != aggregate || resultCategories[id] != categoryForAssessment(id, aggregate) {
			t.Fatalf("result %q aggregate/category = %q/%q, want %q", id, result.Assessment, resultCategories[id], aggregate)
		}
		if strings.HasPrefix(id, "result_unsafe_") {
			unsafeScenario[strings.TrimPrefix(id, "result_unsafe_")] = true
		}
		if strings.HasPrefix(id, "result_inconclusive_") {
			inconclusiveScenario[strings.TrimPrefix(id, "result_inconclusive_")] = true
		}
		for _, coverage := range result.Coverage {
			seenCoverage[coverage] = true
		}
	}
	for _, scenario := range scenarios {
		if !unsafeScenario[scenario] || !inconclusiveScenario[scenario] {
			t.Errorf("scenario %q lacks unsafe/inconclusive aggregate coverage", scenario)
		}
		for _, cleanup := range []string{"succeeded", "failed"} {
			id := "unrun_" + scenario + "_cleanup_" + cleanup
			if _, ok := rows[id]; !ok {
				t.Errorf("missing reachable unrun row %q", id)
			}
		}
	}
	mixed := results["result_mixed_unsafe_over_inconclusive"]
	if mixed.Assessment != assessmentUnsafe || !seenCoverage["precedence.unsafe_over_inconclusive"] {
		t.Fatal("mixed unsafe-over-inconclusive precedence is not covered")
	}

	requiredCoverage := []string{
		"field.capture_complete.lower", "field.capture_complete.upper", "field.attempt_count.max",
		"field.effect_count.max", "field.overlap_count.max", "field.retry_after_effect_count.max",
		"field.retry_after_unconfirmed_count.max", "field.retry_before_response_count.max",
		"field.response_attempt_count.max", "field.response_complete_count.max",
		"field.first_response_complete.lower", "field.first_response_complete.upper",
		"field.delay_complete_count.max", "field.method_consistent.lower",
		"field.destination_consistent.lower", "field.body_consistent.lower",
		"field.response_later_complete_first_incomplete", "field.counts.lower",
		"precedence.unsafe_over_inconclusive",
	}
	for _, coverage := range requiredCoverage {
		if !seenCoverage[coverage] {
			t.Errorf("required valid boundary coverage %q is absent", coverage)
		}
	}
}

func TestScenarioIncompleteCannotBeProducedAndForgeryIsRejected(t *testing.T) {
	validCounts := make(map[string]uint64, len(scenarios))
	for _, scenario := range scenarios {
		visitBoundedObservations(func(value observation) {
			if !validObservation(scenario, value) {
				return
			}
			validCounts[scenario]++
			assessment, findings := assess(scenario, value)
			if assessment == "" {
				t.Fatalf("valid %s observation has no assessment: %#v", scenario, value)
			}
			for _, finding := range findings {
				if finding == findingOrder[17] {
					t.Fatalf("scenario_incomplete became reachable for %s: %#v", scenario, value)
				}
			}
			forged := append(append([]string{}, findings...), findingOrder[17])
			if equalStrings(forged, findings) {
				t.Fatalf("forged scenario_incomplete unexpectedly validates for %s", scenario)
			}
		})
		if validCounts[scenario] == 0 {
			t.Fatalf("exhaustive space found no valid %s observation", scenario)
		}
	}
	// These counts pin the number of accepted combinations. Update them only
	// when the scenario semantics change deliberately.
	want := map[string]uint64{
		"accept_then_disconnect":            1348,
		"disconnect_before_acceptance":      644,
		"changed_body_retry":                1348,
		"cross_origin_redirect_credentials": 9812,
		"retry_limit":                       2564,
		"delayed_response":                  32548,
	}
	for scenario, count := range want {
		if validCounts[scenario] != count {
			t.Errorf("valid bounded %s count = %d, want %d", scenario, validCounts[scenario], count)
		}
	}

	var invalids invalidBundle
	readCanonical(t, "invalid/semantic.json", &invalids)
	found := false
	for _, candidate := range invalids.Cases {
		if candidate.ID == "semantic_scenario_incomplete_forged" {
			found = candidate.Expected == "invalid_result" && candidate.Operation == "append" &&
				candidate.Operand == `"scenario_incomplete"`
		}
	}
	if !found {
		t.Fatal("the explicit forged scenario_incomplete rejection vector is absent")
	}
}

func TestObservationFieldsHaveBoundsAndContradictionControls(t *testing.T) {
	rows, _ := loadRows(t)
	minimum := map[string]uint64{}
	maximum := map[string]uint64{}
	initialized := map[string]bool{}
	booleanStates := map[string]map[bool]bool{
		"capture_complete": {}, "first_response_complete": {}, "method_consistent": {},
		"destination_consistent": {}, "body_consistent": {},
	}
	for _, row := range rows {
		for field, value := range observationCounts(row.Observation) {
			if !initialized[field] || value < minimum[field] {
				minimum[field] = value
			}
			if !initialized[field] || value > maximum[field] {
				maximum[field] = value
			}
			initialized[field] = true
		}
		booleanStates["capture_complete"][row.Observation.CaptureComplete] = true
		booleanStates["first_response_complete"][row.Observation.FirstResponseComplete] = true
		booleanStates["method_consistent"][row.Observation.MethodConsistent] = true
		booleanStates["destination_consistent"][row.Observation.DestinationConsistent] = true
		booleanStates["body_consistent"][row.Observation.BodyConsistent] = true
	}
	wantMaximum := map[string]uint64{
		"attempt_count": 3, "effect_count": 3, "overlap_count": 2,
		"retry_after_effect_count": 2, "retry_after_unconfirmed_count": 2,
		"retry_before_response_count": 2, "response_attempt_count": 3,
		"response_complete_count": 3, "delay_complete_count": 3,
	}
	for field, upper := range wantMaximum {
		if minimum[field] != 0 || maximum[field] != upper {
			t.Errorf("%s reachable bounds = [%d,%d], want [0,%d]", field, minimum[field], maximum[field], upper)
		}
	}
	for field, states := range booleanStates {
		if !states[false] || !states[true] {
			t.Errorf("%s does not cover both reachable boolean bounds", field)
		}
	}

	var invalids invalidBundle
	readCanonical(t, "invalid/semantic.json", &invalids)
	coverage := make(map[string]bool)
	for _, candidate := range invalids.Cases {
		for _, item := range candidate.Coverage {
			coverage[item] = true
		}
	}
	contradictions := map[string]string{
		"capture_complete":              "relationship.overlap_capture",
		"attempt_count":                 "relationship.effect_attempt",
		"effect_count":                  "relationship.effect_attempt",
		"overlap_count":                 "relationship.overlap_capture",
		"retry_after_effect_count":      "relationship.retry_later",
		"retry_after_unconfirmed_count": "relationship.retry_sum",
		"retry_before_response_count":   "relationship.retry_before",
		"response_attempt_count":        "relationship.response_attempt",
		"response_complete_count":       "relationship.response_complete",
		"first_response_complete":       "relationship.first_response_complete",
		"delay_complete_count":          "relationship.delay_attempt",
		"method_consistent":             "proxy.method_requires_attempt",
		"destination_consistent":        "proxy.destination_requires_attempt",
		"body_consistent":               "proxy.body_requires_attempt",
		"credential":                    "scenario.credential_wrong_role",
		"cleanup":                       "enum.cleanup.unknown",
	}
	for field, control := range contradictions {
		if !coverage[control] {
			t.Errorf("%s lacks contradictory-boundary control %q", field, control)
		}
	}
}

func TestInvalidAndNativeControlInventory(t *testing.T) {
	rows, _ := loadRows(t)
	results, _ := loadResults(t, rows)
	paths := map[string]string{
		"invalid/semantic.json":       "semantic",
		"invalid/report-model.json":   "report_model",
		"invalid/canonical-json.json": "canonical_json",
		"invalid/artifact.json":       "artifact",
		"invalid/sanitation.json":     "sanitation",
		"invalid/authority.json":      "authority",
	}
	allIDs := make(map[string]bool)
	coverage := make(map[string]bool)
	counts := map[string]int{}
	loaded := make(map[string]invalidBundle, len(paths))
	for path, category := range paths {
		var bundle invalidBundle
		readCanonical(t, path, &bundle)
		if bundle.SchemaVersion != invalidBundleIdentity || bundle.Category != category ||
			bundle.Cases == nil || len(bundle.Cases) == 0 ||
			(category == "sanitation") != (bundle.Marker == markerValue) {
			t.Fatalf("invalid bundle %s has identity/shape drift: %#v", path, bundle)
		}
		loaded[category] = bundle
		counts[category] = len(bundle.Cases)
		for _, candidate := range bundle.Cases {
			if !closedID(candidate.ID) || allIDs[candidate.ID] || candidate.Expected == "" ||
				candidate.Operation == "" || candidate.Representability == nil || len(candidate.Representability) == 0 ||
				candidate.Coverage == nil || len(candidate.Coverage) == 0 || !uniqueStrings(candidate.Coverage) ||
				!validRepresentability(candidate.Representability) {
				t.Fatalf("invalid case metadata drift in %s: %#v", path, candidate)
			}
			if !closedInvalidGrammar(category, candidate) {
				t.Fatalf("invalid case escapes the closed %s grammar: %#v", category, candidate)
			}
			allIDs[candidate.ID] = true
			for _, item := range candidate.Coverage {
				coverage[item] = true
			}
			if strings.Contains(candidate.Operand, "marker") && category != "report_model" &&
				category != "semantic" && category != "canonical_json" && category != "artifact" {
				t.Fatalf("unexpected marker operand class in %s", candidate.ID)
			}
		}
	}
	wantCounts := map[string]int{"semantic": 64, "report_model": 20, "canonical_json": 34,
		"artifact": 23, "sanitation": 16, "authority": 13}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("invalid/control inventory counts = %#v, want %#v", counts, wantCounts)
	}
	for _, candidate := range loaded["semantic"].Cases {
		if candidate.BaseCase != "" {
			if _, ok := results[candidate.BaseCase]; !ok {
				t.Errorf("semantic case %q references unknown result base %q", candidate.ID, candidate.BaseCase)
			}
		}
	}
	reportInvalidIDs := make(map[string]bool)
	for _, candidate := range loaded["report_model"].Cases {
		reportInvalidIDs[candidate.ID] = true
		if candidate.BaseCase != "" && candidate.Operation != "construct_report" &&
			candidate.BaseCase != "positive" && candidate.BaseCase != "unsafe" &&
			candidate.BaseCase != "inconclusive" && candidate.BaseCase != "mixed" {
			t.Errorf("report-model case %q references unknown projection base %q", candidate.ID, candidate.BaseCase)
		}
	}
	for _, candidate := range loaded["artifact"].Cases {
		if candidate.BaseCase != "" && candidate.BaseCase != "positive" && candidate.BaseCase != "unsafe" &&
			candidate.BaseCase != "inconclusive" && candidate.BaseCase != "mixed" && !reportInvalidIDs[candidate.BaseCase] {
			t.Errorf("artifact case %q references unknown base %q", candidate.ID, candidate.BaseCase)
		}
	}

	required := []string{
		"enum.scenario.zero", "enum.scenario.unknown", "enum.scenario.successor",
		"enum.assessment.zero", "enum.cleanup.zero", "enum.credential.zero", "enum.finding.zero",
		"collection.scenarios.null", "collection.findings.null", "rows.missing", "rows.duplicate",
		"rows.additional", "rows.reordered", "findings.missing", "findings.duplicate",
		"findings.additional", "findings.reordered", "aggregate.drift",
		"field.attempt_count.above_max", "field.effect_count.above_max", "field.overlap_count.above_max",
		"field.retry_after_effect_count.above_max", "field.retry_after_unconfirmed_count.above_max",
		"field.retry_before_response_count.above_max", "field.response_attempt_count.above_max",
		"field.response_complete_count.above_max", "field.delay_complete_count.above_max",
		"field.attempt_count.required_zero", "field.effect_count.required_zero",
		"field.retry_after_effect_count.required_zero", "field.retry_after_unconfirmed_count.required_zero",
		"field.response_attempt_count.required_zero",
		"field.delay_complete_count.required_zero",
		"false_positive.empty_findings", "precedence.unsafe_survives_incomplete",
		"finding.scenario_incomplete.forged", "native.go.mutation", "native.csharp.detachment",
		"report.identity.schema", "report.identity.suite", "report.identity.explanation", "report.identity.claim",
		"report.text.scenario", "report.text.assessment", "report.text.finding", "report.summary.drift",
		"json.empty", "json.oversized", "json.invalid_utf8", "json.bom", "json.terminal_lf",
		"json.indentation", "json.whitespace", "json.member_order", "json.escaping",
		"json.duplicate_member", "json.unknown_member", "json.null", "json.wrong_type",
		"json.number.fractional", "json.number.exponent", "json.number.overflow",
		"json.depth", "json.members", "json.items", "json.identity", "json.trailing", "json.number.negative",
		"artifact.files.missing", "artifact.files.extra", "artifact.files.reordered", "artifact.files.aliased",
		"artifact.files.duplicate", "artifact.files.corrupt", "artifact.files.independent", "artifact.manifest.whitespace",
		"artifact.manifest.order", "artifact.manifest.unknown", "artifact.manifest.identity",
		"artifact.projection.crossed", "artifact.bound.file", "artifact.bound.aggregate",
		"artifact.build.invalid_report", "artifact.build.null_report", "artifact.validate.nil",
		"artifact.validate.null", "artifact.validate.mutation", "artifact.namespace.alias",
		"sanitation.callback.error_value", "sanitation.callback.error_type", "sanitation.panic",
		"sanitation.response.reason", "sanitation.response.header", "sanitation.response.content",
		"sanitation.request.url", "sanitation.request.header", "sanitation.request.body",
		"sanitation.request.replay", "sanitation.client.type", "sanitation.client.formatter",
		"sanitation.filesystem.path", "sanitation.filesystem.error", "sanitation.environment.proxy",
		"sanitation.environment.credential", "authority.go.surface", "authority.csharp.surface",
		"authority.cross_binding", "authority.cli.runtime", "authority.cli.execution",
		"authority.runtime.loopback", "authority.runtime.proxy", "authority.runtime.order",
		"authority.runtime.concurrency", "authority.cli.persistence",
	}
	for _, item := range required {
		if !coverage[item] {
			t.Errorf("required invalid/control coverage %q is absent", item)
		}
	}
	negativeFields := []string{"summary/scenarios", "summary/passed", "summary/failed", "summary/inconclusive",
		"attempt_count", "effect_count", "overlap_count", "retry_after_effect_count",
		"retry_after_unconfirmed_count", "retry_before_response_count", "response_attempt_count",
		"response_complete_count", "delay_complete_count"}
	for _, field := range negativeFields {
		matched := false
		for item := range coverage {
			if strings.HasPrefix(item, "json.number.negative/") && strings.Contains(item, field) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("negative canonical-JSON vector missing for %s", field)
		}
	}
}

func closedInvalidGrammar(category string, candidate invalidCase) bool {
	operations := map[string]map[string]bool{
		"semantic": {
			"set": true, "null": true, "remove": true, "duplicate": true, "append_copy": true,
			"swap": true, "append": true, "set_positive": true, "erase_unsafe": true,
			"native_mutate_after_validate": true, "native_mutate_original_collection": true,
		},
		"report_model": {"set": true, "null": true, "remove": true, "duplicate_member": true,
			"swap": true, "construct_report": true},
		"canonical_json": {"replace_base64": true, "oversize_ascii": true, "prefix_base64": true,
			"remove_terminal_lf": true, "reindent": true, "insert_whitespace": true, "swap_members": true,
			"replace_escape": true, "duplicate_member": true, "add_member": true, "replace_json": true,
			"set": true, "replace_object_members": true, "replace_array_items": true, "suffix_base64": true},
		"artifact": {"native_alias_probe": true, "remove_file": true, "add_file": true, "swap_files": true,
			"alias_contents": true, "duplicate_file": true, "flip_byte": true, "insert_whitespace": true,
			"swap_members": true, "add_member": true, "set": true, "replace_file_from_case": true,
			"resize_file": true, "resize_set": true, "build_artifact": true, "build_artifact_null": true,
			"construct_file_null": true, "construct_file_malformed": true, "mutate_file_name": true,
			"validate_null": true, "validate_nil": true, "mutate_after_validate": true},
		"sanitation": {"inject_marker": true},
		"authority": {"native_nil_collection": true, "native_alias_probe": true, "source_surface": true,
			"source_import": true, "source_capability": true, "native_runtime": true, "filesystem_boundary": true},
	}
	expected := map[string]map[string]bool{
		"semantic":       {"invalid_result": true, "valid_after_caller_mutation": true},
		"report_model":   {"invalid_report": true},
		"canonical_json": {"invalid_report": true},
		"artifact":       {"invalid_artifact": true, "detached": true},
		"sanitation":     {"marker_absent": true},
		"authority": {"invalid_result": true, "detached": true, "exact_surface": true,
			"forbidden_absent": true, "native_control": true},
	}
	return operations[category][candidate.Operation] && expected[category][candidate.Expected]
}

func TestFixtureIdentityExpectedClassAndRepresentability(t *testing.T) {
	digest := sha256.New()
	rowPaths := []string{"results/positive/rows.json", "results/unsafe/rows.json", "results/inconclusive/rows.json"}
	for _, path := range rowPaths {
		var bundle rowBundle
		readCanonical(t, path, &bundle)
		writeNeutralDigestAtom(digest, path)
		for _, candidate := range bundle.Cases {
			writeNeutralDigestAtom(digest, candidate.ID)
			writeNeutralDigestAtom(digest, "valid:"+candidate.Assessment)
			writeNeutralDigestAtom(digest, strings.Join(candidate.Representability, ","))
		}
	}
	resultPaths := []string{"results/positive/results.json", "results/unsafe/results.json",
		"results/inconclusive/results.json", "results/mixed/results.json"}
	for _, path := range resultPaths {
		var bundle resultBundle
		readCanonical(t, path, &bundle)
		writeNeutralDigestAtom(digest, path)
		for _, candidate := range bundle.Cases {
			writeNeutralDigestAtom(digest, candidate.ID)
			writeNeutralDigestAtom(digest, "valid:"+candidate.Assessment)
			writeNeutralDigestAtom(digest, strings.Join(candidate.Representability, ","))
		}
	}
	invalidPaths := []string{"invalid/semantic.json", "invalid/report-model.json", "invalid/canonical-json.json",
		"invalid/artifact.json", "invalid/sanitation.json", "invalid/authority.json"}
	for _, path := range invalidPaths {
		var bundle invalidBundle
		readCanonical(t, path, &bundle)
		writeNeutralDigestAtom(digest, path)
		for _, candidate := range bundle.Cases {
			writeNeutralDigestAtom(digest, candidate.ID)
			writeNeutralDigestAtom(digest, candidate.Expected)
			writeNeutralDigestAtom(digest, strings.Join(candidate.Representability, ","))
		}
	}
	got := hex.EncodeToString(digest.Sum(nil))
	const want = "649955cfaf997e700bcda16a9b4be27e46e17ab4cb1c46fe00e6a52c202b8b5c"
	if got != want {
		t.Fatalf("case identity/expected-class/representability digest = %s, want %s", got, want)
	}
}

func loadRows(t *testing.T) (map[string]rowCase, map[string]string) {
	t.Helper()
	paths := map[string]string{
		"results/positive/rows.json": "positive", "results/unsafe/rows.json": "unsafe",
		"results/inconclusive/rows.json": "inconclusive",
	}
	rows := make(map[string]rowCase)
	categories := make(map[string]string)
	for path, category := range paths {
		var bundle rowBundle
		readCanonical(t, path, &bundle)
		if bundle.SchemaVersion != rowBundleIdentity || bundle.Category != category || bundle.Cases == nil {
			t.Fatalf("row bundle %s drift: %#v", path, bundle)
		}
		for _, row := range bundle.Cases {
			if _, exists := rows[row.ID]; exists {
				t.Fatalf("duplicate row case %q", row.ID)
			}
			rows[row.ID], categories[row.ID] = row, category
		}
	}
	return rows, categories
}

func loadResults(t *testing.T, rows map[string]rowCase) (map[string]resultCase, map[string]string) {
	t.Helper()
	paths := map[string]string{
		"results/positive/results.json": "positive", "results/unsafe/results.json": "unsafe",
		"results/inconclusive/results.json": "inconclusive", "results/mixed/results.json": "mixed",
	}
	results := make(map[string]resultCase)
	categories := make(map[string]string)
	for path, category := range paths {
		var bundle resultBundle
		readCanonical(t, path, &bundle)
		if bundle.SchemaVersion != resultBundleIdentity || bundle.Category != category || bundle.Cases == nil {
			t.Fatalf("result bundle %s drift: %#v", path, bundle)
		}
		for _, result := range bundle.Cases {
			if _, exists := results[result.ID]; exists {
				t.Fatalf("duplicate result case %q", result.ID)
			}
			results[result.ID], categories[result.ID] = result, category
		}
	}
	_ = rows
	return results, categories
}

func visitBoundedObservations(visit func(observation)) {
	credentials := [...]string{"not_observed", "source_only", "absent_at_target", "exposed_at_target", "missing"}
	cleanups := [...]string{"succeeded", "failed"}
	for attempts := uint32(0); attempts <= 3; attempts++ {
		later := uint32(0)
		if attempts != 0 {
			later = attempts - 1
		}
		for effects := uint64(0); effects <= uint64(attempts); effects++ {
			maxOverlap := uint32(0)
			if attempts > 1 {
				maxOverlap = attempts - 1
				if maxOverlap > 2 {
					maxOverlap = 2
				}
			}
			for overlap := uint32(0); overlap <= maxOverlap; overlap++ {
				for afterEffect := uint32(0); afterEffect <= later; afterEffect++ {
					for afterUnconfirmed := uint32(0); afterUnconfirmed+afterEffect <= later; afterUnconfirmed++ {
						for beforeResponse := uint32(0); beforeResponse <= afterEffect; beforeResponse++ {
							for responseAttempts := uint32(0); responseAttempts <= attempts; responseAttempts++ {
								for responseComplete := uint32(0); responseComplete <= responseAttempts; responseComplete++ {
									for first := 0; first < 2; first++ {
										firstComplete := first == 1
										if (firstComplete && responseComplete == 0) ||
											(attempts == 1 && responseComplete > 0 && !firstComplete) {
											continue
										}
										for delay := uint32(0); delay <= attempts; delay++ {
											for consistency := 0; consistency < 8; consistency++ {
												method := consistency&1 != 0
												destination := consistency&2 != 0
												body := consistency&4 != 0
												if attempts == 0 && (!method || !destination || !body) {
													continue
												}
												for _, credential := range credentials {
													if attempts == 0 && credential != "not_observed" {
														continue
													}
													for _, cleanup := range cleanups {
														for capture := 0; capture < 2; capture++ {
															captureComplete := capture == 1
															if overlap != 0 && captureComplete {
																continue
															}
															visit(observation{CaptureComplete: captureComplete, AttemptCount: attempts,
																EffectCount: effects, OverlapCount: overlap, RetryAfterEffectCount: afterEffect,
																RetryAfterUnconfirmedCount: afterUnconfirmed, RetryBeforeResponseCount: beforeResponse,
																ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
																FirstResponseComplete: firstComplete, DelayCompleteCount: delay,
																MethodConsistent: method, DestinationConsistent: destination, BodyConsistent: body,
																Credential: credential, Cleanup: cleanup})
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func observationCounts(value observation) map[string]uint64 {
	return map[string]uint64{
		"attempt_count": uint64(value.AttemptCount), "effect_count": value.EffectCount,
		"overlap_count": uint64(value.OverlapCount), "retry_after_effect_count": uint64(value.RetryAfterEffectCount),
		"retry_after_unconfirmed_count": uint64(value.RetryAfterUnconfirmedCount),
		"retry_before_response_count":   uint64(value.RetryBeforeResponseCount),
		"response_attempt_count":        uint64(value.ResponseAttemptCount),
		"response_complete_count":       uint64(value.ResponseCompleteCount),
		"delay_complete_count":          uint64(value.DelayCompleteCount),
	}
}

func readCanonical(t *testing.T, relative string, destination any) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(corpusRoot, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 || contents[len(contents)-1] != '\n' || !utf8.Valid(contents) {
		t.Fatalf("%s is not nonempty terminal-LF UTF-8", relative)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode %s: %v", relative, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing JSON in %s: %v", relative, err)
	}
	want, err := json.MarshalIndent(destination, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if !bytes.Equal(contents, want) {
		t.Fatalf("%s is not canonical fixture JSON", relative)
	}
	return contents
}

func categoryForAssessment(id, assessment string) string {
	if strings.HasPrefix(id, "result_mixed_") {
		return "mixed"
	}
	switch assessment {
	case assessmentPositive:
		return "positive"
	case assessmentUnsafe:
		return "unsafe"
	case assessmentInconclusive:
		return "inconclusive"
	default:
		return ""
	}
}

func portableCorpusPath(value string) bool {
	_, ok := portableCorpusPathKey(value)
	return ok
}

func portableCorpusPathKey(value string) (string, bool) {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || !utf8.ValidString(value) {
		return "", false
	}
	for _, character := range value {
		if character > 0x7f || !(character == '/' || character == '-' || character == '_' || character == '.' ||
			character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return "", false
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || strings.HasSuffix(segment, ".") || windowsReservedSegment(segment) {
			return "", false
		}
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))) != value {
		return "", false
	}
	return strings.ToLower(value), true
}

func windowsReservedSegment(segment string) bool {
	base := segment
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	base = strings.ToUpper(base)
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		base[3] >= '1' && base[3] <= '9'
}

func lowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func closedID(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for index, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') &&
			!(index != 0 && character == '_') {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validRepresentability(values []string) bool {
	if !uniqueStrings(values) {
		return false
	}
	rank := map[string]int{"go": 0, "csharp": 1, "cli": 2}
	previous := -1
	for _, value := range values {
		current, ok := rank[value]
		if !ok || current <= previous {
			return false
		}
		previous = current
	}
	return true
}

func sortedStrings(values []string) bool {
	return sort.StringsAreSorted(values)
}
