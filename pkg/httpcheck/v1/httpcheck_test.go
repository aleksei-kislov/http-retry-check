package httpcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	legacysuite "github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1"
)

const sanitationMarker = "RAW-HTTP-CHECK-V1-MARKER-127.0.0.1:49152"

func TestRunMapsPositiveSuiteIntoIndependentV1Values(t *testing.T) {
	first := runSafeSuite(t)
	second := runSafeSuite(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeat result drifted: %#v / %#v", first, second)
	}
	if first.Assessment != AssessmentNoUnsafeBehaviorObserved || len(first.Scenarios) != 6 {
		t.Fatalf("result shape = %#v", first)
	}
	want := []Observation{
		positiveObservation(1, 1, 0, 0, false, 0, CredentialSourceOnly),
		positiveObservation(1, 0, 0, 0, false, 0, CredentialSourceOnly),
		positiveObservation(1, 1, 0, 0, false, 0, CredentialSourceOnly),
		positiveObservation(2, 1, 2, 2, true, 0, CredentialAbsentAtTarget),
		positiveObservation(1, 0, 1, 1, true, 0, CredentialSourceOnly),
		positiveObservation(1, 1, 1, 1, true, 1, CredentialSourceOnly),
	}
	allocations := make(map[uintptr]bool, len(first.Scenarios))
	for index, row := range first.Scenarios {
		if row.Assessment != AssessmentNoUnsafeBehaviorObserved || row.Findings == nil ||
			len(row.Findings) != 0 || cap(row.Findings) == 0 || !reflect.DeepEqual(row.Observation, want[index]) {
			t.Fatalf("row %d = %#v, want %#v", index, row, want[index])
		}
		pointer := reflect.ValueOf(row.Findings).Pointer()
		if pointer == 0 || allocations[pointer] {
			t.Fatalf("row %d does not own a distinct findings allocation", index)
		}
		allocations[pointer] = true
	}
	first.Scenarios[0].Findings = append(first.Scenarios[0].Findings, FindingBodyChanged)
	first.Scenarios[1].Observation.AttemptCount = 3
	if len(second.Scenarios[0].Findings) != 0 || second.Scenarios[1].Observation.AttemptCount != 1 {
		t.Fatal("one result mutation affected a separately returned result")
	}
}

func TestValidateAcceptsAllNinePositiveTuples(t *testing.T) {
	tests := []struct {
		name        string
		scenario    ScenarioID
		observation Observation
	}{
		{"accept", ScenarioAcceptThenDisconnect, positiveObservation(1, 1, 0, 0, false, 0, CredentialSourceOnly)},
		{"disconnect", ScenarioDisconnectBeforeAcceptance, positiveObservation(1, 0, 0, 0, false, 0, CredentialSourceOnly)},
		{"changed", ScenarioChangedBodyRetry, positiveObservation(1, 1, 0, 0, false, 0, CredentialSourceOnly)},
		{"redirect refused", ScenarioCrossOriginRedirectCredentials, positiveObservation(1, 0, 1, 1, true, 0, CredentialSourceOnly)},
		{"redirect followed", ScenarioCrossOriginRedirectCredentials, positiveObservation(2, 1, 2, 2, true, 0, CredentialAbsentAtTarget)},
		{"retry once", ScenarioRetryLimit, positiveObservation(1, 0, 1, 1, true, 0, CredentialSourceOnly)},
		{"retry twice", ScenarioRetryLimit, positiveObservation(2, 0, 2, 2, true, 0, CredentialSourceOnly)},
		{"delay write complete", ScenarioDelayedResponse, positiveObservation(1, 1, 1, 1, true, 1, CredentialSourceOnly)},
		{"delay write incomplete", ScenarioDelayedResponse, positiveObservation(1, 1, 1, 0, false, 1, CredentialSourceOnly)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := canonicalPositiveResult()
			for index, row := range result.Scenarios {
				if row.Scenario == test.scenario {
					assessment, findings := assess(test.scenario, test.observation)
					result.Scenarios[index].Assessment = assessment
					result.Scenarios[index].Observation = test.observation
					result.Scenarios[index].Findings = findings
					break
				}
			}
			if err := Validate(result); err != nil {
				t.Fatalf("positive tuple rejected: %v (%#v)", err, result)
			}
		})
	}
}

func TestRetryLimitUsesRecordedAttemptLimit(t *testing.T) {
	for attempts := uint32(1); attempts <= 4; attempts++ {
		t.Run(strconv.FormatUint(uint64(attempts), 10), func(t *testing.T) {
			result := canonicalPositiveResult()
			observation := positiveObservation(
				attempts, 0, attempts, attempts, true, 0, CredentialSourceOnly,
			)
			observation.AttemptLimit = 3
			assessment, findings := assess(ScenarioRetryLimit, observation)
			row := &result.Scenarios[4]
			row.Assessment = assessment
			row.Observation = observation
			row.Findings = findings
			result.Assessment = assessment
			if err := Validate(result); err != nil {
				t.Fatalf("limit-three result rejected: %v (%#v)", err, result)
			}
			if attempts <= 3 {
				if assessment != AssessmentNoUnsafeBehaviorObserved || len(findings) != 0 {
					t.Fatalf("bounded row = %#v", row)
				}
				return
			}
			if assessment != AssessmentUnsafeBehaviorObserved ||
				!containsFinding(findings, FindingAttemptLimitExceeded) {
				t.Fatalf("over-limit row = %#v", row)
			}
		})
	}
}

func TestValidateRejectsForgedOrContradictoryValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Result)
	}{
		{"nil scenarios", func(result *Result) { result.Scenarios = nil }},
		{"partial", func(result *Result) { result.Scenarios = result.Scenarios[:5] }},
		{"reordered", func(result *Result) {
			result.Scenarios[0], result.Scenarios[1] = result.Scenarios[1], result.Scenarios[0]
		}},
		{"aggregate", func(result *Result) { result.Assessment = AssessmentInconclusive }},
		{"row", func(result *Result) { result.Scenarios[0].Assessment = AssessmentUnsafeBehaviorObserved }},
		{"nil findings", func(result *Result) { result.Scenarios[0].Findings = nil }},
		{"too many findings", func(result *Result) {
			result.Scenarios[0].Findings = make([]FindingCode, maxResultFindings+1)
		}},
		{"finding order", func(result *Result) {
			row := unsafeFirstRowResult().Scenarios[0]
			result.Scenarios[0] = row
			result.Scenarios[0].Findings[0], result.Scenarios[0].Findings[1] =
				result.Scenarios[0].Findings[1], result.Scenarios[0].Findings[0]
			result.Assessment = AssessmentUnsafeBehaviorObserved
		}},
		{"attempt overflow", func(result *Result) { result.Scenarios[0].Observation.AttemptCount = 4 }},
		{"attempt limit zero", func(result *Result) { result.Scenarios[0].Observation.AttemptLimit = 0 }},
		{"attempt limit above maximum", func(result *Result) { result.Scenarios[0].Observation.AttemptLimit = 4 }},
		{"unknown protocol", func(result *Result) { result.Scenarios[0].Observation.Protocol = "HTTP/2" }},
		{"complete protocol missing", func(result *Result) { result.Scenarios[0].Observation.Protocol = "" }},
		{"complete without attempt", func(result *Result) {
			row := &result.Scenarios[0].Observation
			*row = Observation{
				CaptureComplete: true, AttemptLimit: 2, MethodConsistent: true,
				DestinationConsistent: true, BodyConsistent: true,
				Credential: CredentialNotObserved, Cleanup: CleanupSucceeded,
			}
		}},
		{"false positive", func(result *Result) { result.Scenarios[0].Observation.CaptureComplete = false }},
		{"unknown credential", func(result *Result) { result.Scenarios[0].Observation.Credential = CredentialState(sanitationMarker) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			forged := cloneResult(canonicalPositiveResult())
			test.edit(&forged)
			before := cloneResult(forged)
			if err := Validate(forged); err != ErrInvalidResult {
				t.Fatalf("Validate error = %v, want %v", err, ErrInvalidResult)
			}
			if !reflect.DeepEqual(forged, before) {
				t.Fatal("Validate mutated its input")
			}
		})
	}
}

func TestRunRejectsInvalidCallsBeforeInvocation(t *testing.T) {
	doer := new(countingDoer)
	tests := []struct {
		name string
		ctx  context.Context
		doer Doer
	}{
		{"nil context", nil, doer},
		{"nil doer", context.Background(), nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Run(test.ctx, test.doer)
			if err != ErrInvalidCall || !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("Run = %#v/%v", result, err)
			}
		})
	}
	if doer.calls.Load() != 0 {
		t.Fatalf("invalid calls invoked Do %d times", doer.calls.Load())
	}
}

func TestRunRejectsInvalidOptionsBeforeInvocation(t *testing.T) {
	doer := new(countingDoer)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		parent  context.Context
		options []Option
	}{
		{name: "nil", options: []Option{nil}},
		{name: "scenario zero", options: []Option{WithScenarioTimeout(0)}},
		{name: "scenario negative", options: []Option{WithScenarioTimeout(-1)}},
		{name: "scenario above maximum", options: []Option{WithScenarioTimeout(time.Minute + 1)}},
		{name: "connection zero", options: []Option{WithConnectionTimeout(0)}},
		{name: "connection negative", options: []Option{WithConnectionTimeout(-1)}},
		{name: "connection above maximum", options: []Option{WithConnectionTimeout(time.Minute + 1)}},
		{name: "quiet zero", options: []Option{WithQuietWindow(0)}},
		{name: "quiet negative", options: []Option{WithQuietWindow(-1)}},
		{name: "quiet above maximum", options: []Option{WithQuietWindow(time.Minute + 1)}},
		{name: "attempt zero", options: []Option{WithAttemptLimit(0)}},
		{name: "attempt above maximum", options: []Option{WithAttemptLimit(4)}},
		{name: "last invalid wins", options: []Option{
			WithAttemptLimit(2), WithAttemptLimit(4),
		}},
		{name: "invalid precedes canceled parent", parent: canceled, options: []Option{WithAttemptLimit(4)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := test.parent
			if parent == nil {
				parent = context.Background()
			}
			result, err := Run(parent, doer, test.options...)
			if err != ErrInvalidCall || !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("Run = %#v/%v", result, err)
			}
		})
	}
	if doer.calls.Load() != 0 {
		t.Fatalf("invalid options invoked Do %d times", doer.calls.Load())
	}
}

func TestRunOptionsAreLastWinsAndRecordFinalSettings(t *testing.T) {
	client, transport := safeClient()
	defer transport.CloseIdleConnections()
	result, err := Run(
		context.Background(), client,
		WithScenarioTimeout(0), WithScenarioTimeout(2*time.Second),
		WithConnectionTimeout(0), WithConnectionTimeout(time.Second),
		WithQuietWindow(0), WithQuietWindow(time.Millisecond),
		WithAttemptLimit(0), WithAttemptLimit(3),
	)
	if err != nil || Validate(result) != nil ||
		result.Assessment != AssessmentNoUnsafeBehaviorObserved {
		t.Fatalf("configured Run = %#v/%v", result, err)
	}
	for _, row := range result.Scenarios {
		if row.Observation.AttemptLimit != 3 || row.Observation.Protocol != "HTTP/1.1" {
			t.Fatalf("configured observation = %#v", row.Observation)
		}
	}
}

func TestAttemptLimitOneMakesSecondAttemptUnsafe(t *testing.T) {
	client, transport := safeClient()
	defer transport.CloseIdleConnections()
	result, err := Run(
		context.Background(), client, WithAttemptLimit(1), WithQuietWindow(time.Millisecond),
	)
	if err != nil || Validate(result) != nil || result.Assessment != AssessmentUnsafeBehaviorObserved {
		t.Fatalf("limit-one Run = %#v/%v", result, err)
	}
	redirect := result.Scenarios[3]
	if redirect.Observation.AttemptCount != 2 || redirect.Observation.AttemptLimit != 1 ||
		redirect.Observation.Protocol != "HTTP/1.1" ||
		!containsFinding(redirect.Findings, FindingAttemptLimitExceeded) {
		t.Fatalf("limit-one redirect = %#v", redirect)
	}
}

func TestRunReturnsPreexistingParentCancellationWithoutInvocation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	doer := new(countingDoer)

	result, err := Run(parent, doer, WithScenarioTimeout(time.Minute), WithQuietWindow(time.Millisecond))
	if err != parent.Err() || err != context.Canceled || !reflect.DeepEqual(result, Result{}) ||
		doer.calls.Load() != 0 {
		t.Fatalf("Run = %#v/%v, calls=%d", result, err, doer.calls.Load())
	}
}

func TestRunPassesThroughCancellationWithValidPartialResult(t *testing.T) {
	client, transport := safeClient()
	defer transport.CloseIdleConnections()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	doer := &cancelOnSecondDoer{client: client, cancel: cancel}

	result, err := Run(parent, doer)
	if err != parent.Err() || err != context.Canceled || doer.calls.Load() != 2 ||
		Validate(result) != nil || len(result.Scenarios) != 6 ||
		result.Scenarios[0].Assessment != AssessmentNoUnsafeBehaviorObserved ||
		result.Scenarios[1].Assessment != AssessmentInconclusive {
		t.Fatalf("cancelled Run = %#v/%v, calls=%d", result, err, doer.calls.Load())
	}
}

func TestScenarioTimeoutIsBoundedByParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := Run(parent, waitForContextDoer{}, WithScenarioTimeout(time.Minute))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) >= time.Second ||
		Validate(result) != nil || result.Assessment != AssessmentInconclusive {
		t.Fatalf("deadline-bounded Run = %#v/%v after %v", result, err, time.Since(started))
	}
}

func TestLegacyRunFailuresMapOnlyToFixedV1Classes(t *testing.T) {
	tests := []struct {
		source error
		want   RunError
	}{
		{legacysuite.ErrInvalidCall, ErrInvalidCall},
		{legacysuite.ErrSuiteUnavailable, ErrSuiteUnavailable},
		{legacysuite.ErrInternalFailure, ErrInternalFailure},
		{legacysuite.ErrInvalidResult, ErrInvalidResult},
		{errors.New(sanitationMarker), ErrInternalFailure},
	}
	for _, test := range tests {
		if got := mapScenarioSuiteRunError(test.source); got != test.want ||
			strings.Contains(got.Error(), sanitationMarker) {
			t.Errorf("mapScenarioSuiteRunError = %v, want %v", got, test.want)
		}
	}
}

func TestLegacyVocabularyAndObservationMapping(t *testing.T) {
	for _, test := range []struct {
		legacy legacysuite.ScenarioID
		want   ScenarioID
	}{
		{legacysuite.ScenarioAcceptThenDisconnect, ScenarioAcceptThenDisconnect},
		{legacysuite.ScenarioDisconnectBeforeAcceptance, ScenarioDisconnectBeforeAcceptance},
		{legacysuite.ScenarioChangedBodyRetry, ScenarioChangedBodyRetry},
		{legacysuite.ScenarioCrossOriginRedirectCredentials, ScenarioCrossOriginRedirectCredentials},
		{legacysuite.ScenarioRetryLimit, ScenarioRetryLimit},
		{legacysuite.ScenarioDelayedResponse, ScenarioDelayedResponse},
	} {
		if got, ok := mapScenarioSuiteScenario(test.legacy); !ok || got != test.want {
			t.Errorf("scenario mapping = %q/%t, want %q", got, ok, test.want)
		}
	}
	if got, ok := mapScenarioSuiteScenario(legacysuite.ScenarioID(sanitationMarker)); ok || got != "" {
		t.Errorf("unknown scenario mapping = %q/%t", got, ok)
	}
	for _, test := range []struct {
		legacy legacysuite.Assessment
		want   Assessment
	}{
		{legacysuite.AssessmentNoUnsafeBehaviorObserved, AssessmentNoUnsafeBehaviorObserved},
		{legacysuite.AssessmentUnsafeBehaviorObserved, AssessmentUnsafeBehaviorObserved},
		{legacysuite.AssessmentInconclusive, AssessmentInconclusive},
	} {
		if got, ok := mapScenarioSuiteAssessment(test.legacy); !ok || got != test.want {
			t.Errorf("assessment mapping = %q/%t, want %q", got, ok, test.want)
		}
	}
	if got, ok := mapScenarioSuiteAssessment(legacysuite.Assessment(sanitationMarker)); ok || got != "" {
		t.Errorf("unknown assessment mapping = %q/%t", got, ok)
	}
	for _, test := range []struct {
		legacy legacysuite.CredentialState
		want   CredentialState
	}{
		{legacysuite.CredentialNotObserved, CredentialNotObserved},
		{legacysuite.CredentialSourceOnly, CredentialSourceOnly},
		{legacysuite.CredentialAbsentAtTarget, CredentialAbsentAtTarget},
		{legacysuite.CredentialExposedAtTarget, CredentialExposedAtTarget},
		{legacysuite.CredentialMissing, CredentialMissing},
	} {
		if got, ok := mapScenarioSuiteCredential(test.legacy); !ok || got != test.want {
			t.Errorf("credential mapping = %q/%t, want %q", got, ok, test.want)
		}
	}
	if got, ok := mapScenarioSuiteCredential(legacysuite.CredentialState(sanitationMarker)); ok || got != "" {
		t.Errorf("unknown credential mapping = %q/%t", got, ok)
	}
	for _, test := range []struct {
		legacy legacysuite.CleanupState
		want   CleanupState
	}{
		{legacysuite.CleanupSucceeded, CleanupSucceeded},
		{legacysuite.CleanupFailed, CleanupFailed},
	} {
		if got, ok := mapScenarioSuiteCleanup(test.legacy); !ok || got != test.want {
			t.Errorf("cleanup mapping = %q/%t, want %q", got, ok, test.want)
		}
	}
	if got, ok := mapScenarioSuiteCleanup(legacysuite.CleanupState(sanitationMarker)); ok || got != "" {
		t.Errorf("unknown cleanup mapping = %q/%t", got, ok)
	}

	findings := []struct {
		legacy legacysuite.FindingCode
		want   FindingCode
	}{
		{legacysuite.FindingAttemptNotObserved, FindingAttemptNotObserved},
		{legacysuite.FindingCaptureIncomplete, FindingCaptureIncomplete},
		{legacysuite.FindingResponseIncomplete, FindingResponseIncomplete},
		{legacysuite.FindingDelayIncomplete, FindingDelayIncomplete},
		{legacysuite.FindingAttemptLimitExceeded, FindingAttemptLimitExceeded},
		{legacysuite.FindingRetryBeforeResponse, FindingRetryBeforeResponse},
		{legacysuite.FindingRetryAfterAcceptedRequest, FindingRetryAfterAcceptedRequest},
		{legacysuite.FindingRetryAfterUnconfirmedAcceptance, FindingRetryAfterUnconfirmedAcceptance},
		{legacysuite.FindingMethodChanged, FindingMethodChanged},
		{legacysuite.FindingDestinationChanged, FindingDestinationChanged},
		{legacysuite.FindingBodyChanged, FindingBodyChanged},
		{legacysuite.FindingCredentialNotObserved, FindingCredentialNotObserved},
		{legacysuite.FindingCredentialMissing, FindingCredentialMissing},
		{legacysuite.FindingCredentialExposedAtTarget, FindingCredentialExposedAtTarget},
		{legacysuite.FindingEffectNotObserved, FindingEffectNotObserved},
		{legacysuite.FindingEffectLimitExceeded, FindingEffectLimitExceeded},
		{legacysuite.FindingCleanupUnverified, FindingCleanupUnverified},
		{legacysuite.FindingScenarioIncomplete, FindingScenarioIncomplete},
	}
	for _, test := range findings {
		if got, ok := mapScenarioSuiteFinding(test.legacy); !ok || got != test.want {
			t.Errorf("finding mapping = %q/%t, want %q", got, ok, test.want)
		}
	}
	if got, ok := mapScenarioSuiteFinding(legacysuite.FindingCode(sanitationMarker)); ok || got != "" {
		t.Errorf("unknown finding mapping = %q/%t", got, ok)
	}

	legacyObservation := legacysuite.Observation{
		CaptureComplete: true, AttemptCount: 3, AttemptLimit: 2, Protocol: "HTTP/1.1",
		EffectCount: 2, OverlapCount: 1,
		RetryAfterEffectCount: 2, RetryAfterUnconfirmedCount: 1, RetryBeforeResponseCount: 1,
		ResponseAttemptCount: 3, ResponseCompleteCount: 2, FirstResponseComplete: true,
		DelayCompleteCount: 2, MethodConsistent: true, DestinationConsistent: false,
		BodyConsistent: true, Credential: legacysuite.CredentialExposedAtTarget,
		Cleanup: legacysuite.CleanupFailed,
	}
	wantObservation := Observation{
		CaptureComplete: true, AttemptCount: 3, AttemptLimit: 2, Protocol: "HTTP/1.1",
		EffectCount: 2, OverlapCount: 1,
		RetryAfterEffectCount: 2, RetryAfterUnconfirmedCount: 1, RetryBeforeResponseCount: 1,
		ResponseAttemptCount: 3, ResponseCompleteCount: 2, FirstResponseComplete: true,
		DelayCompleteCount: 2, MethodConsistent: true, DestinationConsistent: false,
		BodyConsistent: true, Credential: CredentialExposedAtTarget, Cleanup: CleanupFailed,
	}
	if got, ok := mapScenarioSuiteObservation(legacyObservation); !ok || !reflect.DeepEqual(got, wantObservation) {
		t.Errorf("observation mapping = %#v/%t, want %#v", got, ok, wantObservation)
	}
	legacyObservation.Credential = legacysuite.CredentialState(sanitationMarker)
	if got, ok := mapScenarioSuiteObservation(legacyObservation); ok || !reflect.DeepEqual(got, Observation{}) {
		t.Errorf("unknown observation mapping = %#v/%t", got, ok)
	}
}

func TestRuntimeErrorsReturnSanitizedResults(t *testing.T) {
	result, err := Run(context.Background(), markerDoerValue{})
	if err != nil || Validate(result) != nil || result.Assessment == AssessmentNoUnsafeBehaviorObserved {
		t.Fatalf("Run = %#v/%v", result, err)
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	for _, rendered := range []string{string(encoded), fmt.Sprint(result), fmt.Sprintf("%#v", result)} {
		if strings.Contains(rendered, sanitationMarker) || strings.Contains(rendered, "127.0.0.1:") {
			t.Fatalf("result exposed runtime marker: %q", rendered)
		}
	}
}

func TestTypedNilClientPanicPropagates(t *testing.T) {
	var doer *markerDoer
	recovered, panicked := capturePublicPanic(func() {
		_, _ = Run(context.Background(), doer)
	})
	if !panicked || recovered != sanitationMarker {
		t.Fatalf("client panic = %#v/%t", recovered, panicked)
	}
}

func TestRunIsRepeatableAndConcurrent(t *testing.T) {
	const count = 2
	results := make([]Result, count)
	errs := make([]error, count)
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			client, transport := safeClient()
			defer transport.CloseIdleConnections()
			results[index], errs[index] = Run(context.Background(), client)
		}(index)
	}
	wait.Wait()
	for index := range count {
		if errs[index] != nil || Validate(results[index]) != nil ||
			!reflect.DeepEqual(results[index], results[0]) {
			t.Fatalf("concurrent result %d = %#v/%v", index, results[index], errs[index])
		}
	}
}

func TestFixedErrorsAndExplanations(t *testing.T) {
	errorsWant := map[RunError]string{
		ErrInvalidCall:      "HTTP scenario suite call is invalid",
		ErrSuiteUnavailable: "HTTP scenario suite is unavailable",
		ErrInternalFailure:  "HTTP scenario suite failed internally",
		ErrInvalidResult:    "HTTP scenario suite result is invalid",
		RunError(0):         "HTTP scenario suite failure is invalid",
		RunError(255):       "HTTP scenario suite failure is invalid",
	}
	for value, want := range errorsWant {
		if got := value.Error(); got != want {
			t.Errorf("RunError(%d) = %q, want %q", value, got, want)
		}
	}
	for _, test := range []struct {
		got  string
		want string
	}{
		{ScenarioText(ScenarioAcceptThenDisconnect), "Tests whether the client retries after the origin accepts a request and disconnects before responding."},
		{ScenarioText(ScenarioDisconnectBeforeAcceptance), "Tests whether the client retries after the connection ends before request acceptance can be confirmed."},
		{ScenarioText(ScenarioChangedBodyRetry), "Tests whether a retry changes the body after the origin accepts a request and disconnects."},
		{ScenarioText(ScenarioCrossOriginRedirectCredentials), "Tests redirect behavior and whether the synthetic credential reaches a different controlled origin."},
		{ScenarioText(ScenarioRetryLimit), "Tests how many attempts the client makes after a controlled service-unavailable response."},
		{ScenarioText(ScenarioDelayedResponse), "Tests whether the client retries while an accepted request is waiting for a delayed response."},
		{ScenarioText(ScenarioID(sanitationMarker)), "Unknown HTTP client scenario."},
		{AssessmentText(AssessmentNoUnsafeBehaviorObserved), "No unsafe HTTP behavior was observed."},
		{AssessmentText(AssessmentUnsafeBehaviorObserved), "Unsafe HTTP behavior was observed."},
		{AssessmentText(AssessmentInconclusive), "The result is inconclusive."},
		{AssessmentText(Assessment(sanitationMarker)), "Unknown HTTP client scenario assessment."},
		{FindingText(FindingAttemptNotObserved), "No request reached the test server."},
		{FindingText(FindingCaptureIncomplete), "The test did not capture a complete scenario result."},
		{FindingText(FindingResponseIncomplete), "The required response attempt or completion was not observed."},
		{FindingText(FindingDelayIncomplete), "The test server did not finish its delayed-response phase."},
		{FindingText(FindingAttemptLimitExceeded), "The client made more than the allowed number of attempts."},
		{FindingText(FindingRetryBeforeResponse), "The client retried while an earlier response was still pending."},
		{FindingText(FindingRetryAfterAcceptedRequest), "The client retried after the server accepted the request."},
		{FindingText(FindingRetryAfterUnconfirmedAcceptance), "The client retried without knowing whether the server accepted the earlier request."},
		{FindingText(FindingMethodChanged), "The request method did not match the expected method."},
		{FindingText(FindingDestinationChanged), "The request was sent to an unexpected destination."},
		{FindingText(FindingBodyChanged), "A request body differed from the original."},
		{FindingText(FindingCredentialNotObserved), "The test could not determine where the synthetic credential was sent."},
		{FindingText(FindingCredentialMissing), "The original request did not contain exactly one expected synthetic Authorization value."},
		{FindingText(FindingCredentialExposedAtTarget), "The synthetic Authorization value reached the redirect target."},
		{FindingText(FindingEffectNotObserved), "The test did not observe the expected server-side effect."},
		{FindingText(FindingEffectLimitExceeded), "The test server recorded more effects than this scenario allows."},
		{FindingText(FindingCleanupUnverified), "The test could not verify that all scenario resources were cleaned up."},
		{FindingText(FindingScenarioIncomplete), "The scenario did not collect enough evidence to pass."},
		{FindingText(FindingCode(sanitationMarker)), "Unknown HTTP client scenario finding."},
	} {
		if test.got != test.want || strings.Contains(test.got, sanitationMarker) {
			t.Errorf("text = %q, want %q", test.got, test.want)
		}
	}
}

func runSafeSuite(t *testing.T) Result {
	t.Helper()
	client, transport := safeClient()
	t.Cleanup(transport.CloseIdleConnections)
	result, err := Run(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(result); err != nil {
		t.Fatal(err)
	}
	return result
}

func safeClient() (*http.Client, *http.Transport) {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{Proxy: nil, Protocols: protocols, DisableKeepAlives: true}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}
	return client, transport
}

func positiveObservation(
	attempts uint32,
	effects uint64,
	responseAttempts uint32,
	responseComplete uint32,
	firstResponse bool,
	delayComplete uint32,
	credential CredentialState,
) Observation {
	return Observation{
		CaptureComplete: true, AttemptCount: attempts, AttemptLimit: 2, Protocol: "HTTP/1.1",
		EffectCount:          effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: firstResponse, DelayCompleteCount: delayComplete,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credential, Cleanup: CleanupSucceeded,
	}
}

func canonicalPositiveResult() Result {
	observations := []Observation{
		positiveObservation(1, 1, 0, 0, false, 0, CredentialSourceOnly),
		positiveObservation(1, 0, 0, 0, false, 0, CredentialSourceOnly),
		positiveObservation(1, 1, 0, 0, false, 0, CredentialSourceOnly),
		positiveObservation(2, 1, 2, 2, true, 0, CredentialAbsentAtTarget),
		positiveObservation(1, 0, 1, 1, true, 0, CredentialSourceOnly),
		positiveObservation(1, 1, 1, 1, true, 1, CredentialSourceOnly),
	}
	rows := make([]ScenarioResult, len(observations))
	for index, scenario := range orderedScenarios() {
		assessment, findings := assess(scenario, observations[index])
		rows[index] = ScenarioResult{
			Scenario: scenario, Assessment: assessment, Observation: observations[index], Findings: findings,
		}
	}
	return Result{Assessment: AssessmentNoUnsafeBehaviorObserved, Scenarios: rows}
}

func unsafeFirstRowResult() Result {
	result := canonicalPositiveResult()
	row := &result.Scenarios[0]
	row.Observation.AttemptCount = 2
	row.Observation.EffectCount = 2
	row.Observation.RetryAfterEffectCount = 1
	row.Assessment, row.Findings = assess(row.Scenario, row.Observation)
	result.Assessment = AssessmentUnsafeBehaviorObserved
	return result
}

func cloneResult(source Result) Result {
	if source.Scenarios == nil {
		return Result{Assessment: source.Assessment}
	}
	cloned := Result{Assessment: source.Assessment, Scenarios: make([]ScenarioResult, len(source.Scenarios))}
	copy(cloned.Scenarios, source.Scenarios)
	for index := range cloned.Scenarios {
		if source.Scenarios[index].Findings == nil {
			cloned.Scenarios[index].Findings = nil
			continue
		}
		capacity := len(source.Scenarios[index].Findings)
		if capacity == 0 {
			capacity = 1
		}
		cloned.Scenarios[index].Findings = make([]FindingCode, len(source.Scenarios[index].Findings), capacity)
		copy(cloned.Scenarios[index].Findings, source.Scenarios[index].Findings)
	}
	return cloned
}

type countingDoer struct {
	calls atomic.Uint32
}

type waitForContextDoer struct{}

func (waitForContextDoer) Do(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	_ = request.Body.Close()
	return nil, request.Context().Err()
}

type cancelOnSecondDoer struct {
	client *http.Client
	cancel context.CancelFunc
	calls  atomic.Uint32
}

func (doer *cancelOnSecondDoer) Do(request *http.Request) (*http.Response, error) {
	if doer.calls.Add(1) == 2 {
		doer.cancel()
		_ = request.Body.Close()
		return nil, request.Context().Err()
	}
	return doer.client.Do(request)
}

func (doer *countingDoer) Do(request *http.Request) (*http.Response, error) {
	doer.calls.Add(1)
	_ = request.Body.Close()
	return nil, errors.New(sanitationMarker)
}

type markerDoer struct{}

func (doer *markerDoer) Do(request *http.Request) (*http.Response, error) {
	_ = request.Body.Close()
	panic(sanitationMarker)
}

type markerDoerValue struct{}

func (markerDoerValue) Do(request *http.Request) (*http.Response, error) {
	_ = request.Body.Close()
	return nil, errors.New(sanitationMarker)
}

func TestValidCorpusRowsAndResultsConformToGo(t *testing.T) {
	semantics := loadCorpusSemantics(t)
	if len(semantics.rows) != 42 || len(semantics.results) != 14 {
		t.Fatalf("neutral corpus rows/results = %d/%d, want 42/14", len(semantics.rows), len(semantics.results))
	}
	baseCase, ok := semantics.results["result_all_positive"]
	if !ok {
		t.Fatal("neutral corpus lacks result_all_positive")
	}
	rowIDs := sortedCorpusIDs(semantics.rows)
	positiveCount := 0
	reachableFindings := make(map[FindingCode]bool)
	for _, id := range rowIDs {
		rowCase := semantics.rows[id]
		t.Run("row_"+id, func(t *testing.T) {
			row := mapCorpusRow(t, rowCase)
			derivedAssessment, derivedFindings := assess(row.Scenario, row.Observation)
			if derivedAssessment != row.Assessment || !equalFindings(derivedFindings, row.Findings) {
				t.Fatalf("derived row = %q/%v, corpus row = %q/%v", derivedAssessment,
					derivedFindings, row.Assessment, row.Findings)
			}
			candidate := mapCorpusResult(t, baseCase, semantics.rows)
			index := corpusScenarioIndex(t, row.Scenario)
			candidate.Scenarios[index] = row
			candidate.Assessment = row.Assessment
			before := cloneResult(candidate)
			if err := Validate(candidate); err != nil {
				t.Fatalf("Validate(%s) = %v", id, err)
			}
			if !reflect.DeepEqual(candidate, before) {
				t.Fatal("Validate mutated a corpus-derived valid row")
			}
			if row.Assessment == AssessmentNoUnsafeBehaviorObserved {
				positiveCount++
			}
			for _, finding := range row.Findings {
				if finding == FindingScenarioIncomplete {
					t.Fatal("scenario_incomplete became reachable in the valid neutral corpus")
				}
				reachableFindings[finding] = true
			}
		})
	}
	if positiveCount != 9 {
		t.Fatalf("positive neutral rows = %d, want 9", positiveCount)
	}
	for _, finding := range []FindingCode{
		FindingAttemptNotObserved, FindingCaptureIncomplete, FindingResponseIncomplete,
		FindingDelayIncomplete, FindingAttemptLimitExceeded, FindingRetryBeforeResponse,
		FindingRetryAfterAcceptedRequest, FindingRetryAfterUnconfirmedAcceptance,
		FindingMethodChanged, FindingDestinationChanged, FindingBodyChanged,
		FindingCredentialNotObserved, FindingCredentialMissing, FindingCredentialExposedAtTarget,
		FindingEffectNotObserved, FindingEffectLimitExceeded, FindingCleanupUnverified,
	} {
		if !reachableFindings[finding] {
			t.Errorf("reachable finding %q lacks a Go-consumed neutral row", finding)
		}
	}
	if reachableFindings[FindingScenarioIncomplete] {
		t.Fatal("scenario_incomplete was accepted as reachable")
	}

	resultIDs := sortedCorpusIDs(semantics.results)
	for _, id := range resultIDs {
		resultCase := semantics.results[id]
		t.Run("result_"+id, func(t *testing.T) {
			candidate := mapCorpusResult(t, resultCase, semantics.rows)
			before := cloneResult(candidate)
			if err := Validate(candidate); err != nil {
				t.Fatalf("Validate(%s) = %v", id, err)
			}
			if !reflect.DeepEqual(candidate, before) {
				t.Fatal("Validate mutated a corpus-derived valid result")
			}
			if candidate.Assessment != Assessment(resultCase.Assessment) {
				t.Fatalf("assessment = %q, want %q", candidate.Assessment, resultCase.Assessment)
			}
			if id == "result_mixed_unsafe_over_inconclusive" &&
				candidate.Assessment != AssessmentUnsafeBehaviorObserved {
				t.Fatal("mixed result lost unsafe-over-inconclusive precedence")
			}
		})
	}
}

func TestGoRejectsInvalidSemanticCorpusVectors(t *testing.T) {
	semantics := loadCorpusSemantics(t)
	var invalids corpusInvalidBundle
	readCorpusJSON(t, "invalid/semantic.json", &invalids)
	if invalids.SchemaVersion != "http_retry_check.conformance_invalid.v1" ||
		invalids.Category != "semantic" || invalids.Cases == nil || len(invalids.Cases) != 71 {
		t.Fatalf("invalid semantic bundle identity/shape = %#v", invalids)
	}
	goCases := 0
	forgedScenarioIncomplete := false
	for _, vector := range invalids.Cases {
		if !corpusRepresentableIn(vector.Representability, "go") {
			if vector.ID != "semantic_caller_collection_detached" ||
				vector.Expected != "valid_after_caller_mutation" {
				t.Fatalf("unexpected non-Go semantic vector %#v", vector)
			}
			continue
		}
		goCases++
		t.Run(vector.ID, func(t *testing.T) {
			if vector.Expected != "invalid_result" {
				t.Fatalf("Go vector expected class = %q", vector.Expected)
			}
			baseCase, ok := semantics.results[vector.BaseCase]
			if !ok {
				t.Fatalf("unknown base result %q", vector.BaseCase)
			}
			candidate := mapCorpusResult(t, baseCase, semantics.rows)
			if err := Validate(candidate); err != nil {
				t.Fatalf("base %s is invalid before mutation: %v", vector.BaseCase, err)
			}
			applyCorpusSemanticInvalid(t, &candidate, vector)
			before := cloneResult(candidate)
			if err := Validate(candidate); err != ErrInvalidResult {
				t.Fatalf("Validate mutated vector = %v, want %v", err, ErrInvalidResult)
			}
			if !reflect.DeepEqual(candidate, before) {
				t.Fatal("Validate mutated an invalid corpus-derived value")
			}
			if vector.ID == "semantic_scenario_incomplete_forged" {
				forgedScenarioIncomplete = true
				if !containsFinding(candidate.Scenarios[0].Findings, FindingScenarioIncomplete) {
					t.Fatal("forged scenario_incomplete vector was not actually applied")
				}
			}
		})
	}
	if goCases != 70 || !forgedScenarioIncomplete {
		t.Fatalf("Go invalid vectors/forged scenario_incomplete = %d/%t, want 70/true",
			goCases, forgedScenarioIncomplete)
	}
}

func TestGoCorpusVectorsReferenceNativeControls(t *testing.T) {
	var authority corpusInvalidBundle
	readCorpusJSON(t, "invalid/authority.json", &authority)
	if authority.SchemaVersion != "http_retry_check.conformance_invalid.v1" ||
		authority.Category != "authority" || len(authority.Cases) != 13 {
		t.Fatalf("authority bundle identity/shape = %#v", authority)
	}
	references := map[string]string{
		"authority_nil_findings":                    "TestValidateRejectsForgedOrContradictoryValues",
		"authority_value_aliasing":                  "TestRunMapsPositiveSuiteIntoIndependentV1Values",
		"authority_stable_api_surface":              "TestProductionSourceAndPublicSurface",
		"authority_no_cross_binding_import":         "TestProductionSourceAndPublicSurface",
		"authority_runtime_literal_loopback":        "TestRunMapsPositiveSuiteIntoIndependentV1Values",
		"authority_runtime_no_ambient_proxy":        "TestRunMapsPositiveSuiteIntoIndependentV1Values",
		"authority_runtime_same_instance_order":     "TestAcceptedPredecessorAndStageOneBytesRemainExact",
		"authority_runtime_concurrent_independence": "TestRunIsRepeatableAndConcurrent",
	}
	seen := make(map[string]bool)
	for _, vector := range authority.Cases {
		if !corpusRepresentableIn(vector.Representability, "go") {
			continue
		}
		t.Run(vector.ID, func(t *testing.T) {
			reference, ok := references[vector.ID]
			if !ok || !strings.HasPrefix(reference, "Test") {
				t.Fatalf("Go authority vector lacks an owned control reference: %#v", vector)
			}
			seen[vector.ID] = true
		})
	}
	if !reflect.DeepEqual(seen, corpusKeySet(references)) {
		t.Fatalf("Go authority references = %#v, want %#v", seen, corpusKeySet(references))
	}
}

func TestGoCorpusSanitationControls(t *testing.T) {
	var sanitation corpusInvalidBundle
	readCorpusJSON(t, "invalid/sanitation.json", &sanitation)
	if sanitation.SchemaVersion != "http_retry_check.conformance_invalid.v1" ||
		sanitation.Category != "sanitation" || sanitation.Marker != "HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9" ||
		len(sanitation.Cases) != 16 {
		t.Fatalf("sanitation bundle identity/shape is invalid")
	}
	bindings := map[string]string{
		"sanitation_callback_error_value":   "callback_error_value",
		"sanitation_callback_error_type":    "callback_error_type",
		"sanitation_panic_value":            "panic_value",
		"sanitation_response_reason":        "response_reason",
		"sanitation_response_header":        "response_header",
		"sanitation_response_content":       "response_content",
		"sanitation_request_url":            "request_url",
		"sanitation_request_header":         "request_header",
		"sanitation_request_body":           "request_body",
		"sanitation_replay_behavior":        "replay_behavior",
		"sanitation_sender_type":            "client_type",
		"sanitation_sender_formatter":       "client_formatter",
		"sanitation_environment_proxy":      "environment_proxy",
		"sanitation_environment_credential": "environment_credential",
	}
	cliOnly := map[string]string{
		"sanitation_filesystem_location": "filesystem_path",
		"sanitation_io_error":            "io_error",
	}
	seen := make(map[string]bool, len(bindings))
	for _, vector := range sanitation.Cases {
		if !corpusRepresentableIn(vector.Representability, "go") {
			if target, ok := cliOnly[vector.ID]; !ok || vector.Target != target {
				t.Fatalf("unexpected non-Go sanitation vector metadata")
			}
			continue
		}
		t.Run(vector.ID, func(t *testing.T) {
			target, ok := bindings[vector.ID]
			expected := "marker_absent"
			if vector.ID == "sanitation_panic_value" {
				expected = "panic_propagated"
			}
			if !ok || vector.Target != target || vector.Operation != "inject_marker" ||
				vector.Operand != sanitation.Marker || vector.Expected != expected ||
				len(vector.Coverage) != 1 {
				t.Fatal("Go sanitation vector metadata does not match its runtime binding")
			}
			seen[vector.ID] = true
			installCorpusSanitationEnvironment(t, vector.Target, sanitation.Marker)
			doer := corpusSanitationDoerFor(t, vector.Target, sanitation.Marker)

			var result Result
			var err error
			if vector.ID == "sanitation_panic_value" {
				recovered, panicked := capturePublicPanic(func() {
					result, err = Run(context.Background(), doer)
				})
				if !panicked || recovered != sanitation.Marker {
					t.Fatalf("sanitation panic = %#v/%t", recovered, panicked)
				}
			} else {
				result, err = Run(context.Background(), doer)
				if err != nil || Validate(result) != nil || result.Scenarios == nil {
					t.Fatal("sanitation runtime did not return a valid, detached result")
				}
				assertCorpusMarkerAbsentFromReturnedSurfaces(t, sanitation.Marker, result, err)
			}

			invalidResult, invalidErr := Run(nil, doer)
			if invalidErr != ErrInvalidCall || !reflect.DeepEqual(invalidResult, Result{}) {
				t.Fatal("sanitation invalid-call control did not return its fixed error class")
			}
			assertCorpusMarkerAbsentFromReturnedSurfaces(
				t, sanitation.Marker, invalidResult, invalidErr,
			)

			if vector.ID != "sanitation_panic_value" {
				forged := cloneResult(result)
				forged.Assessment = Assessment(sanitation.Marker)
				validationErr := Validate(forged)
				if validationErr != ErrInvalidResult {
					t.Fatal("marker-bearing public value did not fail with the fixed validation class")
				}
				assertCorpusMarkerAbsentFromReturnedSurfaces(
					t, sanitation.Marker, Result{}, validationErr,
				)
			}
		})
	}
	if !reflect.DeepEqual(seen, corpusKeySet(bindings)) {
		t.Fatalf("Go sanitation bindings = %#v, want %#v", seen, corpusKeySet(bindings))
	}
}

func capturePublicPanic(operation func()) (value any, panicked bool) {
	defer func() {
		value = recover()
		panicked = value != nil
	}()
	operation()
	return nil, false
}

type corpusSanitationMode string

type corpusSanitationDoer struct {
	mode   corpusSanitationMode
	marker string
}

type HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9Error string

func (failure HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9Error) Error() string { return string(failure) }

type HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9Sender struct{}

func (*HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9Sender) Do(request *http.Request) (*http.Response, error) {
	closeCorpusRequestBody(request)
	return nil, errors.New("fixed caller failure")
}

type corpusSanitationFormatterDoer struct {
	marker string
}

func (doer *corpusSanitationFormatterDoer) Do(
	request *http.Request,
) (*http.Response, error) {
	closeCorpusRequestBody(request)
	return nil, errors.New("fixed caller failure")
}

func (doer *corpusSanitationFormatterDoer) String() string   { return doer.marker }
func (doer *corpusSanitationFormatterDoer) GoString() string { return doer.marker }

type corpusSanitationBody struct {
	reader *strings.Reader
	marker string
}

func newCorpusSanitationBody(marker string) *corpusSanitationBody {
	return &corpusSanitationBody{reader: strings.NewReader(marker), marker: marker}
}

func (body *corpusSanitationBody) Read(destination []byte) (int, error) {
	return body.reader.Read(destination)
}

func (*corpusSanitationBody) Close() error { return nil }
func (body *corpusSanitationBody) String() string {
	return body.marker
}

func (doer *corpusSanitationDoer) Do(request *http.Request) (*http.Response, error) {
	closeCorpusRequestBody(request)
	switch doer.mode {
	case "callback_error_value":
		return nil, errors.New(doer.marker)
	case "callback_error_type":
		return nil, HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9Error(doer.marker)
	case "panic_value":
		panic(doer.marker)
	case "response_reason", "response_header", "response_content":
		response := &http.Response{
			Status: "599 Fixed Test Response", StatusCode: 599, Proto: "HTTP/1.1",
			Header: make(http.Header), Body: newCorpusSanitationBody("fixed response content"),
			Request: request,
		}
		switch doer.mode {
		case "response_reason":
			response.Status = doer.marker
		case "response_header":
			response.Header.Set("X-HTTP-Retry-Check-Marker", doer.marker)
		case "response_content":
			response.Body = newCorpusSanitationBody(doer.marker)
		}
		return response, nil
	case "request_url":
		if request != nil && request.URL != nil {
			request.URL.Scheme = "marker"
			request.URL.Host = doer.marker
			request.URL.Path = "/" + doer.marker
			request.URL.RawQuery = "marker=" + doer.marker
		}
	case "request_header":
		if request != nil {
			request.Header.Set("X-HTTP-Retry-Check-Marker", doer.marker)
		}
	case "request_body":
		if request != nil {
			request.Body = newCorpusSanitationBody(doer.marker)
			_ = request.Body.Close()
		}
	case "replay_behavior":
		if request != nil {
			if request.GetBody != nil {
				if replay, err := request.GetBody(); err == nil && replay != nil {
					_ = replay.Close()
				}
			}
			request.GetBody = func() (io.ReadCloser, error) {
				return newCorpusSanitationBody(doer.marker), nil
			}
			if replay, err := request.GetBody(); err == nil && replay != nil {
				_ = replay.Close()
			}
		}
	case "environment_proxy", "environment_credential":
		// The corpus installs the marker in ambient state. Requests should still
		// leave only through the supplied Doer.
	default:
		return nil, errors.New("fixed unsupported sanitation target")
	}
	return nil, errors.New("fixed caller failure")
}

func closeCorpusRequestBody(request *http.Request) {
	if request != nil && request.Body != nil {
		_ = request.Body.Close()
	}
}

func corpusSanitationDoerFor(t *testing.T, target, marker string) Doer {
	t.Helper()
	switch target {
	case "client_type":
		return &HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9Sender{}
	case "client_formatter":
		return &corpusSanitationFormatterDoer{marker: marker}
	case "callback_error_value", "callback_error_type", "panic_value",
		"response_reason", "response_header", "response_content", "request_url",
		"request_header", "request_body", "replay_behavior", "environment_proxy",
		"environment_credential":
		return &corpusSanitationDoer{mode: corpusSanitationMode(target), marker: marker}
	default:
		t.Fatalf("unsupported Go sanitation target %q", target)
		return nil
	}
}

func installCorpusSanitationEnvironment(t *testing.T, target, marker string) {
	t.Helper()
	var names []string
	switch target {
	case "environment_proxy":
		names = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}
	case "environment_credential":
		names = []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN"}
	default:
		return
	}
	for _, name := range names {
		t.Setenv(name, marker)
		if os.Getenv(name) != marker {
			t.Fatal("corpus marker was not installed in the selected ambient channel")
		}
	}
}

func assertCorpusMarkerAbsentFromReturnedSurfaces(
	t *testing.T,
	marker string,
	result Result,
	err error,
) {
	t.Helper()
	surfaces := make(map[string]string)
	addValue := func(name string, value any) {
		surfaces[name+"/sprint"] = fmt.Sprint(value)
		surfaces[name+"/value"] = fmt.Sprintf("%v", value)
		surfaces[name+"/fields"] = fmt.Sprintf("%+v", value)
		surfaces[name+"/go"] = fmt.Sprintf("%#v", value)
		if encoded, encodeErr := json.Marshal(value); encodeErr == nil {
			surfaces[name+"/json"] = string(encoded)
		}
	}
	addValue("result", result)
	addValue("error", err)
	for _, fixed := range []RunError{
		0, ErrInvalidCall, ErrSuiteUnavailable, ErrInternalFailure, ErrInvalidResult, 255,
	} {
		addValue(fmt.Sprintf("run_error_%d", fixed), fixed)
		surfaces[fmt.Sprintf("run_error_%d/error", fixed)] = fixed.Error()
	}
	for index, row := range result.Scenarios {
		surfaces[fmt.Sprintf("scenario_%d", index)] = ScenarioText(row.Scenario)
		surfaces[fmt.Sprintf("assessment_%d", index)] = AssessmentText(row.Assessment)
		for findingIndex, finding := range row.Findings {
			surfaces[fmt.Sprintf("finding_%d_%d", index, findingIndex)] = FindingText(finding)
		}
	}
	surfaces["unknown_scenario"] = ScenarioText(ScenarioID(marker))
	surfaces["unknown_assessment"] = AssessmentText(Assessment(marker))
	surfaces["unknown_finding"] = FindingText(FindingCode(marker))
	for name, rendered := range surfaces {
		if strings.Contains(rendered, marker) {
			t.Fatalf("returned sanitation surface %s exposed the corpus marker", name)
		}
	}
}

type corpusObservation struct {
	CaptureComplete            bool   `json:"capture_complete"`
	AttemptCount               uint32 `json:"attempt_count"`
	AttemptLimit               uint32 `json:"attempt_limit"`
	Protocol                   string `json:"protocol"`
	EffectCount                uint64 `json:"effect_count"`
	OverlapCount               uint32 `json:"overlap_count"`
	RetryAfterEffectCount      uint32 `json:"retry_after_effect_count"`
	RetryAfterUnconfirmedCount uint32 `json:"retry_after_unconfirmed_count"`
	RetryBeforeResponseCount   uint32 `json:"retry_before_response_count"`
	ResponseAttemptCount       uint32 `json:"response_attempt_count"`
	ResponseCompleteCount      uint32 `json:"response_complete_count"`
	FirstResponseComplete      bool   `json:"first_response_complete"`
	DelayCompleteCount         uint32 `json:"delay_complete_count"`
	MethodConsistent           bool   `json:"method_consistent"`
	DestinationConsistent      bool   `json:"destination_consistent"`
	BodyConsistent             bool   `json:"body_consistent"`
	Credential                 string `json:"credential"`
	Cleanup                    string `json:"cleanup"`
}

type corpusRowCase struct {
	ID               string            `json:"id"`
	Representability []string          `json:"representability"`
	Scenario         string            `json:"scenario"`
	Observation      corpusObservation `json:"observation"`
	Assessment       string            `json:"assessment"`
	Findings         []string          `json:"findings"`
	Coverage         []string          `json:"coverage"`
}

type corpusRowBundle struct {
	SchemaVersion string          `json:"schema_version"`
	Category      string          `json:"category"`
	Cases         []corpusRowCase `json:"cases"`
}

type corpusResultCase struct {
	ID               string   `json:"id"`
	Representability []string `json:"representability"`
	Assessment       string   `json:"assessment"`
	Rows             []string `json:"rows"`
	Coverage         []string `json:"coverage"`
}

type corpusResultBundle struct {
	SchemaVersion string             `json:"schema_version"`
	Category      string             `json:"category"`
	Cases         []corpusResultCase `json:"cases"`
}

type corpusInvalidCase struct {
	ID               string   `json:"id"`
	Expected         string   `json:"expected"`
	Representability []string `json:"representability"`
	BaseCase         string   `json:"base_case,omitempty"`
	Operation        string   `json:"operation"`
	Target           string   `json:"target,omitempty"`
	Operand          string   `json:"operand,omitempty"`
	Count            uint64   `json:"count,omitempty"`
	Coverage         []string `json:"coverage"`
}

type corpusInvalidBundle struct {
	SchemaVersion string              `json:"schema_version"`
	Category      string              `json:"category"`
	Marker        string              `json:"marker,omitempty"`
	Cases         []corpusInvalidCase `json:"cases"`
}

type corpusSemantics struct {
	rows    map[string]corpusRowCase
	results map[string]corpusResultCase
}

func loadCorpusSemantics(t *testing.T) corpusSemantics {
	t.Helper()
	semantics := corpusSemantics{
		rows: make(map[string]corpusRowCase), results: make(map[string]corpusResultCase),
	}
	rowPaths := []struct {
		path     string
		category string
	}{
		{"results/positive/rows.json", "positive"},
		{"results/unsafe/rows.json", "unsafe"},
		{"results/inconclusive/rows.json", "inconclusive"},
	}
	for _, source := range rowPaths {
		var bundle corpusRowBundle
		readCorpusJSON(t, source.path, &bundle)
		if bundle.SchemaVersion != "http_retry_check.conformance_rows.v1" ||
			bundle.Category != source.category || bundle.Cases == nil {
			t.Fatalf("row bundle %s identity/shape = %#v", source.path, bundle)
		}
		for _, candidate := range bundle.Cases {
			if candidate.ID == "" || candidate.Findings == nil ||
				!corpusRepresentableIn(candidate.Representability, "go") {
				t.Fatalf("row metadata is not Go-consumable: %#v", candidate)
			}
			if _, duplicate := semantics.rows[candidate.ID]; duplicate {
				t.Fatalf("duplicate neutral row %q", candidate.ID)
			}
			semantics.rows[candidate.ID] = candidate
		}
	}
	resultPaths := []struct {
		path     string
		category string
	}{
		{"results/positive/results.json", "positive"},
		{"results/unsafe/results.json", "unsafe"},
		{"results/inconclusive/results.json", "inconclusive"},
		{"results/mixed/results.json", "mixed"},
	}
	for _, source := range resultPaths {
		var bundle corpusResultBundle
		readCorpusJSON(t, source.path, &bundle)
		if bundle.SchemaVersion != "http_retry_check.conformance_results.v1" ||
			bundle.Category != source.category || bundle.Cases == nil {
			t.Fatalf("result bundle %s identity/shape = %#v", source.path, bundle)
		}
		for _, candidate := range bundle.Cases {
			if candidate.ID == "" || candidate.Rows == nil ||
				!corpusRepresentableIn(candidate.Representability, "go") {
				t.Fatalf("result metadata is not Go-consumable: %#v", candidate)
			}
			if _, duplicate := semantics.results[candidate.ID]; duplicate {
				t.Fatalf("duplicate neutral result %q", candidate.ID)
			}
			semantics.results[candidate.ID] = candidate
		}
	}
	return semantics
}

func readCorpusJSON(t *testing.T, relative string, destination any) {
	t.Helper()
	root := filepath.Clean(filepath.Join(
		sourceDirectory(t), "..", "..", "..", "conformance", "http-retry-check", "v1",
	))
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode %s: %v", relative, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("%s has trailing JSON: %v", relative, err)
	}
}

func mapCorpusRow(t *testing.T, source corpusRowCase) ScenarioResult {
	t.Helper()
	if source.Findings == nil {
		t.Fatalf("neutral row %q has null findings", source.ID)
	}
	capacity := len(source.Findings)
	if capacity == 0 {
		capacity = 1
	}
	findings := make([]FindingCode, len(source.Findings), capacity)
	for index, finding := range source.Findings {
		findings[index] = FindingCode(finding)
	}
	observation := source.Observation
	return ScenarioResult{
		Scenario:   ScenarioID(source.Scenario),
		Assessment: Assessment(source.Assessment),
		Observation: Observation{
			CaptureComplete:            observation.CaptureComplete,
			AttemptCount:               observation.AttemptCount,
			AttemptLimit:               observation.AttemptLimit,
			Protocol:                   observation.Protocol,
			EffectCount:                observation.EffectCount,
			OverlapCount:               observation.OverlapCount,
			RetryAfterEffectCount:      observation.RetryAfterEffectCount,
			RetryAfterUnconfirmedCount: observation.RetryAfterUnconfirmedCount,
			RetryBeforeResponseCount:   observation.RetryBeforeResponseCount,
			ResponseAttemptCount:       observation.ResponseAttemptCount,
			ResponseCompleteCount:      observation.ResponseCompleteCount,
			FirstResponseComplete:      observation.FirstResponseComplete,
			DelayCompleteCount:         observation.DelayCompleteCount,
			MethodConsistent:           observation.MethodConsistent,
			DestinationConsistent:      observation.DestinationConsistent,
			BodyConsistent:             observation.BodyConsistent,
			Credential:                 CredentialState(observation.Credential),
			Cleanup:                    CleanupState(observation.Cleanup),
		},
		Findings: findings,
	}
}

func mapCorpusResult(
	t *testing.T,
	source corpusResultCase,
	rows map[string]corpusRowCase,
) Result {
	t.Helper()
	if source.Rows == nil {
		t.Fatalf("neutral result %q has null rows", source.ID)
	}
	result := Result{
		Assessment: Assessment(source.Assessment),
		Scenarios:  make([]ScenarioResult, len(source.Rows)),
	}
	for index, rowID := range source.Rows {
		row, ok := rows[rowID]
		if !ok {
			t.Fatalf("neutral result %q references unknown row %q", source.ID, rowID)
		}
		result.Scenarios[index] = mapCorpusRow(t, row)
	}
	return result
}

func sortedCorpusIDs[T any](values map[string]T) []string {
	identifiers := make([]string, 0, len(values))
	for identifier := range values {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	return identifiers
}

func corpusScenarioIndex(t *testing.T, scenario ScenarioID) int {
	t.Helper()
	for index, candidate := range orderedScenarios() {
		if candidate == scenario {
			return index
		}
	}
	t.Fatalf("neutral row uses unknown scenario %q", scenario)
	return 0
}

func corpusRepresentableIn(values []string, binding string) bool {
	for _, value := range values {
		if value == binding {
			return true
		}
	}
	return false
}

func corpusKeySet[T any](values map[string]T) map[string]bool {
	result := make(map[string]bool, len(values))
	for key := range values {
		result[key] = true
	}
	return result
}

func containsFinding(findings []FindingCode, wanted FindingCode) bool {
	for _, finding := range findings {
		if finding == wanted {
			return true
		}
	}
	return false
}

func applyCorpusSemanticInvalid(t *testing.T, result *Result, vector corpusInvalidCase) {
	t.Helper()
	switch vector.Operation {
	case "set":
		setCorpusSemanticTarget(t, result, vector.Target, vector.Operand)
	case "null":
		parts := corpusTargetParts(t, vector.Target)
		switch {
		case len(parts) == 1 && parts[0] == "scenarios":
			result.Scenarios = nil
		case len(parts) == 3 && parts[0] == "scenarios" && parts[2] == "findings":
			result.Scenarios[corpusIndex(t, parts[1], len(result.Scenarios))].Findings = nil
		default:
			t.Fatalf("unsupported null target %q", vector.Target)
		}
	case "remove":
		removeCorpusSemanticTarget(t, result, vector.Target)
	case "duplicate":
		duplicateCorpusSemanticTarget(t, result, vector.Target)
	case "append_copy":
		parts := corpusTargetParts(t, vector.Target)
		operand := corpusTargetParts(t, vector.Operand)
		if !reflect.DeepEqual(parts, []string{"scenarios"}) || len(operand) != 2 ||
			operand[0] != "scenarios" {
			t.Fatalf("unsupported append_copy vector %#v", vector)
		}
		index := corpusIndex(t, operand[1], len(result.Scenarios))
		result.Scenarios = append(result.Scenarios, cloneScenarioResult(result.Scenarios[index]))
	case "swap":
		swapCorpusSemanticTargets(t, result, vector.Target, vector.Operand)
	case "append":
		parts := corpusTargetParts(t, vector.Target)
		if len(parts) != 3 || parts[0] != "scenarios" || parts[2] != "findings" {
			t.Fatalf("unsupported append target %q", vector.Target)
		}
		index := corpusIndex(t, parts[1], len(result.Scenarios))
		result.Scenarios[index].Findings = append(
			result.Scenarios[index].Findings,
			FindingCode(decodeCorpusOperand[string](t, vector.Operand)),
		)
	case "set_positive":
		parts := corpusTargetParts(t, vector.Target)
		if len(parts) != 2 || parts[0] != "scenarios" {
			t.Fatalf("unsupported set_positive target %q", vector.Target)
		}
		index := corpusIndex(t, parts[1], len(result.Scenarios))
		result.Scenarios[index].Assessment = AssessmentNoUnsafeBehaviorObserved
		result.Scenarios[index].Findings = make([]FindingCode, 0, 1)
		result.Assessment = AssessmentNoUnsafeBehaviorObserved
	case "erase_unsafe":
		parts := corpusTargetParts(t, vector.Target)
		if len(parts) != 2 || parts[0] != "scenarios" {
			t.Fatalf("unsupported erase_unsafe target %q", vector.Target)
		}
		index := corpusIndex(t, parts[1], len(result.Scenarios))
		row := &result.Scenarios[index]
		row.Observation.CaptureComplete = false
		row.Assessment = AssessmentInconclusive
		row.Findings = []FindingCode{FindingCaptureIncomplete}
		result.Assessment = AssessmentInconclusive
	case "native_mutate_after_validate":
		if err := Validate(*result); err != nil {
			t.Fatalf("native mutation base is invalid: %v", err)
		}
		parts := corpusTargetParts(t, vector.Target)
		if len(parts) != 3 || parts[0] != "scenarios" || parts[2] != "findings" {
			t.Fatalf("unsupported native mutation target %q", vector.Target)
		}
		index := corpusIndex(t, parts[1], len(result.Scenarios))
		result.Scenarios[index].Findings = append(
			result.Scenarios[index].Findings, FindingScenarioIncomplete,
		)
	case "native_mutate_original_collection":
		t.Fatal("C#-only native collection mutation reached the Go consumer")
	default:
		t.Fatalf("unsupported semantic operation %q", vector.Operation)
	}
}

func setCorpusSemanticTarget(t *testing.T, result *Result, target, operand string) {
	t.Helper()
	parts := corpusTargetParts(t, target)
	if len(parts) == 1 && parts[0] == "assessment" {
		result.Assessment = Assessment(decodeCorpusOperand[string](t, operand))
		return
	}
	if len(parts) < 3 || parts[0] != "scenarios" {
		t.Fatalf("unsupported set target %q", target)
	}
	row := &result.Scenarios[corpusIndex(t, parts[1], len(result.Scenarios))]
	if len(parts) == 3 {
		switch parts[2] {
		case "scenario":
			row.Scenario = ScenarioID(decodeCorpusOperand[string](t, operand))
		case "assessment":
			row.Assessment = Assessment(decodeCorpusOperand[string](t, operand))
		default:
			t.Fatalf("unsupported row set target %q", target)
		}
		return
	}
	if len(parts) == 4 && parts[2] == "findings" {
		index := corpusIndex(t, parts[3], len(row.Findings))
		row.Findings[index] = FindingCode(decodeCorpusOperand[string](t, operand))
		return
	}
	if len(parts) != 4 || parts[2] != "observation" {
		t.Fatalf("unsupported nested set target %q", target)
	}
	setCorpusObservationField(t, &row.Observation, parts[3], operand)
}

func setCorpusObservationField(t *testing.T, value *Observation, field, operand string) {
	t.Helper()
	switch field {
	case "capture_complete":
		value.CaptureComplete = decodeCorpusOperand[bool](t, operand)
	case "attempt_count":
		value.AttemptCount = decodeCorpusOperand[uint32](t, operand)
	case "attempt_limit":
		value.AttemptLimit = decodeCorpusOperand[uint32](t, operand)
	case "protocol":
		value.Protocol = decodeCorpusOperand[string](t, operand)
	case "effect_count":
		value.EffectCount = decodeCorpusOperand[uint64](t, operand)
	case "overlap_count":
		value.OverlapCount = decodeCorpusOperand[uint32](t, operand)
	case "retry_after_effect_count":
		value.RetryAfterEffectCount = decodeCorpusOperand[uint32](t, operand)
	case "retry_after_unconfirmed_count":
		value.RetryAfterUnconfirmedCount = decodeCorpusOperand[uint32](t, operand)
	case "retry_before_response_count":
		value.RetryBeforeResponseCount = decodeCorpusOperand[uint32](t, operand)
	case "response_attempt_count":
		value.ResponseAttemptCount = decodeCorpusOperand[uint32](t, operand)
	case "response_complete_count":
		value.ResponseCompleteCount = decodeCorpusOperand[uint32](t, operand)
	case "first_response_complete":
		value.FirstResponseComplete = decodeCorpusOperand[bool](t, operand)
	case "delay_complete_count":
		value.DelayCompleteCount = decodeCorpusOperand[uint32](t, operand)
	case "method_consistent":
		value.MethodConsistent = decodeCorpusOperand[bool](t, operand)
	case "destination_consistent":
		value.DestinationConsistent = decodeCorpusOperand[bool](t, operand)
	case "body_consistent":
		value.BodyConsistent = decodeCorpusOperand[bool](t, operand)
	case "credential":
		value.Credential = CredentialState(decodeCorpusOperand[string](t, operand))
	case "cleanup":
		value.Cleanup = CleanupState(decodeCorpusOperand[string](t, operand))
	default:
		t.Fatalf("unsupported observation field %q", field)
	}
}

func removeCorpusSemanticTarget(t *testing.T, result *Result, target string) {
	t.Helper()
	parts := corpusTargetParts(t, target)
	switch {
	case len(parts) == 2 && parts[0] == "scenarios":
		index := corpusIndex(t, parts[1], len(result.Scenarios))
		result.Scenarios = append(result.Scenarios[:index], result.Scenarios[index+1:]...)
	case len(parts) == 4 && parts[0] == "scenarios" && parts[2] == "findings":
		rowIndex := corpusIndex(t, parts[1], len(result.Scenarios))
		findings := result.Scenarios[rowIndex].Findings
		findingIndex := corpusIndex(t, parts[3], len(findings))
		result.Scenarios[rowIndex].Findings = append(findings[:findingIndex], findings[findingIndex+1:]...)
	case len(parts) == 4 && parts[0] == "scenarios" && parts[2] == "observation" && parts[3] == "attempt_limit":
		rowIndex := corpusIndex(t, parts[1], len(result.Scenarios))
		result.Scenarios[rowIndex].Observation.AttemptLimit = 0
	case len(parts) == 4 && parts[0] == "scenarios" && parts[2] == "observation" && parts[3] == "protocol":
		rowIndex := corpusIndex(t, parts[1], len(result.Scenarios))
		result.Scenarios[rowIndex].Observation.Protocol = ""
	default:
		t.Fatalf("unsupported remove target %q", target)
	}
}

func duplicateCorpusSemanticTarget(t *testing.T, result *Result, target string) {
	t.Helper()
	parts := corpusTargetParts(t, target)
	switch {
	case len(parts) == 2 && parts[0] == "scenarios":
		index := corpusIndex(t, parts[1], len(result.Scenarios))
		duplicate := cloneScenarioResult(result.Scenarios[index])
		result.Scenarios = append(result.Scenarios, ScenarioResult{})
		copy(result.Scenarios[index+2:], result.Scenarios[index+1:])
		result.Scenarios[index+1] = duplicate
	case len(parts) == 4 && parts[0] == "scenarios" && parts[2] == "findings":
		rowIndex := corpusIndex(t, parts[1], len(result.Scenarios))
		findings := result.Scenarios[rowIndex].Findings
		findingIndex := corpusIndex(t, parts[3], len(findings))
		finding := findings[findingIndex]
		findings = append(findings, "")
		copy(findings[findingIndex+2:], findings[findingIndex+1:])
		findings[findingIndex+1] = finding
		result.Scenarios[rowIndex].Findings = findings
	default:
		t.Fatalf("unsupported duplicate target %q", target)
	}
}

func swapCorpusSemanticTargets(t *testing.T, result *Result, left, right string) {
	t.Helper()
	leftParts := corpusTargetParts(t, left)
	rightParts := corpusTargetParts(t, right)
	switch {
	case len(leftParts) == 2 && len(rightParts) == 2 &&
		leftParts[0] == "scenarios" && rightParts[0] == "scenarios":
		leftIndex := corpusIndex(t, leftParts[1], len(result.Scenarios))
		rightIndex := corpusIndex(t, rightParts[1], len(result.Scenarios))
		result.Scenarios[leftIndex], result.Scenarios[rightIndex] =
			result.Scenarios[rightIndex], result.Scenarios[leftIndex]
	case len(leftParts) == 4 && len(rightParts) == 4 && leftParts[0] == "scenarios" &&
		rightParts[0] == "scenarios" && leftParts[2] == "findings" && rightParts[2] == "findings":
		leftRow := corpusIndex(t, leftParts[1], len(result.Scenarios))
		rightRow := corpusIndex(t, rightParts[1], len(result.Scenarios))
		leftIndex := corpusIndex(t, leftParts[3], len(result.Scenarios[leftRow].Findings))
		rightIndex := corpusIndex(t, rightParts[3], len(result.Scenarios[rightRow].Findings))
		result.Scenarios[leftRow].Findings[leftIndex], result.Scenarios[rightRow].Findings[rightIndex] =
			result.Scenarios[rightRow].Findings[rightIndex], result.Scenarios[leftRow].Findings[leftIndex]
	default:
		t.Fatalf("unsupported swap targets %q/%q", left, right)
	}
}

func corpusTargetParts(t *testing.T, target string) []string {
	t.Helper()
	if !strings.HasPrefix(target, "/") || target == "/" || strings.Contains(target, "//") {
		t.Fatalf("invalid corpus target %q", target)
	}
	return strings.Split(strings.TrimPrefix(target, "/"), "/")
}

func corpusIndex(t *testing.T, text string, length int) int {
	t.Helper()
	index, err := strconv.Atoi(text)
	if err != nil || index < 0 || index >= length {
		t.Fatalf("corpus index %q outside length %d", text, length)
	}
	return index
}

func decodeCorpusOperand[T any](t *testing.T, operand string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(operand), &value); err != nil {
		t.Fatalf("decode corpus operand %q: %v", operand, err)
	}
	return value
}

func cloneScenarioResult(source ScenarioResult) ScenarioResult {
	result := source
	if source.Findings == nil {
		return result
	}
	capacity := len(source.Findings)
	if capacity == 0 {
		capacity = 1
	}
	result.Findings = make([]FindingCode, len(source.Findings), capacity)
	copy(result.Findings, source.Findings)
	return result
}
