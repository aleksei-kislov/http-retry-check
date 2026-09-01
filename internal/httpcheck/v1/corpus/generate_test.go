//go:build corpusgen

package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestRegenerateHTTPRetryCheckCorpus is an explicit offline-only constructor. It
// never runs in ordinary tests because this file requires the corpusgen build
// tag, and it additionally requires both explicit environment values. The
// destination must not already exist.
func TestRegenerateHTTPRetryCheckCorpus(t *testing.T) {
	if os.Getenv("HTTP_RETRY_CHECK_REGENERATE_CORPUS") != "1" {
		t.Skip("set HTTP_RETRY_CHECK_REGENERATE_CORPUS=1 for explicit offline construction")
	}
	root := os.Getenv("HTTP_RETRY_CHECK_CORPUS_OUTPUT")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		t.Fatal("HTTP_RETRY_CHECK_CORPUS_OUTPUT must be one clean absolute path")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("corpus output must not exist")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	rowBundles := buildRowBundles(t)
	resultBundles := buildResultBundles(t, rowBundles)
	for path, bundle := range rowBundles {
		writeCanonical(t, root, path, bundle)
	}
	for path, bundle := range resultBundles {
		writeCanonical(t, root, path, bundle)
	}
	for path, bundle := range buildInvalidBundles() {
		writeCanonical(t, root, path, bundle)
	}
	writeProjections(t, root, rowBundles, resultBundles)
	writeRootManifest(t, root)
}

func buildRowBundles(t *testing.T) map[string]rowBundle {
	positive := []rowCase{
		newRow(t, "positive_accept_then_disconnect", scenarios[0], positiveAccept(),
			"positive_tuple.accept_then_disconnect", "field.capture_complete.upper", "field.effect_count.nominal"),
		newRow(t, "positive_disconnect_before_acceptance", scenarios[1], positiveDisconnect(),
			"positive_tuple.disconnect_before_acceptance", "field.effect_count.lower"),
		newRow(t, "positive_changed_body_retry", scenarios[2], positiveChanged(),
			"positive_tuple.changed_body_retry"),
		newRow(t, "positive_redirect_refused", scenarios[3], positiveRedirectRefused(),
			"positive_tuple.redirect_refused", "credential.source_only", "field.attempt_count.nominal"),
		newRow(t, "positive_redirect_followed", scenarios[3], positiveRedirectFollowed(),
			"positive_tuple.redirect_followed", "credential.absent_at_target", "field.response_complete_count.nominal"),
		newRow(t, "positive_retry_limit_one", scenarios[4], positiveRetryOne(),
			"positive_tuple.retry_limit_one"),
		newRow(t, "positive_retry_limit_two", scenarios[4], positiveRetryTwo(),
			"positive_tuple.retry_limit_two", "field.attempt_count.valid_upper_policy"),
		newRow(t, "positive_delayed_response_complete", scenarios[5], positiveDelayedComplete(),
			"positive_tuple.delayed_response_complete", "field.first_response_complete.upper"),
		newRow(t, "positive_delayed_response_write_unobserved", scenarios[5], positiveDelayedUnobserved(),
			"positive_tuple.delayed_response_write_unobserved", "field.first_response_complete.lower"),
	}

	unsafeRows := []rowCase{
		newRow(t, "unsafe_accept_retry_after_effect", scenarios[0], withCounts(positiveAccept(), 2, 2, 0, 1, 0, 0, 0, 0, false, 0),
			"scenario_unsafe.accept_then_disconnect", "finding.retry_after_accepted_request", "finding.effect_limit_exceeded"),
		newRow(t, "unsafe_disconnect_retry_after_unconfirmed", scenarios[1], withCounts(positiveDisconnect(), 2, 0, 0, 0, 1, 0, 0, 0, false, 0),
			"scenario_unsafe.disconnect_before_acceptance", "finding.retry_after_unconfirmed_acceptance"),
		newRow(t, "unsafe_changed_body_retry", scenarios[2], bodyChanged(withCounts(positiveChanged(), 2, 2, 0, 1, 0, 0, 0, 0, false, 0)),
			"scenario_unsafe.changed_body_retry", "finding.retry_after_accepted_request", "finding.body_changed", "finding.effect_limit_exceeded", "field.body_consistent.lower"),
		newRow(t, "unsafe_redirect_credential_exposed", scenarios[3], credential(positiveRedirectFollowed(), "exposed_at_target"),
			"scenario_unsafe.cross_origin_redirect_credentials", "finding.credential_exposed_at_target", "credential.exposed_at_target"),
		newRow(t, "unsafe_retry_limit_exceeded", scenarios[4], withCounts(positiveRetryOne(), 3, 0, 0, 0, 0, 0, 3, 3, true, 0),
			"scenario_unsafe.retry_limit", "finding.attempt_limit_exceeded", "field.attempt_count.max", "field.response_attempt_count.max", "field.response_complete_count.max"),
		newRow(t, "unsafe_delayed_retry_before_response", scenarios[5], delayedRetryBeforeResponse(),
			"scenario_unsafe.delayed_response", "finding.retry_before_response", "finding.retry_after_accepted_request", "finding.effect_limit_exceeded", "field.response_later_complete_first_incomplete"),
		newRow(t, "unsafe_method_changed", scenarios[1], methodChanged(positiveDisconnect()),
			"finding.method_changed", "field.method_consistent.lower"),
		newRow(t, "unsafe_destination_changed", scenarios[1], destinationChanged(positiveDisconnect()),
			"finding.destination_changed", "field.destination_consistent.lower"),
		newRow(t, "unsafe_body_changed", scenarios[1], bodyChanged(positiveDisconnect()),
			"finding.body_changed", "field.body_consistent.lower"),
		newRow(t, "unsafe_effect_count_max", scenarios[0], acceptMaxCounts(),
			"finding.attempt_limit_exceeded", "finding.effect_limit_exceeded", "field.effect_count.max", "field.retry_after_effect_count.max"),
		newRow(t, "unsafe_unconfirmed_count_max", scenarios[1], disconnectMaxCounts(),
			"finding.attempt_limit_exceeded", "finding.retry_after_unconfirmed_acceptance", "field.retry_after_unconfirmed_count.max"),
		newRow(t, "unsafe_retry_before_count_max", scenarios[5], delayedMaxCounts(),
			"finding.attempt_limit_exceeded", "finding.retry_before_response", "finding.retry_after_accepted_request", "finding.effect_limit_exceeded", "field.retry_before_response_count.max", "field.delay_complete_count.max"),
		newRow(t, "unsafe_overlap_count_max", scenarios[4], retryOverlapMax(),
			"finding.capture_incomplete", "finding.attempt_limit_exceeded", "field.overlap_count.max"),
	}

	inconclusiveRows := make([]rowCase, 0, 24)
	for index, scenario := range scenarios {
		inconclusiveRows = append(inconclusiveRows,
			newRow(t, "unrun_"+scenario+"_cleanup_succeeded", scenario, unrun("succeeded"),
				"unrun."+scenario, "cleanup.succeeded", "finding.attempt_not_observed", "finding.capture_incomplete", "field.counts.lower"),
			newRow(t, "unrun_"+scenario+"_cleanup_failed", scenario, unrun("failed"),
				"unrun."+scenario, "cleanup.failed", "finding.attempt_not_observed", "finding.capture_incomplete", "finding.cleanup_unverified"),
		)
		_ = index
	}
	inconclusiveRows = append(inconclusiveRows,
		newRow(t, "inconclusive_capture_only", scenarios[0], captureIncomplete(positiveAccept()),
			"finding.capture_incomplete", "field.capture_complete.lower"),
		newRow(t, "inconclusive_response_incomplete", scenarios[3], redirectResponseIncomplete(),
			"finding.response_incomplete", "field.response_complete_count.lower"),
		newRow(t, "inconclusive_delay_incomplete", scenarios[5], delayedIncomplete(),
			"finding.capture_incomplete", "finding.response_incomplete", "finding.delay_incomplete", "field.delay_complete_count.lower"),
		newRow(t, "inconclusive_credential_not_observed", scenarios[1], credentialNotObserved(),
			"finding.capture_incomplete", "finding.credential_not_observed", "credential.not_observed"),
		newRow(t, "inconclusive_credential_missing", scenarios[1], credential(positiveDisconnect(), "missing"),
			"finding.credential_missing", "credential.missing"),
		newRow(t, "inconclusive_effect_not_observed", scenarios[3], redirectEffectNotObserved(),
			"finding.effect_not_observed"),
		newRow(t, "inconclusive_cleanup_unverified", scenarios[0], cleanup(positiveAccept(), "failed"),
			"finding.cleanup_unverified", "cleanup.failed"),
		newRow(t, "inconclusive_later_response_complete", scenarios[4], retryLaterResponseComplete(),
			"finding.response_incomplete", "field.response_later_complete_first_incomplete"),
	)

	return map[string]rowBundle{
		"results/positive/rows.json":     {SchemaVersion: rowBundleIdentity, Category: "positive", Cases: positive},
		"results/unsafe/rows.json":       {SchemaVersion: rowBundleIdentity, Category: "unsafe", Cases: unsafeRows},
		"results/inconclusive/rows.json": {SchemaVersion: rowBundleIdentity, Category: "inconclusive", Cases: inconclusiveRows},
	}
}

func buildResultBundles(t *testing.T, rows map[string]rowBundle) map[string]resultBundle {
	allRows := collectRows(rows)
	positive := defaultPositiveRows()
	positiveResults := []resultCase{
		newResult(t, allRows, "result_all_positive", positive, "result.all_positive", "aggregate.positive"),
	}
	unsafeResults := make([]resultCase, 0, len(scenarios))
	unsafeIDs := [...]string{
		"unsafe_accept_retry_after_effect",
		"unsafe_disconnect_retry_after_unconfirmed",
		"unsafe_changed_body_retry",
		"unsafe_redirect_credential_exposed",
		"unsafe_retry_limit_exceeded",
		"unsafe_delayed_retry_before_response",
	}
	for index, scenario := range scenarios {
		refs := append([]string{}, positive...)
		refs[index] = unsafeIDs[index]
		unsafeResults = append(unsafeResults, newResult(t, allRows, "result_unsafe_"+scenario, refs,
			"result.unsafe."+scenario, "aggregate.unsafe"))
	}
	inconclusiveResults := make([]resultCase, 0, len(scenarios))
	for index, scenario := range scenarios {
		refs := append([]string{}, positive...)
		refs[index] = "unrun_" + scenario + "_cleanup_succeeded"
		inconclusiveResults = append(inconclusiveResults, newResult(t, allRows, "result_inconclusive_"+scenario, refs,
			"result.inconclusive."+scenario, "aggregate.inconclusive"))
	}
	mixedRefs := append([]string{}, positive...)
	mixedRefs[3] = "unsafe_redirect_credential_exposed"
	mixedRefs[5] = "unrun_delayed_response_cleanup_failed"
	mixed := []resultCase{
		newResult(t, allRows, "result_mixed_unsafe_over_inconclusive", mixedRefs,
			"result.mixed", "precedence.unsafe_over_inconclusive", "aggregate.unsafe"),
	}
	return map[string]resultBundle{
		"results/positive/results.json":     {SchemaVersion: resultBundleIdentity, Category: "positive", Cases: positiveResults},
		"results/unsafe/results.json":       {SchemaVersion: resultBundleIdentity, Category: "unsafe", Cases: unsafeResults},
		"results/inconclusive/results.json": {SchemaVersion: resultBundleIdentity, Category: "inconclusive", Cases: inconclusiveResults},
		"results/mixed/results.json":        {SchemaVersion: resultBundleIdentity, Category: "mixed", Cases: mixed},
	}
}

func buildInvalidBundles() map[string]invalidBundle {
	both := []string{"go", "csharp"}
	semantic := []invalidCase{
		invalid("semantic_scenario_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/scenario", `""`, 0, "enum.scenario.zero"),
		invalid("semantic_scenario_unknown", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/scenario", `"marker_unknown"`, 0, "enum.scenario.unknown"),
		invalid("semantic_scenario_successor", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/scenario", `"accept_then_disconnect_v2"`, 0, "enum.scenario.successor"),
		invalid("semantic_assessment_zero", "invalid_result", both, "result_all_positive", "set", "/assessment", `""`, 0, "enum.assessment.zero"),
		invalid("semantic_assessment_unknown", "invalid_result", both, "result_all_positive", "set", "/assessment", `"marker_unknown"`, 0, "enum.assessment.unknown"),
		invalid("semantic_assessment_successor", "invalid_result", both, "result_all_positive", "set", "/assessment", `"pass_v2"`, 0, "enum.assessment.successor"),
		invalid("semantic_cleanup_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/cleanup", `""`, 0, "enum.cleanup.zero"),
		invalid("semantic_cleanup_unknown", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/cleanup", `"marker_unknown"`, 0, "enum.cleanup.unknown"),
		invalid("semantic_cleanup_successor", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/cleanup", `"succeeded_v2"`, 0, "enum.cleanup.successor"),
		invalid("semantic_credential_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/credential", `""`, 0, "enum.credential.zero"),
		invalid("semantic_credential_unknown", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/credential", `"marker_unknown"`, 0, "enum.credential.unknown"),
		invalid("semantic_credential_successor", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/credential", `"source_only_v2"`, 0, "enum.credential.successor"),
		invalid("semantic_finding_zero", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/findings/0", `""`, 0, "enum.finding.zero"),
		invalid("semantic_finding_unknown", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/findings/0", `"marker_unknown"`, 0, "enum.finding.unknown"),
		invalid("semantic_finding_successor", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/findings/0", `"capture_incomplete_v2"`, 0, "enum.finding.successor"),
		invalid("semantic_scenarios_null", "invalid_result", both, "result_all_positive", "null", "/scenarios", "", 0, "collection.scenarios.null"),
		invalid("semantic_findings_null", "invalid_result", both, "result_all_positive", "null", "/scenarios/0/findings", "", 0, "collection.findings.null"),
		invalid("semantic_row_missing", "invalid_result", both, "result_all_positive", "remove", "/scenarios/5", "", 0, "rows.missing"),
		invalid("semantic_row_duplicate", "invalid_result", both, "result_all_positive", "duplicate", "/scenarios/0", "", 0, "rows.duplicate"),
		invalid("semantic_row_additional", "invalid_result", both, "result_all_positive", "append_copy", "/scenarios", "/scenarios/0", 0, "rows.additional"),
		invalid("semantic_rows_reordered", "invalid_result", both, "result_all_positive", "swap", "/scenarios/0", "/scenarios/1", 0, "rows.reordered"),
		invalid("semantic_finding_missing", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "remove", "/scenarios/0/findings/0", "", 0, "findings.missing"),
		invalid("semantic_finding_duplicate", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "duplicate", "/scenarios/0/findings/0", "", 0, "findings.duplicate"),
		invalid("semantic_finding_additional", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "append", "/scenarios/0/findings", `"cleanup_unverified"`, 0, "findings.additional"),
		invalid("semantic_findings_reordered", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "swap", "/scenarios/0/findings/0", "/scenarios/0/findings/1", 0, "findings.reordered"),
		invalid("semantic_aggregate_drift", "invalid_result", both, "result_all_positive", "set", "/assessment", `"inconclusive"`, 0, "aggregate.drift"),
		invalid("semantic_attempt_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/attempt_count", "4", 0, "field.attempt_count.above_max"),
		invalid("semantic_effect_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/effect_count", "4", 0, "field.effect_count.above_max"),
		invalid("semantic_overlap_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/overlap_count", "3", 0, "field.overlap_count.above_max"),
		invalid("semantic_retry_after_effect_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/retry_after_effect_count", "3", 0, "field.retry_after_effect_count.above_max"),
		invalid("semantic_retry_after_unconfirmed_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/1/observation/retry_after_unconfirmed_count", "3", 0, "field.retry_after_unconfirmed_count.above_max"),
		invalid("semantic_retry_before_response_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/5/observation/retry_before_response_count", "3", 0, "field.retry_before_response_count.above_max"),
		invalid("semantic_response_attempt_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/3/observation/response_attempt_count", "4", 0, "field.response_attempt_count.above_max"),
		invalid("semantic_response_complete_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/3/observation/response_complete_count", "4", 0, "field.response_complete_count.above_max"),
		invalid("semantic_delay_above_max", "invalid_result", both, "result_all_positive", "set", "/scenarios/5/observation/delay_complete_count", "4", 0, "field.delay_complete_count.above_max"),
		invalid("semantic_effect_exceeds_attempt", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/effect_count", "2", 0, "relationship.effect_attempt"),
		invalid("semantic_overlap_contradicts_complete", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/overlap_count", "1", 0, "relationship.overlap_capture"),
		invalid("semantic_retry_after_exceeds_later", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/retry_after_effect_count", "1", 0, "relationship.retry_later"),
		invalid("semantic_retry_causal_sum_exceeds_later", "invalid_result", both, "result_unsafe_accept_then_disconnect", "set", "/scenarios/0/observation/retry_after_unconfirmed_count", "1", 0, "relationship.retry_sum"),
		invalid("semantic_retry_before_exceeds_after_effect", "invalid_result", both, "result_all_positive", "set", "/scenarios/5/observation/retry_before_response_count", "1", 0, "relationship.retry_before"),
		invalid("semantic_response_attempt_exceeds_attempt", "invalid_result", both, "result_all_positive", "set", "/scenarios/3/observation/response_attempt_count", "3", 0, "relationship.response_attempt"),
		invalid("semantic_response_complete_exceeds_attempt", "invalid_result", both, "result_all_positive", "set", "/scenarios/3/observation/response_complete_count", "3", 0, "relationship.response_complete"),
		invalid("semantic_first_response_without_completion", "invalid_result", both, "result_all_positive", "set", "/scenarios/5/observation/response_complete_count", "0", 0, "relationship.first_response_complete"),
		invalid("semantic_single_later_response_role", "invalid_result", both, "result_all_positive", "set", "/scenarios/3/observation/first_response_complete", "false", 0, "relationship.response_role"),
		invalid("semantic_delay_exceeds_attempt", "invalid_result", both, "result_all_positive", "set", "/scenarios/5/observation/delay_complete_count", "2", 0, "relationship.delay_attempt"),
		invalid("semantic_delay_wrong_scenario", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/delay_complete_count", "1", 0, "scenario.delay_wrong_role"),
		invalid("semantic_response_wrong_scenario", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/response_attempt_count", "1", 0, "scenario.response_wrong_role"),
		invalid("semantic_retry_unconfirmed_wrong_scenario", "invalid_result", both, "result_unsafe_accept_then_disconnect", "set", "/scenarios/0/observation/retry_after_unconfirmed_count", "1", 0, "scenario.retry_causality_wrong_role"),
		invalid("semantic_attempt_required_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/attempt_count", "0", 0, "field.attempt_count.required_zero"),
		invalid("semantic_effect_required_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/effect_count", "0", 0, "field.effect_count.required_zero"),
		invalid("semantic_retry_after_effect_required_zero", "invalid_result", both, "result_unsafe_accept_then_disconnect", "set", "/scenarios/0/observation/retry_after_effect_count", "0", 0, "field.retry_after_effect_count.required_zero"),
		invalid("semantic_retry_after_unconfirmed_required_zero", "invalid_result", both, "result_unsafe_disconnect_before_acceptance", "set", "/scenarios/1/observation/retry_after_unconfirmed_count", "0", 0, "field.retry_after_unconfirmed_count.required_zero"),
		invalid("semantic_response_attempt_required_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/3/observation/response_attempt_count", "0", 0, "field.response_attempt_count.required_zero"),
		invalid("semantic_delay_required_zero", "invalid_result", both, "result_all_positive", "set", "/scenarios/5/observation/delay_complete_count", "0", 0, "field.delay_complete_count.required_zero"),
		invalid("semantic_target_credential_wrong_scenario", "invalid_result", both, "result_all_positive", "set", "/scenarios/0/observation/credential", `"absent_at_target"`, 0, "scenario.credential_wrong_role"),
		invalid("semantic_method_fact_without_attempt", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/observation/method_consistent", "false", 0, "proxy.method_requires_attempt"),
		invalid("semantic_destination_fact_without_attempt", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/observation/destination_consistent", "false", 0, "proxy.destination_requires_attempt"),
		invalid("semantic_body_fact_without_attempt", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/observation/body_consistent", "false", 0, "proxy.body_requires_attempt"),
		invalid("semantic_credential_fact_without_attempt", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set", "/scenarios/0/observation/credential", `"source_only"`, 0, "proxy.credential_requires_attempt"),
		invalid("semantic_false_positive_empty_findings", "invalid_result", both, "result_inconclusive_accept_then_disconnect", "set_positive", "/scenarios/0", "", 0, "false_positive.empty_findings"),
		invalid("semantic_unsafe_erased_by_incomplete_capture", "invalid_result", both, "result_unsafe_cross_origin_redirect_credentials", "erase_unsafe", "/scenarios/3", "", 0, "precedence.unsafe_survives_incomplete"),
		invalid("semantic_scenario_incomplete_forged", "invalid_result", both, "result_all_positive", "append", "/scenarios/0/findings", `"scenario_incomplete"`, 0, "finding.scenario_incomplete.forged"),
		invalid("semantic_native_mutation_after_validation", "invalid_result", []string{"go"}, "result_all_positive", "native_mutate_after_validate", "/scenarios/0/findings", "", 0, "native.go.mutation"),
		invalid("semantic_caller_collection_detached", "valid_after_caller_mutation", []string{"csharp"}, "result_all_positive", "native_mutate_original_collection", "/scenarios", "", 0, "native.csharp.detachment"),
	}

	reportModel := []invalidCase{
		invalid("report_outcome_zero", "invalid_report", both, "positive", "set", "/outcome", `""`, 0, "report.outcome.zero"),
		invalid("report_outcome_unknown", "invalid_report", both, "positive", "set", "/outcome", `"marker_unknown"`, 0, "report.outcome.unknown"),
		invalid("report_outcome_successor", "invalid_report", both, "positive", "set", "/outcome", `"pass_v2"`, 0, "report.outcome.successor"),
		invalid("report_schema_drift", "invalid_report", both, "positive", "set", "/schema_version", `"http_retry_check.report.v2"`, 0, "report.identity.schema"),
		invalid("report_suite_drift", "invalid_report", both, "positive", "set", "/suite_identity", `"http_retry_check.scenario_suite.v2"`, 0, "report.identity.suite"),
		invalid("report_explanation_identity_drift", "invalid_report", both, "positive", "set", "/explanation_identity", `"http_retry_check.scenario_explanations.v2"`, 0, "report.identity.explanation"),
		invalid("report_claim_ceiling_drift", "invalid_report", both, "positive", "set", "/claim_ceiling", `"marker_claim"`, 0, "report.identity.claim"),
		invalid("report_scenario_text_drift", "invalid_report", both, "positive", "set", "/scenarios/0/scenario_text", `"marker_text"`, 0, "report.text.scenario"),
		invalid("report_assessment_text_drift", "invalid_report", both, "positive", "set", "/assessment_text", `"marker_text"`, 0, "report.text.assessment"),
		invalid("report_row_assessment_text_drift", "invalid_report", both, "positive", "set", "/scenarios/0/assessment_text", `"marker_text"`, 0, "report.text.row_assessment"),
		invalid("report_finding_text_drift", "invalid_report", both, "inconclusive", "set", "/scenarios/5/findings/0/text", `"marker_text"`, 0, "report.text.finding"),
		invalid("report_aggregate_drift", "invalid_report", both, "positive", "set", "/assessment", `"inconclusive"`, 0, "report.aggregate.drift"),
		invalid("report_summary_drift", "invalid_report", both, "positive", "set", "/summary/passed", "5", 0, "report.summary.drift"),
		invalid("report_scenarios_null", "invalid_report", both, "positive", "null", "/scenarios", "", 0, "report.collection.null"),
		invalid("report_findings_null", "invalid_report", both, "positive", "null", "/scenarios/0/findings", "", 0, "report.findings.null"),
		invalid("report_nested_missing", "invalid_report", both, "positive", "remove", "/scenarios/0/observation", "", 0, "report.nested.missing"),
		invalid("report_nested_duplicate", "invalid_report", both, "positive", "duplicate_member", "/scenarios/0/assessment", "", 0, "report.nested.duplicate"),
		invalid("report_rows_reordered", "invalid_report", both, "positive", "swap", "/scenarios/0", "/scenarios/1", 0, "report.rows.reordered"),
		invalid("report_findings_reordered", "invalid_report", both, "mixed", "swap", "/scenarios/5/findings/0", "/scenarios/5/findings/1", 0, "report.findings.reordered"),
		invalid("report_constructor_invalid_result", "invalid_report", both, "semantic_scenario_incomplete_forged", "construct_report", "", "", 0, "report.constructor.contradiction"),
	}

	canonicalJSON := []invalidCase{
		invalid("json_empty", "invalid_report", both, "positive", "replace_base64", "", "", 0, "json.empty"),
		invalid("json_oversized", "invalid_report", both, "positive", "oversize_ascii", "", " ", 262145, "json.oversized"),
		invalid("json_invalid_utf8", "invalid_report", both, "positive", "replace_base64", "", "/w==", 0, "json.invalid_utf8"),
		invalid("json_bom", "invalid_report", both, "positive", "prefix_base64", "", "77u/", 0, "json.bom"),
		invalid("json_missing_terminal_lf", "invalid_report", both, "positive", "remove_terminal_lf", "", "", 0, "json.terminal_lf"),
		invalid("json_alternate_indentation", "invalid_report", both, "positive", "reindent", "", "4", 0, "json.indentation"),
		invalid("json_alternate_whitespace", "invalid_report", both, "positive", "insert_whitespace", "/schema_version", " ", 0, "json.whitespace"),
		invalid("json_member_order", "invalid_report", both, "positive", "swap_members", "/schema_version", "/suite_identity", 0, "json.member_order"),
		invalid("json_alternate_escaping", "invalid_report", both, "positive", "replace_escape", "/claim_ceiling", `\u0054`, 0, "json.escaping"),
		invalid("json_duplicate_member", "invalid_report", both, "positive", "duplicate_member", "/schema_version", "", 0, "json.duplicate_member"),
		invalid("json_unknown_member", "invalid_report", both, "positive", "add_member", "/marker_unknown", "true", 0, "json.unknown_member"),
		invalid("json_null", "invalid_report", both, "positive", "replace_json", "", "null", 0, "json.null"),
		invalid("json_wrong_type", "invalid_report", both, "positive", "set", "/summary", `"marker_wrong_type"`, 0, "json.wrong_type"),
		invalid("json_fractional_number", "invalid_report", both, "positive", "set", "/summary/passed", "6.0", 0, "json.number.fractional"),
		invalid("json_exponent_number", "invalid_report", both, "positive", "set", "/summary/passed", "6e0", 0, "json.number.exponent"),
		invalid("json_overflowing_number", "invalid_report", both, "positive", "set", "/summary/passed", "18446744073709551616", 0, "json.number.overflow"),
		invalid("json_excessive_depth", "invalid_report", both, "positive", "replace_json", "", `[[[[[[[[[0]]]]]]]]]`, 0, "json.depth"),
		invalid("json_excessive_members", "invalid_report", both, "positive", "replace_object_members", "", "marker", 33, "json.members"),
		invalid("json_excessive_items", "invalid_report", both, "positive", "replace_array_items", "", "0", 33, "json.items"),
		invalid("json_wrong_identity", "invalid_report", both, "positive", "set", "/schema_version", `"http_retry_check.report.v2"`, 0, "json.identity"),
		invalid("json_trailing_data", "invalid_report", both, "positive", "suffix_base64", "", "dHJhaWxpbmc=", 0, "json.trailing"),
	}
	negativeTargets := []string{
		"/summary/scenarios", "/summary/passed", "/summary/failed", "/summary/inconclusive",
		"/scenarios/0/observation/attempt_count", "/scenarios/0/observation/effect_count",
		"/scenarios/0/observation/overlap_count", "/scenarios/0/observation/retry_after_effect_count",
		"/scenarios/0/observation/retry_after_unconfirmed_count", "/scenarios/0/observation/retry_before_response_count",
		"/scenarios/0/observation/response_attempt_count", "/scenarios/0/observation/response_complete_count",
		"/scenarios/0/observation/delay_complete_count",
	}
	for _, target := range negativeTargets {
		id := "json_negative_" + strings.ReplaceAll(strings.TrimPrefix(target, "/"), "/", "_")
		canonicalJSON = append(canonicalJSON, invalid(id, "invalid_report", both, "positive", "set", target, "-1", 0, "json.number.negative", "json.number.negative"+target))
	}

	artifact := []invalidCase{
		invalid("artifact_payload_detachment", "detached", both, "positive", "native_alias_probe", "payloads", "", 0, "artifact.files.independent"),
		invalid("artifact_missing_file", "invalid_artifact", both, "positive", "remove_file", "summary.md", "", 0, "artifact.files.missing"),
		invalid("artifact_extra_file", "invalid_artifact", both, "positive", "add_file", "marker.txt", "", 0, "artifact.files.extra"),
		invalid("artifact_reordered_files", "invalid_artifact", both, "positive", "swap_files", "report.json", "junit.xml", 0, "artifact.files.reordered"),
		invalid("artifact_aliased_payload", "invalid_artifact", []string{"go"}, "positive", "alias_contents", "report.json", "junit.xml", 0, "artifact.files.aliased"),
		invalid("artifact_duplicate_file", "invalid_artifact", both, "positive", "duplicate_file", "report.json", "", 0, "artifact.files.duplicate"),
		invalid("artifact_corrupt_file", "invalid_artifact", both, "positive", "flip_byte", "report.json", "0", 0, "artifact.files.corrupt"),
		invalid("artifact_manifest_whitespace", "invalid_artifact", both, "positive", "insert_whitespace", "manifest.json", " ", 0, "artifact.manifest.whitespace"),
		invalid("artifact_manifest_order", "invalid_artifact", both, "positive", "swap_members", "/schema_version", "/report_schema_version", 0, "artifact.manifest.order"),
		invalid("artifact_manifest_unknown_member", "invalid_artifact", both, "positive", "add_member", "/marker_unknown", "true", 0, "artifact.manifest.unknown"),
		invalid("artifact_manifest_identity_drift", "invalid_artifact", both, "positive", "set", "/schema_version", `"http_retry_check.artifact_manifest.v2"`, 0, "artifact.manifest.identity"),
		invalid("artifact_crossed_projection", "invalid_artifact", both, "positive", "replace_file_from_case", "summary.md", "mixed", 0, "artifact.projection.crossed"),
		invalid("artifact_file_above_max", "invalid_artifact", both, "positive", "resize_file", "report.json", "x", 262145, "artifact.bound.file"),
		invalid("artifact_aggregate_above_max", "invalid_artifact", both, "positive", "resize_set", "", "x", 1048577, "artifact.bound.aggregate"),
		invalid("artifact_build_invalid_report", "invalid_artifact", both, "report_schema_drift", "build_artifact", "", "", 0, "artifact.build.invalid_report"),
		invalid("artifact_build_null_report", "invalid_artifact", []string{"csharp"}, "", "build_artifact_null", "", "", 0, "artifact.build.null_report"),
		invalid("artifact_file_null_constructor", "invalid_artifact", []string{"csharp"}, "", "construct_file_null", "", "", 0, "artifact.file.null_constructor"),
		invalid("artifact_file_malformed_constructor", "invalid_artifact", []string{"csharp"}, "", "construct_file_malformed", "", "", 0, "artifact.file.malformed_constructor"),
		invalid("artifact_constructible_malformed_value", "invalid_artifact", []string{"go"}, "positive", "mutate_file_name", "report.json", "marker", 0, "artifact.go.malformed"),
		invalid("artifact_namespace_alias", "invalid_artifact", []string{"go"}, "positive", "mutate_file_name", "report.json", "./report.json", 0, "artifact.namespace.alias"),
		invalid("artifact_validate_null", "invalid_artifact", []string{"csharp"}, "", "validate_null", "", "", 0, "artifact.validate.null"),
		invalid("artifact_validate_nil", "invalid_artifact", []string{"go"}, "", "validate_nil", "", "", 0, "artifact.validate.nil"),
		invalid("artifact_mutation_after_validation", "invalid_artifact", both, "positive", "mutate_after_validate", "report.json", "0", 0, "artifact.validate.mutation"),
	}

	sanitation := []invalidCase{
		invalid("sanitation_callback_error_value", "marker_absent", both, "", "inject_marker", "callback_error_value", markerValue, 0, "sanitation.callback.error_value"),
		invalid("sanitation_callback_error_type", "marker_absent", both, "", "inject_marker", "callback_error_type", markerValue, 0, "sanitation.callback.error_type"),
		invalid("sanitation_panic_value", "marker_absent", []string{"go"}, "", "inject_marker", "panic_value", markerValue, 0, "sanitation.panic"),
		invalid("sanitation_response_reason", "marker_absent", both, "", "inject_marker", "response_reason", markerValue, 0, "sanitation.response.reason"),
		invalid("sanitation_response_header", "marker_absent", both, "", "inject_marker", "response_header", markerValue, 0, "sanitation.response.header"),
		invalid("sanitation_response_content", "marker_absent", both, "", "inject_marker", "response_content", markerValue, 0, "sanitation.response.content"),
		invalid("sanitation_request_url", "marker_absent", both, "", "inject_marker", "request_url", markerValue, 0, "sanitation.request.url"),
		invalid("sanitation_request_header", "marker_absent", both, "", "inject_marker", "request_header", markerValue, 0, "sanitation.request.header"),
		invalid("sanitation_request_body", "marker_absent", both, "", "inject_marker", "request_body", markerValue, 0, "sanitation.request.body"),
		invalid("sanitation_replay_behavior", "marker_absent", both, "", "inject_marker", "replay_behavior", markerValue, 0, "sanitation.request.replay"),
		invalid("sanitation_sender_type", "marker_absent", both, "", "inject_marker", "client_type", markerValue, 0, "sanitation.client.type"),
		invalid("sanitation_sender_formatter", "marker_absent", both, "", "inject_marker", "client_formatter", markerValue, 0, "sanitation.client.formatter"),
		invalid("sanitation_filesystem_location", "marker_absent", []string{"cli"}, "", "inject_marker", "filesystem_path", markerValue, 0, "sanitation.filesystem.path"),
		invalid("sanitation_io_error", "marker_absent", []string{"cli"}, "", "inject_marker", "io_error", markerValue, 0, "sanitation.filesystem.error"),
		invalid("sanitation_environment_proxy", "marker_absent", []string{"go", "csharp", "cli"}, "", "inject_marker", "environment_proxy", markerValue, 0, "sanitation.environment.proxy"),
		invalid("sanitation_environment_credential", "marker_absent", []string{"go", "csharp", "cli"}, "", "inject_marker", "environment_credential", markerValue, 0, "sanitation.environment.credential"),
	}

	authority := []invalidCase{
		invalid("authority_nil_findings", "invalid_result", []string{"go"}, "result_all_positive", "native_nil_collection", "/scenarios/0/findings", "", 0, "authority.go.nil"),
		invalid("authority_value_aliasing", "detached", []string{"go"}, "result_all_positive", "native_alias_probe", "/scenarios", "", 0, "authority.go.aliasing"),
		invalid("authority_constructor_aliasing", "detached", []string{"csharp"}, "result_all_positive", "native_alias_probe", "/scenarios", "", 0, "authority.csharp.aliasing"),
		invalid("authority_stable_api_surface", "exact_surface", []string{"go"}, "", "source_surface", "pkg/httpcheck/v1", "", 0, "authority.go.surface"),
		invalid("authority_native_api_surface", "exact_surface", []string{"csharp"}, "", "source_surface", "HttpRetryCheck.V1", "", 0, "authority.csharp.surface"),
		invalid("authority_no_cross_binding_import", "forbidden_absent", both, "", "source_import", "other_binding", "", 0, "authority.cross_binding"),
		invalid("authority_cli_no_runtime_import", "forbidden_absent", []string{"cli"}, "", "source_import", "native_http_runtime", "", 0, "authority.cli.runtime"),
		invalid("authority_cli_no_execution", "forbidden_absent", []string{"cli"}, "", "source_capability", "process_plugin_rpc_package_test_network", "", 0, "authority.cli.execution"),
		invalid("authority_runtime_literal_loopback", "native_control", both, "", "native_runtime", "literal_127_0_0_1", "", 0, "authority.runtime.loopback"),
		invalid("authority_runtime_no_ambient_proxy", "native_control", both, "", "native_runtime", "proxy_free_http1", "", 0, "authority.runtime.proxy"),
		invalid("authority_runtime_same_instance_order", "native_control", both, "", "native_runtime", "same_instance_canonical_order", "", 0, "authority.runtime.order"),
		invalid("authority_runtime_concurrent_independence", "native_control", both, "", "native_runtime", "concurrent_independence", "", 0, "authority.runtime.concurrency"),
		invalid("authority_cli_no_follow_persistence", "native_control", []string{"cli"}, "", "filesystem_boundary", "no_follow_atomic_no_replace", "", 0, "authority.cli.persistence"),
	}

	return map[string]invalidBundle{
		"invalid/semantic.json":       {SchemaVersion: invalidBundleIdentity, Category: "semantic", Cases: semantic},
		"invalid/report-model.json":   {SchemaVersion: invalidBundleIdentity, Category: "report_model", Cases: reportModel},
		"invalid/canonical-json.json": {SchemaVersion: invalidBundleIdentity, Category: "canonical_json", Cases: canonicalJSON},
		"invalid/artifact.json":       {SchemaVersion: invalidBundleIdentity, Category: "artifact", Cases: artifact},
		"invalid/sanitation.json":     {SchemaVersion: invalidBundleIdentity, Category: "sanitation", Marker: markerValue, Cases: sanitation},
		"invalid/authority.json":      {SchemaVersion: invalidBundleIdentity, Category: "authority", Cases: authority},
	}
}

func positiveAccept() observation {
	return observation{CaptureComplete: true, AttemptCount: 1, EffectCount: 1, MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true, Credential: "source_only", Cleanup: "succeeded"}
}

func positiveDisconnect() observation {
	return observation{CaptureComplete: true, AttemptCount: 1, MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true, Credential: "source_only", Cleanup: "succeeded"}
}

func positiveChanged() observation { return positiveAccept() }

func positiveRedirectRefused() observation {
	return observation{CaptureComplete: true, AttemptCount: 1, ResponseAttemptCount: 1,
		ResponseCompleteCount: 1, FirstResponseComplete: true, MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true, Credential: "source_only", Cleanup: "succeeded"}
}

func positiveRedirectFollowed() observation {
	return observation{CaptureComplete: true, AttemptCount: 2, EffectCount: 1, ResponseAttemptCount: 2,
		ResponseCompleteCount: 2, FirstResponseComplete: true, MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true, Credential: "absent_at_target", Cleanup: "succeeded"}
}

func positiveRetryOne() observation { return positiveRedirectRefused() }

func positiveRetryTwo() observation {
	value := positiveRedirectRefused()
	value.AttemptCount, value.ResponseAttemptCount, value.ResponseCompleteCount = 2, 2, 2
	return value
}

func positiveDelayedComplete() observation {
	value := positiveAccept()
	value.ResponseAttemptCount, value.ResponseCompleteCount = 1, 1
	value.FirstResponseComplete, value.DelayCompleteCount = true, 1
	return value
}

func positiveDelayedUnobserved() observation {
	value := positiveDelayedComplete()
	value.ResponseCompleteCount, value.FirstResponseComplete = 0, false
	return value
}

func withCounts(value observation, attempts uint32, effects uint64, overlap, afterEffect, afterUnconfirmed, beforeResponse, responseAttempts, responseComplete uint32, first bool, delay uint32) observation {
	value.AttemptCount, value.EffectCount, value.OverlapCount = attempts, effects, overlap
	value.RetryAfterEffectCount, value.RetryAfterUnconfirmedCount = afterEffect, afterUnconfirmed
	value.RetryBeforeResponseCount, value.ResponseAttemptCount = beforeResponse, responseAttempts
	value.ResponseCompleteCount, value.FirstResponseComplete = responseComplete, first
	value.DelayCompleteCount = delay
	return value
}

func captureIncomplete(value observation) observation { value.CaptureComplete = false; return value }
func methodChanged(value observation) observation     { value.MethodConsistent = false; return value }
func destinationChanged(value observation) observation {
	value.DestinationConsistent = false
	return value
}
func bodyChanged(value observation) observation              { value.BodyConsistent = false; return value }
func credential(value observation, state string) observation { value.Credential = state; return value }
func cleanup(value observation, state string) observation    { value.Cleanup = state; return value }

func delayedRetryBeforeResponse() observation {
	value := positiveDelayedComplete()
	value.CaptureComplete = false
	value.AttemptCount, value.EffectCount = 2, 2
	value.RetryAfterEffectCount, value.RetryBeforeResponseCount = 1, 1
	value.ResponseAttemptCount, value.ResponseCompleteCount = 2, 1
	value.FirstResponseComplete, value.DelayCompleteCount = false, 2
	return value
}

func acceptMaxCounts() observation {
	value := positiveAccept()
	value.AttemptCount, value.EffectCount, value.RetryAfterEffectCount = 3, 3, 2
	return value
}

func disconnectMaxCounts() observation {
	value := positiveDisconnect()
	value.AttemptCount, value.RetryAfterUnconfirmedCount = 3, 2
	return value
}

func delayedMaxCounts() observation {
	value := positiveDelayedComplete()
	value.CaptureComplete = false
	value.AttemptCount, value.EffectCount, value.RetryAfterEffectCount = 3, 3, 2
	value.RetryBeforeResponseCount = 2
	value.ResponseAttemptCount, value.ResponseCompleteCount = 3, 3
	value.DelayCompleteCount = 3
	return value
}

func retryOverlapMax() observation {
	value := positiveRetryOne()
	value.CaptureComplete = false
	value.AttemptCount, value.OverlapCount = 3, 2
	value.ResponseAttemptCount, value.ResponseCompleteCount = 3, 3
	return value
}

func unrun(cleanupState string) observation {
	return observation{MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: "not_observed", Cleanup: cleanupState}
}

func redirectResponseIncomplete() observation {
	value := positiveRedirectRefused()
	value.ResponseCompleteCount, value.FirstResponseComplete = 0, false
	return value
}

func delayedIncomplete() observation {
	value := positiveAccept()
	value.CaptureComplete = false
	return value
}

func credentialNotObserved() observation {
	value := positiveDisconnect()
	value.CaptureComplete, value.Credential = false, "not_observed"
	return value
}

func redirectEffectNotObserved() observation {
	value := positiveRetryTwo()
	value.EffectCount = 0
	return value
}

func retryLaterResponseComplete() observation {
	value := positiveRetryTwo()
	value.ResponseCompleteCount, value.FirstResponseComplete = 1, false
	return value
}

func newRow(t *testing.T, id, scenario string, value observation, coverage ...string) rowCase {
	t.Helper()
	if !validObservation(scenario, value) {
		t.Fatalf("generator row %s is not a valid observation", id)
	}
	assessment, findings := assess(scenario, value)
	return rowCase{ID: id, Representability: []string{"go", "csharp"}, Scenario: scenario,
		Observation: value, Assessment: assessment, Findings: findings, Coverage: coverage}
}

func newResult(t *testing.T, rows map[string]rowCase, id string, references []string, coverage ...string) resultCase {
	t.Helper()
	if len(references) != len(scenarios) {
		t.Fatalf("result %s has %d rows", id, len(references))
	}
	aggregate := assessmentPositive
	for index, reference := range references {
		row, ok := rows[reference]
		if !ok || row.Scenario != scenarios[index] {
			t.Fatalf("result %s row %d has bad reference %q", id, index, reference)
		}
		aggregate = combineAssessment(aggregate, row.Assessment)
	}
	return resultCase{ID: id, Representability: []string{"go", "csharp"}, Assessment: aggregate,
		Rows: append([]string{}, references...), Coverage: coverage}
}

func invalid(id, expected string, representability []string, baseCase, operation, target, operand string, count uint64, coverage ...string) invalidCase {
	return invalidCase{ID: id, Expected: expected, Representability: append([]string{}, representability...),
		BaseCase: baseCase, Operation: operation, Target: target, Operand: operand, Count: count,
		Coverage: coverage}
}

func defaultPositiveRows() []string {
	return []string{"positive_accept_then_disconnect", "positive_disconnect_before_acceptance",
		"positive_changed_body_retry", "positive_redirect_followed", "positive_retry_limit_two",
		"positive_delayed_response_complete"}
}

func collectRows(bundles map[string]rowBundle) map[string]rowCase {
	rows := make(map[string]rowCase)
	for _, bundle := range bundles {
		for _, row := range bundle.Cases {
			rows[row.ID] = row
		}
	}
	return rows
}

func collectResults(bundles map[string]resultBundle) map[string]resultCase {
	results := make(map[string]resultCase)
	for _, bundle := range bundles {
		for _, result := range bundle.Cases {
			results[result.ID] = result
		}
	}
	return results
}

func writeProjections(t *testing.T, root string, rowBundles map[string]rowBundle, resultBundles map[string]resultBundle) {
	rows := collectRows(rowBundles)
	results := collectResults(resultBundles)
	cases := map[string]string{
		"positive":     "result_all_positive",
		"unsafe":       "result_unsafe_cross_origin_redirect_credentials",
		"inconclusive": "result_inconclusive_delayed_response",
		"mixed":        "result_mixed_unsafe_over_inconclusive",
	}
	for directory, caseID := range cases {
		reportValue := buildNeutralProjectionReport(t, rows, results[caseID])
		reportBytes := marshalCanonical(t, reportValue)
		junitBytes := renderNeutralJUnit(t, reportValue)
		summaryBytes := renderNeutralMarkdown(reportValue)
		manifestBytes := marshalCanonical(t, buildNeutralArtifactManifest(reportBytes, junitBytes, summaryBytes))
		base := filepath.ToSlash(filepath.Join("projections", directory))
		writeBytes(t, root, base+"/manifest.json", manifestBytes)
		writeBytes(t, root, base+"/report.json", reportBytes)
		writeBytes(t, root, base+"/junit.xml", junitBytes)
		writeBytes(t, root, base+"/summary.md", summaryBytes)
	}
}

func buildNeutralProjectionReport(t *testing.T, rows map[string]rowCase, value resultCase) neutralReport {
	t.Helper()
	result := neutralReport{
		SchemaVersion: reportIdentity, SuiteIdentity: suiteIdentity,
		ExplanationIdentity: explanationIdentity, ClaimCeiling: claimCeiling,
		Assessment: value.Assessment, AssessmentText: assessmentText(value.Assessment),
		Outcome: map[string]string{
			assessmentPositive: "pass", assessmentUnsafe: "fail", assessmentInconclusive: "inconclusive",
		}[value.Assessment],
		Summary:   neutralSummary{Scenarios: uint32(len(value.Rows))},
		Scenarios: make([]neutralReportRow, len(value.Rows)),
	}
	for index, reference := range value.Rows {
		row, ok := rows[reference]
		if !ok {
			t.Fatalf("projection result %s references unknown row %s", value.ID, reference)
		}
		findings := make([]neutralFinding, len(row.Findings))
		for findingIndex, finding := range row.Findings {
			findings[findingIndex] = neutralFinding{Code: finding, Text: findingText(finding)}
		}
		result.Scenarios[index] = neutralReportRow{
			Scenario: row.Scenario, ScenarioText: scenarioText(row.Scenario),
			Assessment: row.Assessment, AssessmentText: assessmentText(row.Assessment),
			Observation: row.Observation, Findings: findings,
		}
		switch row.Assessment {
		case assessmentPositive:
			result.Summary.Passed++
		case assessmentUnsafe:
			result.Summary.Failed++
		case assessmentInconclusive:
			result.Summary.Inconclusive++
		default:
			t.Fatalf("projection result %s has unknown row assessment %q", value.ID, row.Assessment)
		}
	}
	validateNeutralReport(t, result, rows, value)
	return result
}

func buildNeutralArtifactManifest(report, junit, summary []byte) artifactManifest {
	contents := [][]byte{report, junit, summary}
	names := [...]string{"report.json", "junit.xml", "summary.md"}
	mediaTypes := [...]string{"application/json", "application/xml", "text/markdown; charset=utf-8"}
	manifest := artifactManifest{
		SchemaVersion: artifactIdentity, ReportSchemaVersion: reportIdentity,
		ClaimCeiling: claimCeiling, DigestDomain: artifactDigestDomain,
		Files: make([]artifactDescriptor, len(contents)),
	}
	for index, payload := range contents {
		digest := sha256.Sum256(payload)
		manifest.Files[index] = artifactDescriptor{
			Name: names[index], MediaType: mediaTypes[index], SizeBytes: uint64(len(payload)),
			SHA256: hex.EncodeToString(digest[:]),
		}
	}
	digest := sha256.New()
	writeNeutralDigestAtom(digest, artifactDigestDomain)
	writeNeutralDigestAtom(digest, artifactIdentity)
	writeNeutralDigestAtom(digest, reportIdentity)
	writeNeutralDigestAtom(digest, claimCeiling)
	for _, descriptor := range manifest.Files {
		writeNeutralDigestAtom(digest, descriptor.Name)
		writeNeutralDigestAtom(digest, descriptor.MediaType)
		writeNeutralDigestAtom(digest, fmt.Sprint(descriptor.SizeBytes))
		writeNeutralDigestAtom(digest, descriptor.SHA256)
	}
	manifest.AggregateSHA256 = hex.EncodeToString(digest.Sum(nil))
	return manifest
}

func writeCanonical(t *testing.T, root, relative string, value any) {
	t.Helper()
	writeBytes(t, root, relative, marshalCanonical(t, value))
}

func marshalCanonical(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func writeBytes(t *testing.T, root, relative string, encoded []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRootManifest(t *testing.T, root string) {
	t.Helper()
	files := make([]corpusFile, 0, 32)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular corpus entry %s", path)
		}
		relative, err := filepath.Rel(root, path)
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
		files = append(files, corpusFile{Path: relative, Size: uint64(len(contents)), SHA256: hex.EncodeToString(digest[:])})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	writeCanonical(t, root, "manifest.json", corpusManifest{SchemaVersion: manifestIdentity,
		ProductIdentity: productIdentity, SuiteIdentity: suiteIdentity, ReportIdentity: reportIdentity,
		ConformanceIdentity: conformanceIdentity, Files: files})
}
