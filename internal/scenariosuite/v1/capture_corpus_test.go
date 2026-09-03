package scenariosuite

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

type captureCorpus struct {
	SchemaVersion string              `json:"schema_version"`
	Endpoint      string              `json:"endpoint"`
	Cases         []captureCorpusCase `json:"cases"`
}

type captureCorpusCase struct {
	ID         string                   `json:"id"`
	WireBase64 string                   `json:"wire_base64"`
	Expected   captureCorpusExpectation `json:"expected"`
}

type captureCorpusExpectation struct {
	HeadersObserved       bool `json:"headers_observed"`
	Complete              bool `json:"complete"`
	CaptureComplete       bool `json:"capture_complete"`
	MethodConsistent      bool `json:"method_consistent"`
	DestinationConsistent bool `json:"destination_consistent"`
	BodyConsistent        bool `json:"body_consistent"`
	CredentialExact       bool `json:"credential_exact"`
	CredentialExposed     bool `json:"credential_exposed"`
}

func TestSharedCaptureCorpus(t *testing.T) {
	contents := readCaptureCorpus(t)
	var corpus captureCorpus
	if err := json.Unmarshal(contents, &corpus); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.MarshalIndent(corpus, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(canonical, contents) || corpus.SchemaVersion != "http_retry_check.capture_corpus.v1" ||
		corpus.Endpoint != "127.0.0.1:41001" || len(corpus.Cases) != 53 {
		t.Fatalf("capture corpus identity/shape drift: endpoint=%q cases=%d", corpus.Endpoint, len(corpus.Cases))
	}
	address, err := netip.ParseAddrPort(corpus.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(corpus.Cases))
	for _, test := range corpus.Cases {
		if test.ID == "" || seen[test.ID] {
			t.Fatalf("invalid or duplicate capture case ID %q", test.ID)
		}
		seen[test.ID] = true
		wire, err := base64.StdEncoding.Strict().DecodeString(test.WireBase64)
		if err != nil || len(wire) == 0 {
			t.Fatalf("case %q has invalid wire: %v", test.ID, err)
		}
		observed := readScenarioAttempt(
			newMemoryConn(string(wire)), address, time.Now().Add(time.Second),
		)
		clear(wire)
		got := captureCorpusExpectation{
			HeadersObserved: observed.headersObserved, Complete: observed.complete,
			CaptureComplete: observed.captureComplete, MethodConsistent: observed.methodConsistent,
			DestinationConsistent: observed.destinationConsistent, BodyConsistent: observed.bodyConsistent,
			CredentialExact: observed.credentialExact, CredentialExposed: observed.credentialExposed,
		}
		if !reflect.DeepEqual(got, test.Expected) {
			t.Errorf("case %q = %#v, want %#v", test.ID, got, test.Expected)
		}
	}
	for _, required := range []string{
		"baseline_content_length", "content_length_with_chunked", "authorization_obs_fold",
		"protocol_multidigit_major", "protocol_multidigit_minor", "marker_in_cookie",
		"marker_in_custom_header", "marker_in_malformed_head", "marker_in_malformed_raw_target", "marker_in_truncated_head",
		"marker_in_body_only", "raw_target_invalid_percent", "raw_target_absolute_form",
		"raw_target_delete_control", "content_length_leading_zero", "content_length_lexical_duplicate",
		"body_changed_then_truncated",
		"chunk_trailer_value_delete_control",
		"transfer_encoding_unicode_kelvin",
		"chunk_extension_quoted_valid", "chunk_extension_bare_semicolon", "chunk_extension_name_malformed",
		"chunk_extension_value_delete_control", "chunk_extension_quoted_unclosed", "chunk_extension_empty_token_value",
	} {
		if !seen[required] {
			t.Errorf("capture corpus is missing required case %q", required)
		}
	}
}

func TestMalformedRedirectHeadPreservesUnsafeCredentialEvidence(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioCrossOriginRedirectCredentials, func(context.Context) bool { return true },
	)
	exchangeRawRequest(t, origin.endpoints[0].address, rawRequest(
		origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
		syntheticBodyText, len(syntheticBodyText),
	))
	sendRawAndClose(t, origin.endpoints[1].address,
		"POST /case?value="+syntheticCredentialMarker+"\x7f HTTP/1.1\r\nHost: "+
			origin.endpoints[1].address.String()+"\r\nContent-Length: 0\r\n\r\n")
	waitForAttemptCount(t, origin, 2)
	deadline := time.Now().Add(time.Second)
	for {
		origin.mu.Lock()
		pending := len(origin.connections)
		origin.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("malformed target capture did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, valid := newScenarioResult(ScenarioCrossOriginRedirectCredentials, observation)
	if !valid || row.Assessment != AssessmentUnsafeBehaviorObserved || observation.CaptureComplete ||
		observation.Credential != CredentialExposedAtTarget ||
		!containsFinding(row.Findings, FindingCaptureIncomplete) ||
		!containsFinding(row.Findings, FindingCredentialExposedAtTarget) {
		t.Fatalf("malformed redirect exposure = %#v, attempts=%#v, valid=%v", row, origin.attempts, valid)
	}
}

func TestChangedThenTruncatedReplayPreservesUnsafeBodyEvidence(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioChangedBodyRetry, func(context.Context) bool { return true },
	)
	sendRawAndClose(t, origin.endpoints[0].address, rawRequest(
		origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
		syntheticBodyText, len(syntheticBodyText),
	))
	waitForOriginState(t, origin, func(origin *scenarioOrigin) bool {
		return origin.effectCount == 1 && len(origin.connections) == 0
	})
	sendRawAndClose(t, origin.endpoints[0].address, rawRequest(
		origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
		"x", len(syntheticBodyText),
	))
	waitForOriginState(t, origin, func(origin *scenarioOrigin) bool {
		return len(origin.attempts) == 2 && len(origin.connections) == 0
	})
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, valid := newScenarioResult(ScenarioChangedBodyRetry, observation)
	if !valid || row.Assessment != AssessmentUnsafeBehaviorObserved || observation.CaptureComplete ||
		observation.BodyConsistent || observation.EffectCount != 1 ||
		observation.RetryAfterEffectCount != 1 ||
		!containsFinding(row.Findings, FindingCaptureIncomplete) ||
		!containsFinding(row.Findings, FindingRetryAfterAcceptedRequest) ||
		!containsFinding(row.Findings, FindingBodyChanged) {
		t.Fatalf("changed truncated replay = %#v, valid=%v", row, valid)
	}
}

func waitForOriginState(t *testing.T, origin *scenarioOrigin, ready func(*scenarioOrigin) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		origin.mu.Lock()
		complete := ready(origin)
		origin.mu.Unlock()
		if complete {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("origin state did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func readCaptureCorpus(t *testing.T) []byte {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("capture corpus source path unavailable")
	}
	path := filepath.Join(filepath.Dir(current), "..", "..", "..", "conformance",
		"http-retry-check", "v1", "capture", "request-wires.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
