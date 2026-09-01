package scenariosuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testFailure uint8

func (testFailure) Error() string { return "scenario suite test failure" }

const (
	sanitationSuppliedErrorMarker  = "HTTP_RETRY_CHECK_SANITATION_SUPPLIED_ERROR_01"
	sanitationPanicMarker          = "HTTP_RETRY_CHECK_SANITATION_PANIC_VALUE_02"
	sanitationResponseFieldMarker  = "HTTP_RETRY_CHECK_SANITATION_RESPONSE_FIELD_03"
	sanitationResponseBodyMarker   = "HTTP_RETRY_CHECK_SANITATION_RESPONSE_BODY_04"
	sanitationResponseHeaderMarker = "HTTP_RETRY_CHECK_SANITATION_RESPONSE_HEADER_05"
	sanitationRequestURLMarker     = "HTTP_RETRY_CHECK_SANITATION_REQUEST_URL_06"
	sanitationRequestBodyMarker    = "HTTP_RETRY_CHECK_SANITATION_REQUEST_BODY_07"
	sanitationRequestHeaderMarker  = "HTTP_RETRY_CHECK_SANITATION_REQUEST_HEADER_08"
	sanitationConcreteTypeMarker   = "brpSanitationConcreteClientMarker"
)

type sanitationMode uint8

const (
	sanitationErrorMode sanitationMode = iota + 1
	sanitationPanicMode
	sanitationFieldsMode
)

type sanitationMarkerError string

func (failure sanitationMarkerError) Error() string { return string(failure) }

type brpSanitationConcreteClientMarker struct {
	mode sanitationMode
}

func (client *brpSanitationConcreteClientMarker) Do(
	request *http.Request,
) (*http.Response, error) {
	if request != nil && request.Body != nil {
		_ = request.Body.Close()
	}
	switch client.mode {
	case sanitationErrorMode:
		return nil, sanitationMarkerError(sanitationSuppliedErrorMarker)
	case sanitationPanicMode:
		panic(sanitationPanicMarker)
	case sanitationFieldsMode:
		if request.URL != nil {
			request.URL.Scheme = "marker"
			request.URL.Host = sanitationRequestURLMarker
			request.URL.Path = "/" + sanitationRequestURLMarker
			request.URL.RawQuery = "marker=" + sanitationRequestURLMarker
		}
		request.Header.Set("X-HTTP-Retry-Check-Marker", sanitationRequestHeaderMarker)
		request.Body = newSanitationMarkerBody(sanitationRequestBodyMarker)
		request.GetBody = func() (io.ReadCloser, error) {
			return newSanitationMarkerBody(sanitationRequestBodyMarker), nil
		}
		return &http.Response{
			Status: sanitationResponseFieldMarker, StatusCode: 599,
			Proto: sanitationResponseFieldMarker,
			Header: http.Header{
				"X-HTTP-Retry-Check-Marker": []string{sanitationResponseHeaderMarker},
			},
			Body: newSanitationMarkerBody(sanitationResponseBodyMarker), Request: request,
		}, nil
	default:
		return nil, sanitationMarkerError(sanitationSuppliedErrorMarker)
	}
}

type sanitationMarkerBody struct {
	reader *strings.Reader
	marker string
}

func newSanitationMarkerBody(marker string) *sanitationMarkerBody {
	return &sanitationMarkerBody{reader: strings.NewReader(marker), marker: marker}
}

func (body *sanitationMarkerBody) Read(value []byte) (int, error) {
	return body.reader.Read(value)
}

func (*sanitationMarkerBody) Close() error { return nil }
func (body *sanitationMarkerBody) String() string {
	return body.marker
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestControlledLiteralIdentity(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		length int
		digest string
	}{
		{
			name: "ordinary", value: syntheticBodyText, length: 47,
			digest: "bcb750ca1e2ddd20e4440273ddd40c364b43a0e805d84750ae11caa2c59917ef",
		},
		{
			name: "changed", value: changedSyntheticBodyText, length: 47,
			digest: "01a7ec771db34ae2e2e1a061501bab03863cf4f0f62290bd78ff546ea149b693",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest := sha256.Sum256([]byte(test.value))
			if len(test.value) != test.length || hex.EncodeToString(digest[:]) != test.digest {
				t.Fatalf("literal length/digest = %d/%x", len(test.value), digest)
			}
		})
	}
	if syntheticBodyText == changedSyntheticBodyText ||
		syntheticCredential != "Bearer http-retry-check-synthetic-scenario-suite-v1" ||
		controlledPath != "/case" || delayedResponseDuration != 250*time.Millisecond ||
		!strings.HasPrefix(noContentResponseText, "HTTP/1.1 204 ") ||
		!strings.HasPrefix(unavailableResponseText, "HTTP/1.1 503 ") ||
		strings.Contains(unavailableResponseText, "Retry-After") {
		t.Fatal("controlled literal identity changed")
	}
}

func TestVocabularyAndRunErrors(t *testing.T) {
	wantScenarios := []ScenarioID{
		"accept_then_disconnect", "disconnect_before_acceptance", "changed_body_retry",
		"cross_origin_redirect_credentials", "retry_limit", "delayed_response",
	}
	if !reflect.DeepEqual(orderedScenarios(), wantScenarios) ||
		!reflect.DeepEqual([]ScenarioID{
			ScenarioAcceptThenDisconnect, ScenarioDisconnectBeforeAcceptance, ScenarioChangedBodyRetry,
			ScenarioCrossOriginRedirectCredentials, ScenarioRetryLimit, ScenarioDelayedResponse,
		}, wantScenarios) {
		t.Fatal("scenario vocabulary/order changed")
	}
	assertStringVocabulary(t, "assessment", []string{
		string(AssessmentNoUnsafeBehaviorObserved), string(AssessmentUnsafeBehaviorObserved),
		string(AssessmentInconclusive),
	}, []string{"no_unsafe_behavior_observed", "unsafe_behavior_observed", "inconclusive"})
	assertStringVocabulary(t, "cleanup", []string{
		string(CleanupSucceeded), string(CleanupFailed),
	}, []string{"succeeded", "failed"})
	assertStringVocabulary(t, "credential", []string{
		string(CredentialNotObserved), string(CredentialSourceOnly),
		string(CredentialAbsentAtTarget), string(CredentialExposedAtTarget), string(CredentialMissing),
	}, []string{"not_observed", "source_only", "absent_at_target", "exposed_at_target", "missing"})
	assertStringVocabulary(t, "finding", []string{
		string(FindingAttemptNotObserved), string(FindingCaptureIncomplete),
		string(FindingResponseIncomplete), string(FindingDelayIncomplete),
		string(FindingAttemptLimitExceeded), string(FindingRetryBeforeResponse),
		string(FindingRetryAfterAcceptedRequest), string(FindingRetryAfterUnconfirmedAcceptance),
		string(FindingMethodChanged), string(FindingDestinationChanged), string(FindingBodyChanged),
		string(FindingCredentialNotObserved), string(FindingCredentialMissing),
		string(FindingCredentialExposedAtTarget), string(FindingEffectNotObserved),
		string(FindingEffectLimitExceeded), string(FindingCleanupUnverified),
		string(FindingScenarioIncomplete),
	}, []string{
		"attempt_not_observed", "capture_incomplete", "response_incomplete", "delay_incomplete",
		"attempt_limit_exceeded", "retry_before_response", "retry_after_accepted_request",
		"retry_after_unconfirmed_acceptance", "method_changed", "destination_changed",
		"body_changed", "credential_not_observed", "credential_missing",
		"credential_exposed_at_target", "effect_not_observed", "effect_limit_exceeded",
		"cleanup_unverified", "scenario_incomplete",
	})
	errors := []struct {
		value  RunError
		number uint8
		text   string
	}{
		{ErrInvalidCall, 1, "HTTP scenario suite call is invalid"},
		{ErrSuiteUnavailable, 2, "HTTP scenario suite is unavailable"},
		{ErrInternalFailure, 3, "HTTP scenario suite failed internally"},
		{ErrInvalidResult, 4, "HTTP scenario suite result is invalid"},
		{RunError(0), 0, "HTTP scenario suite failure is invalid"},
		{RunError(255), 255, "HTTP scenario suite failure is invalid"},
	}
	for _, test := range errors {
		if uint8(test.value) != test.number || test.value.Error() != test.text {
			t.Fatalf("RunError %d = %d/%q", test.number, test.value, test.value.Error())
		}
	}
}

func assertStringVocabulary(t *testing.T, name string, actual, expected []string) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s vocabulary = %v, want %v", name, actual, expected)
	}
}

func TestRunAdmitsAllSevenListenersBeforeFirstInvocation(t *testing.T) {
	tracker := newListenerTracker(0, 0)
	dependency := trackedDependencies(tracker)
	var calls atomic.Uint32
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 && tracker.callCount() != 7 {
			t.Errorf("first invocation observed %d admitted listeners", tracker.callCount())
		}
		_ = request.Body.Close()
		return nil, testFailure(1)
	})

	result, err := run(context.Background(), doer, dependency)
	if err != nil || calls.Load() != 1 || len(result.Scenarios) != 6 || Validate(result) != nil {
		t.Fatalf("run result/calls/error = %#v/%d/%v", result, calls.Load(), err)
	}
	tracker.assertAllClosed(t)
}

func TestRunAdmissionFailureClosesAllListeners(t *testing.T) {
	tests := []struct {
		name          string
		closeFailure  int
		expectedError error
	}{
		{name: "verified", expectedError: ErrSuiteUnavailable},
		{name: "close failure", closeFailure: 2, expectedError: ErrInternalFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracker := newListenerTracker(4, test.closeFailure)
			var calls atomic.Uint32
			result, err := run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, testFailure(1)
			}), trackedDependencies(tracker))
			if err != test.expectedError || !reflect.DeepEqual(result, Result{}) || calls.Load() != 0 {
				t.Fatalf("run = %#v/%v, calls=%d", result, err, calls.Load())
			}
			tracker.assertAllClosed(t)
		})
	}
}

func TestRunRejectsDuplicateListenerAddressesAcrossWholeAdmission(t *testing.T) {
	tests := []struct {
		name        string
		sourceCall  int
		duplicateAt int
	}{
		{name: "cross scenario", sourceCall: 1, duplicateAt: 2},
		{name: "redirect pair", sourceCall: 4, duplicateAt: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracker := newDuplicateListenerTracker(test.sourceCall, test.duplicateAt)
			var calls atomic.Uint32
			result, err := run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, testFailure(1)
			}), trackedDependencies(tracker))
			if err != ErrSuiteUnavailable || !reflect.DeepEqual(result, Result{}) || calls.Load() != 0 {
				t.Fatalf("duplicate admission = %#v/%v, calls=%d", result, err, calls.Load())
			}
			tracker.assertAllClosed(t)
		})
	}
}

func TestRunClosesEveryUnrunListenerAfterInvalidInvocation(t *testing.T) {
	tracker := newListenerTracker(0, 0)
	var calls atomic.Uint32
	result, err := run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		_ = request.Body.Close()
		panic(testFailure(1))
	}), trackedDependencies(tracker))
	if err != nil || calls.Load() != 1 || Validate(result) != nil || len(result.Scenarios) != 6 {
		t.Fatalf("run = %#v/%v, calls=%d", result, err, calls.Load())
	}
	if result.Scenarios[0].Observation.CaptureComplete ||
		result.Scenarios[0].Observation.Cleanup != CleanupSucceeded {
		t.Fatalf("invalid invocation row = %#v", result.Scenarios[0])
	}
	for index := 1; index < len(result.Scenarios); index++ {
		observation := result.Scenarios[index].Observation
		if observation.CaptureComplete || observation.Cleanup != CleanupSucceeded {
			t.Fatalf("unrun row %d = %#v", index, result.Scenarios[index])
		}
	}
	tracker.assertAllClosed(t)
}

func TestRunDeadlineCannotProducePositiveAssessment(t *testing.T) {
	tracker := newListenerTracker(0, 0)
	dependency := trackedDependencies(tracker)
	dependency.caseTimeout = 20 * time.Millisecond
	dependency.cleanupTimeout = 100 * time.Millisecond
	var calls atomic.Uint32
	result, err := run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-request.Context().Done()
		_ = request.Body.Close()
		return nil, testFailure(1)
	}), dependency)
	if err != nil || calls.Load() != 1 || Validate(result) != nil ||
		result.Assessment != AssessmentInconclusive || result.Scenarios[0].Observation.CaptureComplete {
		t.Fatalf("deadline run = %#v/%v, calls=%d", result, err, calls.Load())
	}
	tracker.assertAllClosed(t)
}

func TestInvocationOutcomeAndBodyQuiescenceAreIndependent(t *testing.T) {
	tests := []struct {
		name    string
		doer    Doer
		cleanup CleanupState
		capture bool
	}{
		{
			name: "nil success response",
			doer: doerFunc(func(request *http.Request) (*http.Response, error) {
				_ = request.Body.Close()
				return nil, nil
			}),
			cleanup: CleanupSucceeded,
		},
		{
			name: "response close failure",
			doer: doerFunc(func(request *http.Request) (*http.Response, error) {
				_ = request.Body.Close()
				return &http.Response{Body: &faultResponseBody{mode: responseCloseFailure}}, nil
			}),
			cleanup: CleanupSucceeded,
		},
		{
			name: "response close panic",
			doer: doerFunc(func(request *http.Request) (*http.Response, error) {
				_ = request.Body.Close()
				return &http.Response{Body: &faultResponseBody{mode: responseClosePanic}}, nil
			}),
			cleanup: CleanupSucceeded,
		},
		{
			name: "body not quiesced",
			doer: doerFunc(func(*http.Request) (*http.Response, error) {
				return nil, testFailure(1)
			}),
			cleanup: CleanupFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tracker := newListenerTracker(0, 0)
			dependency := trackedDependencies(tracker)
			dependency.caseTimeout = 20 * time.Millisecond
			result, err := run(context.Background(), test.doer, dependency)
			if err != nil || Validate(result) != nil || result.Scenarios[0].Observation.CaptureComplete != test.capture ||
				result.Scenarios[0].Observation.Cleanup != test.cleanup {
				t.Fatalf("invocation outcome = %#v/%v", result, err)
			}
			tracker.assertAllClosed(t)
		})
	}
}

func TestUnusedRedirectReplayCloseDoesNotPass(t *testing.T) {
	t.Run("one unused redirect replay", func(t *testing.T) {
		request, bodies, ok := newScenarioRequest(
			context.Background(), "http://127.0.0.1:1/case",
			ScenarioCrossOriginRedirectCredentials,
		)
		if !ok || request.Body.Close() != nil {
			t.Fatal("construct or close original body")
		}
		replay, err := request.GetBody()
		if err != nil {
			t.Fatal(err)
		}
		bodies.permitUnusedRedirectReplayClose(http.StatusTemporaryRedirect)
		bodies.seal()
		waitContext, cancelWait := context.WithTimeout(context.Background(), 100*time.Millisecond)
		waitErr := bodies.wait(waitContext)
		cancelWait()
		if waitErr != nil {
			t.Fatalf("unused redirect replay did not close: %v", waitErr)
		}
		buffer := make([]byte, 1)
		if count, readErr := replay.Read(buffer); count != 0 || readErr != invocationFailure {
			t.Fatalf("closed replay read = %d/%v", count, readErr)
		}
		if err := replay.Close(); err != nil {
			t.Fatal(err)
		}
	})

	tests := []struct {
		name       string
		scenario   ScenarioID
		count      int
		start      bool
		closeFirst bool
		permit     bool
	}{
		{
			name: "started redirect replay", scenario: ScenarioCrossOriginRedirectCredentials,
			count: 1, start: true, permit: true,
		},
		{
			name: "multiple redirect replays", scenario: ScenarioCrossOriginRedirectCredentials,
			count: 2, permit: true,
		},
		{
			name: "closed replay before leaked replay", scenario: ScenarioCrossOriginRedirectCredentials,
			count: 2, closeFirst: true, permit: true,
		},
		{
			name:     "started closed replay before leaked replay",
			scenario: ScenarioCrossOriginRedirectCredentials,
			count:    2, start: true, closeFirst: true, permit: true,
		},
		{
			name: "redirect replay without returned 307", scenario: ScenarioCrossOriginRedirectCredentials,
			count: 1,
		},
		{
			name: "unused nonredirect replay", scenario: ScenarioAcceptThenDisconnect,
			count: 1, permit: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, bodies, ok := newScenarioRequest(
				context.Background(), "http://127.0.0.1:1/case", test.scenario,
			)
			if !ok || request.Body.Close() != nil {
				t.Fatal("construct or close original body")
			}
			replays := make([]io.ReadCloser, 0, test.count)
			for range test.count {
				replay, err := request.GetBody()
				if err != nil {
					t.Fatal(err)
				}
				replays = append(replays, replay)
			}
			if test.start {
				buffer := make([]byte, 1)
				if count, err := replays[0].Read(buffer); count != 1 || err != nil {
					t.Fatalf("start replay = %d/%v", count, err)
				}
			}
			if test.closeFirst {
				if err := replays[0].Close(); err != nil {
					t.Fatal(err)
				}
			}
			if test.permit {
				bodies.permitUnusedRedirectReplayClose(http.StatusTemporaryRedirect)
			}
			bodies.seal()
			waitContext, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
			waitErr := bodies.wait(waitContext)
			cancelWait()
			if waitErr != invocationFailure {
				t.Fatalf("unsettled replay wait = %v", waitErr)
			}
			for _, replay := range replays {
				if err := replay.Close(); err != nil {
					t.Fatal(err)
				}
			}
			settledContext, cancelSettled := context.WithTimeout(context.Background(), 100*time.Millisecond)
			settledErr := bodies.wait(settledContext)
			cancelSettled()
			if settledErr != nil {
				t.Fatalf("closed replays did not settle: %v", settledErr)
			}
		})
	}
}

func TestNonnilInvocationErrorNeverTouchesReturnedResponse(t *testing.T) {
	responseBody := &panicOnSecondCloseBody{}
	if err := responseBody.Close(); err != nil {
		t.Fatal(err)
	}
	request, bodies, ok := newScenarioRequest(
		context.Background(), "http://127.0.0.1:1/case", ScenarioAcceptThenDisconnect,
	)
	if !ok {
		t.Fatal("construct request")
	}
	outcome := invokeDoer(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
		_ = request.Body.Close()
		return &http.Response{Body: responseBody}, testFailure(1)
	}), request, bodies)
	if outcome.completion != invocationErrorCompletion || !outcome.bodiesQuiesced ||
		responseBody.closeCalls.Load() != 1 {
		t.Fatalf("error response outcome = %#v, closes=%d", outcome, responseBody.closeCalls.Load())
	}
}

func TestTypedNilUsesOrdinaryDispatchAndDoesNotPass(t *testing.T) {
	tracker := newListenerTracker(0, 0)
	var doer *typedNilDoer
	result, err := run(context.Background(), doer, trackedDependencies(tracker))
	if err != nil || Validate(result) != nil || result.Scenarios[0].Observation.CaptureComplete ||
		result.Scenarios[0].Observation.Cleanup != CleanupSucceeded {
		t.Fatalf("typed-nil dispatch = %#v/%v", result, err)
	}
	tracker.assertAllClosed(t)
}

func TestUnrunListenerCloseFailureAffectsOnlyItsCleanupRow(t *testing.T) {
	tracker := newListenerTracker(0, 2)
	result, err := run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
		_ = request.Body.Close()
		panic(testFailure(1))
	}), trackedDependencies(tracker))
	if err != nil || Validate(result) != nil || result.Scenarios[0].Observation.Cleanup != CleanupSucceeded ||
		result.Scenarios[1].Observation.Cleanup != CleanupFailed {
		t.Fatalf("unrun close-failure result = %#v/%v", result, err)
	}
	for index := 2; index < len(result.Scenarios); index++ {
		if result.Scenarios[index].Observation.Cleanup != CleanupSucceeded {
			t.Fatalf("unrelated unrun cleanup %d = %#v", index, result.Scenarios[index])
		}
	}
	tracker.assertAllClosed(t)
}

func TestValidateRejectsContradictoryShapesWithoutMutation(t *testing.T) {
	valid := canonicalPositiveResult()
	before := cloneInternalResult(valid)
	if Validate(valid) != nil || !reflect.DeepEqual(valid, before) {
		t.Fatal("Validate mutated a valid result")
	}
	tests := []struct {
		name string
		edit func(*Result)
	}{
		{name: "nil scenarios", edit: func(result *Result) { result.Scenarios = nil }},
		{name: "partial", edit: func(result *Result) { result.Scenarios = result.Scenarios[:5] }},
		{name: "additional", edit: func(result *Result) {
			result.Scenarios = append(result.Scenarios, result.Scenarios[0])
		}},
		{name: "reordered", edit: func(result *Result) {
			result.Scenarios[0], result.Scenarios[1] = result.Scenarios[1], result.Scenarios[0]
		}},
		{name: "duplicate", edit: func(result *Result) {
			result.Scenarios[1].Scenario = result.Scenarios[0].Scenario
		}},
		{name: "unknown scenario", edit: func(result *Result) { result.Scenarios[0].Scenario = "unknown" }},
		{name: "unknown aggregate", edit: func(result *Result) { result.Assessment = "unknown" }},
		{name: "nil findings", edit: func(result *Result) { result.Scenarios[0].Findings = nil }},
		{name: "reordered findings", edit: func(result *Result) {
			row := unavailableRow(ScenarioAcceptThenDisconnect, CleanupFailed)
			row.Findings[0], row.Findings[len(row.Findings)-1] = row.Findings[len(row.Findings)-1], row.Findings[0]
			result.Scenarios[0] = row
		}},
		{name: "attempt overflow", edit: func(result *Result) { result.Scenarios[0].Observation.AttemptCount = 4 }},
		{name: "effect overflow", edit: func(result *Result) { result.Scenarios[0].Observation.EffectCount = 2 }},
		{name: "overlap complete", edit: func(result *Result) { result.Scenarios[0].Observation.OverlapCount = 1 }},
		{name: "complete accept missing causal count", edit: func(result *Result) {
			row := &result.Scenarios[0]
			row.Observation.AttemptCount = 2
			row.Observation.EffectCount = 2
		}},
		{name: "complete cross missing response attempt", edit: func(result *Result) {
			row := &result.Scenarios[3]
			row.Observation.AttemptCount = 2
		}},
		{name: "complete delayed missing causal count", edit: func(result *Result) {
			row := &result.Scenarios[5]
			row.Observation.AttemptCount = 2
			row.Observation.EffectCount = 2
			row.Observation.DelayCompleteCount = 2
			row.Observation.ResponseAttemptCount = 2
		}},
		{name: "response contradiction", edit: func(result *Result) {
			result.Scenarios[4].Observation.FirstResponseComplete = false
		}},
		{name: "delay without effect", edit: func(result *Result) {
			result.Scenarios[5].Observation.EffectCount = 0
		}},
		{name: "absent target without effect", edit: func(result *Result) {
			row := &result.Scenarios[3]
			row.Observation.AttemptCount = 2
			row.Observation.Credential = CredentialAbsentAtTarget
		}},
		{name: "absent target missing source response attempt", edit: func(result *Result) {
			row := &result.Scenarios[3]
			row.Observation.CaptureComplete = false
			row.Observation.AttemptCount = 3
			row.Observation.EffectCount = 2
			row.Observation.ResponseAttemptCount = 2
			row.Observation.ResponseCompleteCount = 2
			row.Observation.Credential = CredentialAbsentAtTarget
		}},
		{name: "missing credential missing source response attempt", edit: func(result *Result) {
			row := &result.Scenarios[3]
			row.Observation.CaptureComplete = false
			row.Observation.AttemptCount = 3
			row.Observation.EffectCount = 2
			row.Observation.ResponseAttemptCount = 2
			row.Observation.ResponseCompleteCount = 2
			row.Observation.Credential = CredentialMissing
		}},
		{name: "source-only without complete source", edit: func(result *Result) {
			row := &result.Scenarios[3]
			row.Observation.CaptureComplete = false
			row.Observation.ResponseAttemptCount = 0
			row.Observation.ResponseCompleteCount = 0
			row.Observation.FirstResponseComplete = false
		}},
		{name: "accepted source credential without effect", edit: func(result *Result) {
			row := &result.Scenarios[0]
			row.Observation.CaptureComplete = false
			row.Observation.EffectCount = 0
		}},
		{name: "body mismatch without complete request", edit: func(result *Result) {
			row := &result.Scenarios[0]
			row.Observation.CaptureComplete = false
			row.Observation.EffectCount = 0
			row.Observation.BodyConsistent = false
			row.Observation.Credential = CredentialNotObserved
		}},
		{name: "preaccept body mismatch without complete request", edit: func(result *Result) {
			row := &result.Scenarios[1]
			row.Observation.CaptureComplete = false
			row.Observation.BodyConsistent = false
			row.Observation.Credential = CredentialNotObserved
		}},
		{name: "accepted effect without credential observation", edit: func(result *Result) {
			result.Scenarios[0].Observation.Credential = CredentialNotObserved
		}},
		{name: "complete preaccept without credential observation", edit: func(result *Result) {
			result.Scenarios[1].Observation.Credential = CredentialNotObserved
		}},
		{name: "preaccept retry without credential observation", edit: func(result *Result) {
			row := &result.Scenarios[1]
			row.Observation.CaptureComplete = false
			row.Observation.AttemptCount = 2
			row.Observation.RetryAfterUnconfirmedCount = 1
			row.Observation.Credential = CredentialNotObserved
		}},
		{name: "cross source response without credential observation", edit: func(result *Result) {
			result.Scenarios[3].Observation.Credential = CredentialNotObserved
		}},
		{name: "retry response without credential observation", edit: func(result *Result) {
			result.Scenarios[4].Observation.Credential = CredentialNotObserved
		}},
		{name: "delayed effect without credential observation", edit: func(result *Result) {
			result.Scenarios[5].Observation.Credential = CredentialNotObserved
		}},
		{name: "unknown credential", edit: func(result *Result) {
			result.Scenarios[0].Observation.Credential = "unknown"
		}},
		{name: "unknown cleanup", edit: func(result *Result) {
			result.Scenarios[0].Observation.Cleanup = "unknown"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			forged := cloneInternalResult(valid)
			test.edit(&forged)
			if Validate(forged) != ErrInvalidResult {
				t.Fatalf("Validate accepted %#v", forged)
			}
		})
	}
}

func TestObservationsAreEncodableAndFindingsRemainApplicable(t *testing.T) {
	tests := []struct {
		name        string
		scenario    ScenarioID
		observation Observation
		assessment  Assessment
		findings    []FindingCode
	}{
		{
			name: "later response only", scenario: ScenarioRetryLimit,
			observation: observationForTest(2, 0, 2, 1, false, CredentialSourceOnly),
			assessment:  AssessmentInconclusive,
			findings:    []FindingCode{FindingResponseIncomplete},
		},
		{
			name: "incomplete redirect target source-only", scenario: ScenarioCrossOriginRedirectCredentials,
			observation: func() Observation {
				value := observationForTest(2, 0, 1, 1, true, CredentialSourceOnly)
				value.CaptureComplete = false
				return value
			}(),
			assessment: AssessmentInconclusive,
			findings: []FindingCode{
				FindingCaptureIncomplete, FindingResponseIncomplete, FindingEffectNotObserved,
			},
		},
		{
			name: "two target effects", scenario: ScenarioCrossOriginRedirectCredentials,
			observation: func() Observation {
				value := observationForTest(3, 2, 3, 3, true, CredentialAbsentAtTarget)
				value.DestinationConsistent = false
				return value
			}(),
			assessment: AssessmentUnsafeBehaviorObserved,
			findings: []FindingCode{
				FindingAttemptLimitExceeded, FindingDestinationChanged, FindingEffectLimitExceeded,
			},
		},
		{
			name: "complete preaccept body mismatch", scenario: ScenarioDisconnectBeforeAcceptance,
			observation: func() Observation {
				value := observationForTest(1, 0, 0, 0, false, CredentialSourceOnly)
				value.BodyConsistent = false
				return value
			}(),
			assessment: AssessmentUnsafeBehaviorObserved,
			findings:   []FindingCode{FindingBodyChanged},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row, ok := newScenarioResult(test.scenario, test.observation)
			if !ok || row.Assessment != test.assessment || !equalFindings(row.Findings, test.findings) {
				t.Fatalf("row = %#v, ok=%v", row, ok)
			}
			result := resultReplacing(test.scenario, row)
			if Validate(result) != nil {
				t.Fatalf("result rejected = %#v", result)
			}
		})
	}

	wrongCounters := []struct {
		name     string
		scenario ScenarioID
		value    Observation
	}{
		{
			name: "accept unconfirmed", scenario: ScenarioAcceptThenDisconnect,
			value: func() Observation {
				value := observationForTest(2, 1, 0, 0, false, CredentialSourceOnly)
				value.RetryAfterUnconfirmedCount = 1
				return value
			}(),
		},
		{
			name: "redirect after-effect", scenario: ScenarioCrossOriginRedirectCredentials,
			value: func() Observation {
				value := observationForTest(2, 1, 2, 2, true, CredentialAbsentAtTarget)
				value.RetryAfterEffectCount = 1
				return value
			}(),
		},
		{
			name: "retry unconfirmed", scenario: ScenarioRetryLimit,
			value: func() Observation {
				value := observationForTest(2, 0, 2, 2, true, CredentialSourceOnly)
				value.RetryAfterUnconfirmedCount = 1
				return value
			}(),
		},
		{
			name: "delayed unconfirmed", scenario: ScenarioDelayedResponse,
			value: func() Observation {
				value := observationForTest(2, 1, 1, 1, true, CredentialSourceOnly)
				value.DelayCompleteCount = 1
				value.RetryAfterUnconfirmedCount = 1
				return value
			}(),
		},
	}
	for _, test := range wrongCounters {
		t.Run(test.name, func(t *testing.T) {
			if validObservation(test.scenario, test.value) {
				t.Fatalf("wrong-scenario counter accepted: %#v", test.value)
			}
			valid := canonicalPositiveResult()
			for index := range valid.Scenarios {
				if valid.Scenarios[index].Scenario == test.scenario {
					valid.Scenarios[index].Observation = test.value
				}
			}
			if Validate(valid) != ErrInvalidResult {
				t.Fatal("Validate accepted wrong-scenario causal counter")
			}
		})
	}
}

func TestIncompleteHTTP10TargetPreservesDuplicateCredentialExposure(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioCrossOriginRedirectCredentials, func(context.Context) bool { return true },
	)
	response := exchangeRawRequest(
		t, origin.endpoints[0].address,
		rawRequest(origin.endpoints[0].address, "HTTP/1.1", nil,
			syntheticBodyText, len(syntheticBodyText)),
	)
	if !strings.HasPrefix(response, "HTTP/1.1 307 ") {
		t.Fatalf("source response = %q", response)
	}
	targetRequest := rawRequest(origin.endpoints[1].address, "HTTP/1.0",
		[]string{syntheticCredential, "supplemental-synthetic-value"}, "{", len(syntheticBodyText))
	targetRequest = strings.Replace(targetRequest, "POST /case ", "GET /unexpected ", 1)
	sendRawAndClose(t, origin.endpoints[1].address, targetRequest)
	waitForHeadersObserved(t, origin, 2)
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, ok := newScenarioResult(ScenarioCrossOriginRedirectCredentials, observation)
	if !ok || row.Assessment != AssessmentUnsafeBehaviorObserved || observation.CaptureComplete ||
		observation.Credential != CredentialExposedAtTarget ||
		observation.MethodConsistent || observation.DestinationConsistent ||
		!containsFinding(row.Findings, FindingCredentialExposedAtTarget) ||
		!containsFinding(row.Findings, FindingCaptureIncomplete) ||
		!containsFinding(row.Findings, FindingMethodChanged) ||
		!containsFinding(row.Findings, FindingDestinationChanged) {
		t.Fatalf("truncated HTTP/1.0 target row = %#v, ok=%v", row, ok)
	}
}

func TestAuthorizationMarkerDetectionKeepsSourceValidationExact(t *testing.T) {
	address := netip.MustParseAddrPort("127.0.0.1:41001")
	tests := []struct {
		name        string
		values      []string
		wantExact   bool
		wantExposed bool
	}{
		{name: "exact", values: []string{syntheticCredential}, wantExact: true, wantExposed: true},
		{
			name: "prefix", values: []string{"forwarded " + syntheticCredential},
			wantExposed: true,
		},
		{
			name: "suffix", values: []string{syntheticCredential + " transformed"},
			wantExposed: true,
		},
		{name: "unrelated", values: []string{"Bearer unrelated-credential"}},
		{
			name: "multiple values", values: []string{
				"Bearer unrelated-credential",
				"forwarded " + syntheticCredential + " transformed",
			},
			wantExposed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := newMemoryConn(rawRequest(
				address,
				"HTTP/1.1",
				test.values,
				syntheticBodyText,
				len(syntheticBodyText),
			))
			observed := readScenarioAttempt(connection, address, time.Now().Add(time.Second))
			if !observed.complete || !observed.captureComplete ||
				observed.credentialExact != test.wantExact ||
				observed.credentialExposed != test.wantExposed {
				t.Fatalf(
					"credential capture = complete:%v capture:%v exact:%v exposed:%v",
					observed.complete,
					observed.captureComplete,
					observed.credentialExact,
					observed.credentialExposed,
				)
			}
		})
	}
}

func TestRedirectRefusalAndSourceMultiplicity(t *testing.T) {
	t.Run("refusal", func(t *testing.T) {
		origin, caseContext, cancelCase := startRawOrigin(
			t, ScenarioCrossOriginRedirectCredentials, func(context.Context) bool { return true },
		)
		exchangeRawRequest(
			t, origin.endpoints[0].address,
			rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
				syntheticBodyText, len(syntheticBodyText)),
		)
		observation := finishRawOrigin(t, origin, caseContext, cancelCase)
		row, ok := newScenarioResult(ScenarioCrossOriginRedirectCredentials, observation)
		if !ok || row.Assessment != AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 ||
			observation.AttemptCount != 1 || observation.Credential != CredentialSourceOnly {
			t.Fatalf("redirect refusal = %#v, ok=%v", row, ok)
		}
	})

	t.Run("duplicate source blocks absence claim", func(t *testing.T) {
		origin, caseContext, cancelCase := startRawOrigin(
			t, ScenarioCrossOriginRedirectCredentials, func(context.Context) bool { return true },
		)
		exchangeRawRequest(
			t, origin.endpoints[0].address,
			rawRequest(origin.endpoints[0].address, "HTTP/1.1",
				[]string{syntheticCredential, "additional-value"}, syntheticBodyText, len(syntheticBodyText)),
		)
		exchangeRawRequest(
			t, origin.endpoints[1].address,
			rawRequest(origin.endpoints[1].address, "HTTP/1.1", nil,
				syntheticBodyText, len(syntheticBodyText)),
		)
		observation := finishRawOrigin(t, origin, caseContext, cancelCase)
		row, ok := newScenarioResult(ScenarioCrossOriginRedirectCredentials, observation)
		if !ok || row.Assessment != AssessmentInconclusive || observation.Credential != CredentialMissing ||
			!containsFinding(row.Findings, FindingCredentialMissing) {
			t.Fatalf("source multiplicity = %#v, ok=%v", row, ok)
		}
	})
}

func TestRetryLimitCountsZeroThroughFour(t *testing.T) {
	for attempts := 0; attempts <= 4; attempts++ {
		t.Run(decimalString(attempts), func(t *testing.T) {
			origin, caseContext, cancelCase := startRawOrigin(
				t, ScenarioRetryLimit, func(context.Context) bool { return true },
			)
			for index := 0; index < attempts && index < maxObservedAttempts; index++ {
				exchangeRawRequest(
					t, origin.endpoints[0].address,
					rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
						syntheticBodyText, len(syntheticBodyText)),
				)
			}
			var observation Observation
			if attempts == 4 {
				sendRawBestEffort(
					t, origin.endpoints[0].address,
					rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
						syntheticBodyText, len(syntheticBodyText)),
				)
				waitForCaptureMissing(t, origin)
				observation = finishRawOriginAllowEnded(t, origin, cancelCase)
			} else {
				observation = finishRawOrigin(t, origin, caseContext, cancelCase)
			}
			row, ok := newScenarioResult(ScenarioRetryLimit, observation)
			if !ok {
				t.Fatalf("retry row is not encodable: %#v", observation)
			}
			switch attempts {
			case 0:
				if row.Assessment != AssessmentInconclusive ||
					!containsFinding(row.Findings, FindingAttemptNotObserved) {
					t.Fatalf("zero retry row = %#v", row)
				}
			case 1, 2:
				if row.Assessment != AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 {
					t.Fatalf("bounded retry row = %#v", row)
				}
			case 3, 4:
				if row.Assessment != AssessmentUnsafeBehaviorObserved ||
					!containsFinding(row.Findings, FindingAttemptLimitExceeded) {
					t.Fatalf("excess retry row = %#v", row)
				}
			}
			if attempts == 4 && observation.CaptureComplete {
				t.Fatal("fourth admission did not fail capture closed")
			}
		})
	}
}

func TestMalformedOversizedAndChunkFramingInputsAreBounded(t *testing.T) {
	tests := []struct {
		name    string
		request func(netip.AddrPort) string
	}{
		{name: "malformed", request: func(netip.AddrPort) string { return "BROKEN\r\n\r\n" }},
		{name: "oversized body", request: func(address netip.AddrPort) string {
			return rawRequest(address, "HTTP/1.1", []string{syntheticCredential},
				strings.Repeat("x", maxRequestBodySize+1), maxRequestBodySize+1)
		}},
		{name: "chunk framing exhausts wire budget", request: func(address netip.AddrPort) string {
			var request strings.Builder
			request.Grow(maxRequestBodySize + 4096)
			request.WriteString("POST /case HTTP/1.1\r\nHost: ")
			request.WriteString(address.String())
			request.WriteString("\r\nAuthorization: ")
			request.WriteString(syntheticCredential)
			request.WriteString("\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n")
			for request.Len() <= maxRequestBodySize+2048 {
				request.WriteString("1\r\na\r\n")
			}
			request.WriteString("0\r\n\r\n")
			return request.String()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin, caseContext, cancelCase := startRawOrigin(
				t, ScenarioAcceptThenDisconnect, func(context.Context) bool { return true },
			)
			sendRawBestEffort(t, origin.endpoints[0].address, test.request(origin.endpoints[0].address))
			waitForAttemptCount(t, origin, 1)
			observation := finishRawOrigin(t, origin, caseContext, cancelCase)
			row, ok := newScenarioResult(ScenarioAcceptThenDisconnect, observation)
			if !ok || observation.CaptureComplete || observation.EffectCount != 0 ||
				row.Assessment != AssessmentInconclusive ||
				!containsFinding(row.Findings, FindingCaptureIncomplete) {
				t.Fatalf("bounded malformed row = %#v, ok=%v", row, ok)
			}
		})
	}
}

func TestPostHeaderWireBudgetPropagatesIntoEveryResponsePhase(t *testing.T) {
	encodings := []struct {
		name            string
		request         func(netip.AddrPort) string
		remainingBudget int64
		captureComplete bool
	}{
		{
			name: "content length leaves one byte",
			request: func(address netip.AddrPort) string {
				return rawRequest(address, "HTTP/1.1", []string{syntheticCredential},
					strings.Repeat("x", maxRequestBodySize), maxRequestBodySize)
			},
			remainingBudget: 1,
			captureComplete: true,
		},
		{
			name:            "chunk framing leaves zero bytes",
			request:         zeroRemainingChunkedRequest,
			remainingBudget: 0,
			captureComplete: false,
		},
	}
	for _, encoding := range encodings {
		t.Run(encoding.name+" parser", func(t *testing.T) {
			address := netip.MustParseAddrPort("127.0.0.1:41001")
			connection := newMemoryConn(encoding.request(address))
			observed := readScenarioAttempt(connection, address, time.Now().Add(time.Second))
			if !observed.complete || observed.remainingReadBudget != encoding.remainingBudget ||
				observed.captureComplete != encoding.captureComplete || observed.bodyConsistent {
				t.Fatalf("parser budget = complete:%v remaining:%d capture:%v body:%v",
					observed.complete, observed.remainingReadBudget,
					observed.captureComplete, observed.bodyConsistent)
			}
		})
		for _, scenario := range []ScenarioID{
			ScenarioCrossOriginRedirectCredentials, ScenarioRetryLimit, ScenarioDelayedResponse,
		} {
			t.Run(encoding.name+" "+string(scenario), func(t *testing.T) {
				origin, endpoint := newPhaseTestOrigin(scenario)
				connection := newMemoryConn(encoding.request(endpoint.address))
				index, admitted := origin.admit(connection, endpoint.role)
				if !admitted {
					t.Fatal("memory connection was not admitted")
				}
				origin.observe(context.Background(), connection, endpoint, index)
				observation := origin.snapshot()
				row, ok := newScenarioResult(scenario, observation)
				if !ok || observation.CaptureComplete != encoding.captureComplete ||
					observation.ResponseAttemptCount != 1 || observation.ResponseCompleteCount != 1 ||
					!observation.FirstResponseComplete || observation.BodyConsistent ||
					row.Assessment != AssessmentUnsafeBehaviorObserved {
					t.Fatalf("propagated budget row = %#v, ok=%v", row, ok)
				}
				if scenario == ScenarioDelayedResponse && observation.DelayCompleteCount != 1 {
					t.Fatalf("delayed budget row = %#v", row)
				}
				if encoding.remainingBudget == 0 &&
					!containsFinding(row.Findings, FindingCaptureIncomplete) {
					t.Fatalf("zero budget was not fail-closed: %#v", row)
				}
			})
		}
	}
}

func TestCrossOriginRuntimePreservesTwoTargetEffects(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioCrossOriginRedirectCredentials, func(context.Context) bool { return true },
	)
	exchangeRawRequest(
		t, origin.endpoints[0].address,
		rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
			syntheticBodyText, len(syntheticBodyText)),
	)
	for range 2 {
		exchangeRawRequest(
			t, origin.endpoints[1].address,
			rawRequest(origin.endpoints[1].address, "HTTP/1.1", nil,
				syntheticBodyText, len(syntheticBodyText)),
		)
	}
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, ok := newScenarioResult(ScenarioCrossOriginRedirectCredentials, observation)
	if !ok || row.Assessment != AssessmentUnsafeBehaviorObserved ||
		observation.AttemptCount != 3 || observation.EffectCount != 2 ||
		observation.ResponseAttemptCount != 3 || observation.ResponseCompleteCount != 3 ||
		observation.Credential != CredentialAbsentAtTarget ||
		!containsFinding(row.Findings, FindingAttemptLimitExceeded) ||
		!containsFinding(row.Findings, FindingEffectLimitExceeded) {
		t.Fatalf("two-target row = %#v, ok=%v", row, ok)
	}
}

func TestCrossOriginThirdRequestCannotReenterSourceSequence(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioCrossOriginRedirectCredentials, func(context.Context) bool { return true },
	)
	exchangeRawRequest(
		t, origin.endpoints[0].address,
		rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
			syntheticBodyText, len(syntheticBodyText)),
	)
	exchangeRawRequest(
		t, origin.endpoints[1].address,
		rawRequest(origin.endpoints[1].address, "HTTP/1.1", nil,
			syntheticBodyText, len(syntheticBodyText)),
	)
	exchangeRawRequest(
		t, origin.endpoints[0].address,
		rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
			syntheticBodyText, len(syntheticBodyText)),
	)
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, ok := newScenarioResult(ScenarioCrossOriginRedirectCredentials, observation)
	if !ok || observation.DestinationConsistent || row.Assessment != AssessmentUnsafeBehaviorObserved ||
		!containsFinding(row.Findings, FindingDestinationChanged) {
		t.Fatalf("source-target-source row = %#v, ok=%v", row, ok)
	}
}

func TestBlockedResponseWriteUsesScenarioSpecificOverlapPhase(t *testing.T) {
	tests := []struct {
		name                string
		scenario            ScenarioID
		wantOverlap         uint32
		wantRetryAfter      uint32
		wantRetryBefore     uint32
		wantActiveOnBlocked uint32
	}{
		{name: "cross redirect write excluded", scenario: ScenarioCrossOriginRedirectCredentials,
			wantActiveOnBlocked: 1},
		{name: "retry response write excluded", scenario: ScenarioRetryLimit,
			wantActiveOnBlocked: 1},
		{name: "delayed response write included", scenario: ScenarioDelayedResponse,
			wantOverlap: 1, wantRetryAfter: 1, wantRetryBefore: 1, wantActiveOnBlocked: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin, client, cancel := startBlockedResponseWrite(t, test.scenario)
			defer cancel()
			laterServer, laterClient := net.Pipe()
			if _, admitted := origin.admit(laterServer, sourceEndpoint); !admitted {
				t.Fatal("later connection was not admitted")
			}
			origin.mu.Lock()
			overlap := origin.overlapCount
			retryAfter := origin.retryAfterEffect
			retryBefore := origin.retryBeforeResponse
			active := origin.active
			origin.mu.Unlock()
			if overlap != test.wantOverlap || retryAfter != test.wantRetryAfter ||
				retryBefore != test.wantRetryBefore || active != test.wantActiveOnBlocked {
				t.Fatalf("blocked phase = overlap:%d after:%d before:%d active:%d",
					overlap, retryAfter, retryBefore, active)
			}
			releaseUnobservedAdmission(origin, laterServer, laterClient)
			readBlockedResponse(t, client)
			waitForOriginHandlers(t, origin)
		})
	}
}

func TestDelayedPendingPhaseClearsBeforeLaterAdmission(t *testing.T) {
	for _, mode := range []string{"completed", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			origin, client, cancel := startBlockedResponseWrite(t, ScenarioDelayedResponse)
			defer cancel()
			if mode == "completed" {
				readBlockedResponse(t, client)
			} else {
				_ = client.Close()
			}
			waitForOriginHandlers(t, origin)
			origin.mu.Lock()
			pending := origin.pendingResponse
			active := origin.active
			origin.mu.Unlock()
			if pending != 0 || active != 0 {
				t.Fatalf("terminated phase = pending:%d active:%d", pending, active)
			}
			laterServer, laterClient := net.Pipe()
			if _, admitted := origin.admit(laterServer, sourceEndpoint); !admitted {
				t.Fatal("post-termination connection was not admitted")
			}
			origin.mu.Lock()
			overlap := origin.overlapCount
			retryAfter := origin.retryAfterEffect
			retryBefore := origin.retryBeforeResponse
			origin.mu.Unlock()
			if overlap != 0 || retryAfter != 1 || retryBefore != 0 {
				t.Fatalf("post-termination counters = overlap:%d after:%d before:%d",
					overlap, retryAfter, retryBefore)
			}
			releaseUnobservedAdmission(origin, laterServer, laterClient)
		})
	}
}

func TestDelayedSameConnectionPipelineDuringTimerIsNeverPositive(t *testing.T) {
	enteredDelay := make(chan struct{})
	releaseDelay := make(chan struct{})
	var signal sync.Once
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioDelayedResponse,
		func(ctx context.Context) bool {
			signal.Do(func() { close(enteredDelay) })
			select {
			case <-releaseDelay:
				return true
			case <-ctx.Done():
				return false
			}
		},
	)
	connection := dialRaw(t, origin.endpoints[0].address)
	request := rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
		syntheticBodyText, len(syntheticBodyText))
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-enteredDelay:
	case <-time.After(time.Second):
		t.Fatal("delayed origin did not enter controlled delay")
	}
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	close(releaseDelay)
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = io.ReadAll(connection)
	_ = connection.Close()
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, ok := newScenarioResult(ScenarioDelayedResponse, observation)
	if !ok || row.Assessment == AssessmentNoUnsafeBehaviorObserved ||
		observation.CaptureComplete || !containsFinding(row.Findings, FindingCaptureIncomplete) {
		t.Fatalf("same-connection pipeline row = %#v, ok=%v", row, ok)
	}
}

func TestDelayedSameConnectionPipelineDuringBlockedWriteIsNeverPositive(t *testing.T) {
	origin, client, cancel := startBlockedResponseWrite(t, ScenarioDelayedResponse)
	defer cancel()
	if _, err := client.Write([]byte{'P'}); err != nil {
		t.Fatal(err)
	}
	readBlockedResponse(t, client)
	waitForOriginHandlers(t, origin)
	observation := origin.snapshot()
	row, ok := newScenarioResult(ScenarioDelayedResponse, observation)
	if !ok || row.Assessment == AssessmentNoUnsafeBehaviorObserved || observation.CaptureComplete ||
		!containsFinding(row.Findings, FindingCaptureIncomplete) {
		t.Fatalf("blocked-write pipeline row = %#v, ok=%v", row, ok)
	}
}

func TestDelayedSameConnectionPipelineDuringFinishWindowIsObserved(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	var monitors sync.WaitGroup
	monitor := startTrailingMonitor(server, time.Now().Add(time.Second), 1, &monitors)
	injected := make(chan error, 1)
	go func() {
		timer := time.NewTimer(time.Millisecond)
		<-timer.C
		_, err := client.Write([]byte{'P'})
		injected <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	trailing, complete, closeVerified := monitor.finish(ctx, server, trailingProbeTimeout)
	if err := <-injected; err != nil {
		t.Fatal(err)
	}
	monitors.Wait()
	if !trailing || !complete || !closeVerified {
		t.Fatalf("finish-window pipeline = trailing:%v complete:%v close:%v",
			trailing, complete, closeVerified)
	}
}

func TestConcurrentPartialAttemptsDoNotFabricateCausalFindings(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioAcceptThenDisconnect, func(context.Context) bool { return true },
	)
	first := dialRaw(t, origin.endpoints[0].address)
	second := dialRaw(t, origin.endpoints[0].address)
	partial := rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
		"{", len(syntheticBodyText))
	if _, err := io.WriteString(first, partial); err != nil {
		t.Fatal(err)
	}
	waitForAttemptCount(t, origin, 1)
	if _, err := io.WriteString(second, partial); err != nil {
		t.Fatal(err)
	}
	waitForAttemptCount(t, origin, 2)
	_ = first.Close()
	_ = second.Close()
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, ok := newScenarioResult(ScenarioAcceptThenDisconnect, observation)
	if !ok || row.Assessment != AssessmentInconclusive || observation.EffectCount != 0 ||
		observation.RetryAfterEffectCount != 0 || observation.RetryBeforeResponseCount != 0 ||
		containsFinding(row.Findings, FindingRetryAfterAcceptedRequest) ||
		containsFinding(row.Findings, FindingRetryBeforeResponse) {
		t.Fatalf("partial concurrency row = %#v, ok=%v", row, ok)
	}
}

func TestChangedBodyScenarioDistinguishesUnchangedChangedAndTruncatedReplay(t *testing.T) {
	tests := []struct {
		name           string
		replayBody     string
		declaredLength int
		complete       bool
		bodyConsistent bool
		effects        uint64
	}{
		{name: "unchanged", replayBody: syntheticBodyText, declaredLength: len(syntheticBodyText),
			complete: true, bodyConsistent: true, effects: 2},
		{name: "changed", replayBody: changedSyntheticBodyText, declaredLength: len(changedSyntheticBodyText),
			complete: true, bodyConsistent: false, effects: 2},
		{name: "truncated", replayBody: "{", declaredLength: len(changedSyntheticBodyText),
			bodyConsistent: true, effects: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin, caseContext, cancelCase := startRawOrigin(
				t, ScenarioChangedBodyRetry, func(context.Context) bool { return true },
			)
			exchangeRawRequest(
				t, origin.endpoints[0].address,
				rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
					syntheticBodyText, len(syntheticBodyText)),
			)
			replay := rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
				test.replayBody, test.declaredLength)
			if test.complete {
				exchangeRawRequest(t, origin.endpoints[0].address, replay)
			} else {
				sendRawAndClose(t, origin.endpoints[0].address, replay)
				waitForHeadersObserved(t, origin, 2)
			}
			observation := finishRawOrigin(t, origin, caseContext, cancelCase)
			row, ok := newScenarioResult(ScenarioChangedBodyRetry, observation)
			if !ok || row.Assessment != AssessmentUnsafeBehaviorObserved ||
				observation.BodyConsistent != test.bodyConsistent || observation.EffectCount != test.effects ||
				observation.RetryAfterEffectCount != 1 ||
				!containsFinding(row.Findings, FindingRetryAfterAcceptedRequest) {
				t.Fatalf("replay row = %#v, ok=%v", row, ok)
			}
			if test.name == "changed" && !containsFinding(row.Findings, FindingBodyChanged) {
				t.Fatal("complete changed replay did not preserve body mismatch")
			}
			if test.name == "truncated" &&
				(observation.CaptureComplete || !containsFinding(row.Findings, FindingCaptureIncomplete)) {
				t.Fatal("truncated replay did not fail capture closed")
			}
		})
	}
}

func TestDelayedFailedResponseEndsBeforeLaterRetry(t *testing.T) {
	origin := &scenarioOrigin{
		scenario: ScenarioDelayedResponse, attempts: []capturedAttempt{{complete: true}},
		connections: make(map[net.Conn]bool), active: 1, effectCount: 1, pendingResponse: 1,
	}
	server, client := net.Pipe()
	_ = client.Close()
	origin.writeDelayedResponse(server, 0, []byte(noContentResponseText))
	_ = server.Close()
	if origin.responseTry != 1 || origin.responseDone != 0 || origin.pendingResponse != 0 ||
		origin.active != 0 {
		t.Fatalf("failed response phase = %#v", origin)
	}
	retryServer, retryClient := net.Pipe()
	_, admitted := origin.admit(retryServer, sourceEndpoint)
	if !admitted || origin.retryAfterEffect != 1 || origin.retryBeforeResponse != 0 {
		t.Fatalf("later retry counters = after:%d before:%d", origin.retryAfterEffect, origin.retryBeforeResponse)
	}
	_ = retryClient.Close()
	_ = retryServer.Close()
	origin.mu.Lock()
	delete(origin.connections, retryServer)
	if origin.active != 0 {
		origin.active--
	}
	origin.mu.Unlock()
	origin.wait.Done()
}

func TestAbsoluteDeadlineAndWireBudgetCannotPass(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	var monitors sync.WaitGroup
	absolute := time.Now().Add(15 * time.Millisecond)
	monitor := startTrailingMonitor(server, absolute, 1, &monitors)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	trailing, complete, closeVerified := monitor.finish(ctx, server, 50*time.Millisecond)
	monitors.Wait()
	if trailing || complete || !closeVerified || time.Since(started) >= 50*time.Millisecond {
		t.Fatalf("absolute monitor = trailing:%v complete:%v close:%v elapsed:%v",
			trailing, complete, closeVerified, time.Since(started))
	}
	zeroBudget := startTrailingMonitor(server, time.Now().Add(time.Second), 0, &monitors)
	trailing, complete, closeVerified = zeroBudget.finish(ctx, server, 0)
	if trailing || complete || !closeVerified {
		t.Fatalf("zero-budget monitor = %v/%v/%v", trailing, complete, closeVerified)
	}
}

func TestDelayedCancellationJoinsMonitorWithVerifiedCleanup(t *testing.T) {
	entered := make(chan struct{})
	var signal sync.Once
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioDelayedResponse,
		func(ctx context.Context) bool {
			signal.Do(func() { close(entered) })
			<-ctx.Done()
			return false
		},
	)
	connection := dialRaw(t, origin.endpoints[0].address)
	if _, err := io.WriteString(connection, rawRequest(
		origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
		syntheticBodyText, len(syntheticBodyText),
	)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("delay was not entered")
	}
	cancelCase()
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	settled := origin.close(cleanupContext)
	cancelCleanup()
	_ = connection.Close()
	observation := origin.snapshot()
	row, ok := newScenarioResult(ScenarioDelayedResponse, observation)
	if !settled || !ok || observation.CaptureComplete || observation.Cleanup != CleanupSucceeded ||
		row.Assessment != AssessmentInconclusive || caseContext.Err() == nil {
		t.Fatalf("cancelled delayed row = %#v, settled=%v ok=%v", row, settled, ok)
	}
}

func TestMonitorDeadlineAndCloseDoubleFailureIsBoundedAndUnverified(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	connection := &stubbornMonitorConn{Conn: server}
	var monitors sync.WaitGroup
	monitor := startTrailingMonitor(
		connection, time.Now().Add(time.Second), 1, &monitors,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	trailing, complete, closeVerified := monitor.finish(ctx, connection, 0)
	if trailing || complete || closeVerified || time.Since(started) >= 200*time.Millisecond {
		t.Fatalf("double failure = %v/%v/%v after %v",
			trailing, complete, closeVerified, time.Since(started))
	}
	_ = connection.Conn.Close()
	done := make(chan struct{})
	go func() {
		monitors.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not leave tracked quiescence after underlying release")
	}
	origin := &scenarioOrigin{connections: make(map[net.Conn]bool)}
	origin.markCloseFailed()
	if origin.snapshot().Cleanup != CleanupFailed {
		t.Fatal("unverified forced close did not classify cleanup failure")
	}
}

func TestUnexpectedListenerCloseIsCaptureFailureNotCleanupFailure(t *testing.T) {
	origin, caseContext, cancelCase := startRawOrigin(
		t, ScenarioRetryLimit, func(context.Context) bool { return true },
	)
	exchangeRawRequest(
		t, origin.endpoints[0].address,
		rawRequest(origin.endpoints[0].address, "HTTP/1.1", []string{syntheticCredential},
			syntheticBodyText, len(syntheticBodyText)),
	)
	if err := origin.endpoints[0].listener.Close(); err != nil {
		t.Fatal(err)
	}
	waitForServeFailure(t, origin)
	observation := finishRawOrigin(t, origin, caseContext, cancelCase)
	row, ok := newScenarioResult(ScenarioRetryLimit, observation)
	if !ok || observation.CaptureComplete || observation.Cleanup != CleanupSucceeded ||
		row.Assessment != AssessmentInconclusive || !containsFinding(row.Findings, FindingCaptureIncomplete) {
		t.Fatalf("unexpected listener close row = %#v, ok=%v", row, ok)
	}
}

func TestResultsAndFixedErrorsDoNotLeakCallerMarkers(t *testing.T) {
	markers := []string{
		sanitationSuppliedErrorMarker,
		sanitationPanicMarker,
		sanitationResponseFieldMarker,
		sanitationResponseBodyMarker,
		sanitationResponseHeaderMarker,
		sanitationRequestURLMarker,
		sanitationRequestBodyMarker,
		sanitationRequestHeaderMarker,
		sanitationConcreteTypeMarker,
	}
	for _, mode := range []sanitationMode{
		sanitationErrorMode, sanitationPanicMode, sanitationFieldsMode,
	} {
		result, err := Run(context.Background(), &brpSanitationConcreteClientMarker{mode: mode})
		if err != nil || Validate(result) != nil {
			t.Fatalf("marker mode %d returned %#v/%v", mode, result, err)
		}
		assertRepresentationsExcludeMarkers(t, markers, result, err)
	}

	client := &brpSanitationConcreteClientMarker{mode: sanitationErrorMode}
	invalidResult, invalidErr := Run(nil, client)
	if invalidErr != ErrInvalidCall || !reflect.DeepEqual(invalidResult, Result{}) {
		t.Fatalf("invalid marker call = %#v/%v", invalidResult, invalidErr)
	}
	assertRepresentationsExcludeMarkers(t, markers, invalidResult, invalidErr)

	unavailableDependency := dependencies{
		listen: func(context.Context, string, string) (net.Listener, error) {
			return nil, sanitationMarkerError(sanitationSuppliedErrorMarker)
		},
		caseTimeout: time.Second, connectionTimeout: time.Second, cleanupTimeout: time.Second,
		delay: func(context.Context) bool { return true },
	}
	unavailableResult, unavailableErr := run(context.Background(), client, unavailableDependency)
	if unavailableErr != ErrSuiteUnavailable || !reflect.DeepEqual(unavailableResult, Result{}) {
		t.Fatalf("unavailable marker call = %#v/%v", unavailableResult, unavailableErr)
	}
	assertRepresentationsExcludeMarkers(t, markers, unavailableResult, unavailableErr)

	internalTracker := newSanitationAdmissionTracker()
	internalDependency := unavailableDependency
	internalDependency.listen = internalTracker.listen
	internalResult, internalErr := run(context.Background(), client, internalDependency)
	if internalErr != ErrInternalFailure || !reflect.DeepEqual(internalResult, Result{}) {
		t.Fatalf("internal marker call = %#v/%v", internalResult, internalErr)
	}
	assertRepresentationsExcludeMarkers(t, markers, internalResult, internalErr)

	forged := canonicalPositiveResult()
	forged.Assessment = Assessment(sanitationResponseFieldMarker)
	invalidResultErr := Validate(forged)
	if invalidResultErr != ErrInvalidResult {
		t.Fatalf("forged marker error = %v", invalidResultErr)
	}
	assertRepresentationsExcludeMarkers(t, markers, invalidResultErr)
	for _, fixed := range []RunError{
		0, ErrInvalidCall, ErrSuiteUnavailable, ErrInternalFailure, ErrInvalidResult, 255,
	} {
		assertRepresentationsExcludeMarkers(t, markers, fixed)
	}
}

func assertRepresentationsExcludeMarkers(t *testing.T, markers []string, values ...any) {
	t.Helper()
	for _, value := range values {
		representations := []string{
			fmt.Sprint(value), fmt.Sprintf("%v", value), fmt.Sprintf("%#v", value),
		}
		if encoded, err := json.Marshal(value); err == nil {
			representations = append(representations, string(encoded))
		}
		for _, representation := range representations {
			for _, marker := range markers {
				if strings.Contains(representation, marker) {
					t.Fatalf("representation exposed marker %q: %q", marker, representation)
				}
			}
		}
	}
}

func newPhaseTestOrigin(scenario ScenarioID) (*scenarioOrigin, *scenarioEndpoint) {
	source := &scenarioEndpoint{
		address: netip.MustParseAddrPort("127.0.0.1:41001"), role: sourceEndpoint,
	}
	endpoints := []*scenarioEndpoint{source}
	if scenario == ScenarioCrossOriginRedirectCredentials {
		endpoints = append(endpoints, &scenarioEndpoint{
			address: netip.MustParseAddrPort("127.0.0.1:41002"), role: redirectEndpoint,
		})
	}
	return &scenarioOrigin{
		scenario: scenario, endpoints: endpoints, connectionTimeout: time.Second,
		delay:       func(context.Context) bool { return true },
		connections: make(map[net.Conn]bool),
		attempts:    make([]capturedAttempt, 0, maxObservedAttempts),
	}, source
}

func startBlockedResponseWrite(
	t *testing.T,
	scenario ScenarioID,
) (*scenarioOrigin, net.Conn, context.CancelFunc) {
	t.Helper()
	origin, endpoint := newPhaseTestOrigin(scenario)
	server, client := net.Pipe()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	index, admitted := origin.admit(server, endpoint.role)
	if !admitted {
		t.Fatal("phase connection was not admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	go origin.observe(ctx, server, endpoint, index)
	if _, err := io.WriteString(client, rawRequest(
		endpoint.address, "HTTP/1.1", []string{syntheticCredential},
		syntheticBodyText, len(syntheticBodyText),
	)); err != nil {
		cancel()
		t.Fatal(err)
	}
	waitForResponseAttemptCount(t, origin, 1)
	return origin, client, cancel
}

func readBlockedResponse(t *testing.T, client net.Conn) {
	t.Helper()
	if _, err := io.ReadAll(client); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
}

func releaseUnobservedAdmission(origin *scenarioOrigin, server, client net.Conn) {
	_ = client.Close()
	_ = server.Close()
	origin.mu.Lock()
	delete(origin.connections, server)
	if origin.active != 0 {
		origin.active--
	}
	origin.captureMissing = true
	origin.mu.Unlock()
	origin.wait.Done()
}

func waitForResponseAttemptCount(t *testing.T, origin *scenarioOrigin, count uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		origin.mu.Lock()
		observed := origin.responseTry
		origin.mu.Unlock()
		if observed >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("response attempt count did not reach %d", count)
}

func waitForOriginHandlers(t *testing.T, origin *scenarioOrigin) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		origin.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("origin handlers did not quiesce")
	}
}

func zeroRemainingChunkedRequest(address netip.AddrPort) string {
	const payloadSize = 1048563
	var request strings.Builder
	request.Grow(maxRequestHeaderSize + maxRequestBodySize + 1)
	request.WriteString("POST /case HTTP/1.1\r\nHost: ")
	request.WriteString(address.String())
	request.WriteString("\r\nAuthorization: ")
	request.WriteString(syntheticCredential)
	request.WriteString("\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n")
	request.WriteString("ffff3\r\n")
	request.WriteString(strings.Repeat("x", payloadSize))
	request.WriteString("\r\n0\r\n\r\n")
	return request.String()
}

type memoryConn struct {
	reader *strings.Reader
}

func newMemoryConn(request string) *memoryConn {
	return &memoryConn{reader: strings.NewReader(request)}
}

func (connection *memoryConn) Read(value []byte) (int, error) {
	return connection.reader.Read(value)
}

func (*memoryConn) Write(value []byte) (int, error)  { return len(value), nil }
func (*memoryConn) Close() error                     { return nil }
func (*memoryConn) LocalAddr() net.Addr              { return fixedTestAddr("local") }
func (*memoryConn) RemoteAddr() net.Addr             { return fixedTestAddr("remote") }
func (*memoryConn) SetDeadline(time.Time) error      { return nil }
func (*memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (*memoryConn) SetWriteDeadline(time.Time) error { return nil }

type fixedTestAddr string

func (fixedTestAddr) Network() string { return "tcp4" }
func (address fixedTestAddr) String() string {
	return string(address)
}

func startRawOrigin(
	t *testing.T,
	scenario ScenarioID,
	delay func(context.Context) bool,
) (*scenarioOrigin, context.Context, context.CancelFunc) {
	t.Helper()
	count := 1
	if scenario == ScenarioCrossOriginRedirectCredentials {
		count = 2
	}
	listeners := make([]net.Listener, 0, count)
	for range count {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
	}
	caseContext, cancelCase := context.WithTimeout(context.Background(), 2*time.Second)
	origin, ok := newScenarioOrigin(scenario, listeners, time.Second, delay, cancelCase)
	if !ok {
		cancelCase()
		for _, listener := range listeners {
			_ = listener.Close()
		}
		t.Fatal("construct raw origin")
	}
	origin.start(caseContext)
	return origin, caseContext, cancelCase
}

func finishRawOrigin(
	t *testing.T,
	origin *scenarioOrigin,
	caseContext context.Context,
	cancelCase context.CancelFunc,
) Observation {
	t.Helper()
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	settled := origin.close(cleanupContext)
	cancelCleanup()
	caseErr := caseContext.Err()
	cancelCase()
	if !settled || caseErr != nil {
		t.Fatalf("raw origin did not quiesce: settled=%v case=%v", settled, caseErr)
	}
	return origin.snapshot()
}

func finishRawOriginAllowEnded(
	t *testing.T,
	origin *scenarioOrigin,
	cancelCase context.CancelFunc,
) Observation {
	t.Helper()
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	settled := origin.close(cleanupContext)
	cancelCleanup()
	cancelCase()
	if !settled {
		t.Fatal("ended raw origin did not quiesce")
	}
	return origin.snapshot()
}

func rawRequest(
	address netip.AddrPort,
	protocol string,
	authorization []string,
	body string,
	declaredLength int,
) string {
	var request strings.Builder
	request.WriteString("POST /case ")
	request.WriteString(protocol)
	request.WriteString("\r\nHost: ")
	request.WriteString(address.String())
	request.WriteString("\r\n")
	for _, value := range authorization {
		request.WriteString("Authorization: ")
		request.WriteString(value)
		request.WriteString("\r\n")
	}
	request.WriteString("Content-Length: ")
	request.WriteString(decimalString(declaredLength))
	request.WriteString("\r\nConnection: close\r\n\r\n")
	request.WriteString(body)
	return request.String()
}

func decimalString(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value != 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func dialRaw(t *testing.T, address netip.AddrPort) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp4", address.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func exchangeRawRequest(t *testing.T, address netip.AddrPort, request string) string {
	t.Helper()
	connection := dialRaw(t, address)
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		t.Fatal(err)
	}
	return string(response)
}

func sendRawAndClose(t *testing.T, address netip.AddrPort, request string) {
	t.Helper()
	connection := dialRaw(t, address)
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
}

func sendRawBestEffort(t *testing.T, address netip.AddrPort, request string) {
	t.Helper()
	connection := dialRaw(t, address)
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(connection, request)
	_ = connection.Close()
}

func waitForAttemptCount(t *testing.T, origin *scenarioOrigin, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		origin.mu.Lock()
		observed := len(origin.attempts)
		origin.mu.Unlock()
		if observed >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("attempt count did not reach %d", count)
}

func waitForHeadersObserved(t *testing.T, origin *scenarioOrigin, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		origin.mu.Lock()
		observed := len(origin.attempts) >= count && origin.attempts[count-1].headersObserved
		origin.mu.Unlock()
		if observed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("attempt %d headers were not retained", count)
}

func waitForCaptureMissing(t *testing.T, origin *scenarioOrigin) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		origin.mu.Lock()
		missing := origin.captureMissing
		origin.mu.Unlock()
		if missing {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("capture did not become incomplete")
}

func waitForServeFailure(t *testing.T, origin *scenarioOrigin) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		origin.mu.Lock()
		failed := origin.serveFailed
		origin.mu.Unlock()
		if failed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("unexpected listener close was not recorded")
}

func containsFinding(findings []FindingCode, wanted FindingCode) bool {
	for _, finding := range findings {
		if finding == wanted {
			return true
		}
	}
	return false
}

func cloneInternalResult(source Result) Result {
	cloned := Result{Assessment: source.Assessment, Scenarios: make([]ScenarioResult, len(source.Scenarios))}
	copy(cloned.Scenarios, source.Scenarios)
	for index := range cloned.Scenarios {
		cloned.Scenarios[index].Findings = make([]FindingCode, len(source.Scenarios[index].Findings))
		copy(cloned.Scenarios[index].Findings, source.Scenarios[index].Findings)
	}
	return cloned
}

type responseCloseMode uint8

const (
	responseCloseFailure responseCloseMode = iota + 1
	responseClosePanic
)

type faultResponseBody struct {
	mode responseCloseMode
}

func (*faultResponseBody) Read([]byte) (int, error) { return 0, io.EOF }

func (body *faultResponseBody) Close() error {
	if body.mode == responseClosePanic {
		panic(testFailure(1))
	}
	return testFailure(1)
}

type panicOnSecondCloseBody struct {
	closeCalls atomic.Uint32
}

func (*panicOnSecondCloseBody) Read([]byte) (int, error) { return 0, io.EOF }

func (body *panicOnSecondCloseBody) Close() error {
	if body.closeCalls.Add(1) > 1 {
		panic(testFailure(1))
	}
	return nil
}

type typedNilDoer struct{}

func (doer *typedNilDoer) Do(request *http.Request) (*http.Response, error) {
	_ = request.Body.Close()
	if doer == nil {
		panic(testFailure(1))
	}
	return nil, testFailure(1)
}

type stubbornMonitorConn struct {
	net.Conn
	readDeadlines atomic.Uint32
}

func (connection *stubbornMonitorConn) SetReadDeadline(time.Time) error {
	if connection.readDeadlines.Add(1) == 1 {
		return nil
	}
	return testFailure(1)
}

func (*stubbornMonitorConn) Close() error { return testFailure(1) }

func observationForTest(
	attempts uint32,
	effects uint64,
	responseAttempts uint32,
	responseComplete uint32,
	firstResponse bool,
	credential CredentialState,
) Observation {
	return Observation{
		CaptureComplete: true, AttemptCount: attempts, EffectCount: effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: firstResponse, MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true, Credential: credential,
		Cleanup: CleanupSucceeded,
	}
}

func canonicalPositiveResult() Result {
	observations := map[ScenarioID]Observation{
		ScenarioAcceptThenDisconnect:       observationForTest(1, 1, 0, 0, false, CredentialSourceOnly),
		ScenarioDisconnectBeforeAcceptance: observationForTest(1, 0, 0, 0, false, CredentialSourceOnly),
		ScenarioChangedBodyRetry:           observationForTest(1, 1, 0, 0, false, CredentialSourceOnly),
		ScenarioCrossOriginRedirectCredentials: observationForTest(
			1, 0, 1, 1, true, CredentialSourceOnly,
		),
		ScenarioRetryLimit: observationForTest(1, 0, 1, 1, true, CredentialSourceOnly),
		ScenarioDelayedResponse: func() Observation {
			value := observationForTest(1, 1, 1, 1, true, CredentialSourceOnly)
			value.DelayCompleteCount = 1
			return value
		}(),
	}
	rows := make([]ScenarioResult, 0, len(orderedScenarios()))
	for _, scenario := range orderedScenarios() {
		row, ok := newScenarioResult(scenario, observations[scenario])
		if !ok {
			return Result{}
		}
		rows = append(rows, row)
	}
	result, _ := newResult(rows)
	return result
}

func resultReplacing(scenario ScenarioID, replacement ScenarioResult) Result {
	base := canonicalPositiveResult()
	for index := range base.Scenarios {
		if base.Scenarios[index].Scenario == scenario {
			base.Scenarios[index] = replacement
		}
	}
	result, _ := newResult(base.Scenarios)
	return result
}

type listenerTracker struct {
	mu                sync.Mutex
	listenCalls       int
	failAt            int
	closeFailureAt    int
	duplicateSourceAt int
	duplicateAt       int
	listeners         []*trackedListener
}

type sanitationAdmissionTracker struct {
	calls int
}

func newSanitationAdmissionTracker() *sanitationAdmissionTracker {
	return &sanitationAdmissionTracker{}
}

func (tracker *sanitationAdmissionTracker) listen(
	ctx context.Context,
	network string,
	address string,
) (net.Listener, error) {
	tracker.calls++
	if tracker.calls != 1 {
		return nil, sanitationMarkerError(sanitationSuppliedErrorMarker)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, network, address)
	if err != nil {
		return nil, sanitationMarkerError(sanitationSuppliedErrorMarker)
	}
	return &sanitationCloseMarkerListener{Listener: listener}, nil
}

type sanitationCloseMarkerListener struct {
	net.Listener
}

func (listener *sanitationCloseMarkerListener) Close() error {
	_ = listener.Listener.Close()
	return sanitationMarkerError(sanitationSuppliedErrorMarker)
}

type trackedListener struct {
	net.Listener
	closeFailure bool
	closeCalls   atomic.Uint32
}

func newListenerTracker(failAt, closeFailureAt int) *listenerTracker {
	return &listenerTracker{failAt: failAt, closeFailureAt: closeFailureAt}
}

func newDuplicateListenerTracker(sourceAt, duplicateAt int) *listenerTracker {
	return &listenerTracker{duplicateSourceAt: sourceAt, duplicateAt: duplicateAt}
}

func (tracker *listenerTracker) listen(
	ctx context.Context,
	network string,
	address string,
) (net.Listener, error) {
	tracker.mu.Lock()
	tracker.listenCalls++
	call := tracker.listenCalls
	tracker.mu.Unlock()
	if call == tracker.failAt {
		return nil, testFailure(1)
	}
	if call == tracker.duplicateAt {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		if tracker.duplicateSourceAt <= 0 || tracker.duplicateSourceAt > len(tracker.listeners) {
			return nil, testFailure(1)
		}
		return tracker.listeners[tracker.duplicateSourceAt-1], nil
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, network, address)
	if err != nil {
		return nil, testFailure(1)
	}
	tracked := &trackedListener{Listener: listener, closeFailure: call == tracker.closeFailureAt}
	tracker.mu.Lock()
	tracker.listeners = append(tracker.listeners, tracked)
	tracker.mu.Unlock()
	return tracked, nil
}

func (tracker *listenerTracker) callCount() int {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.listenCalls
}

func (tracker *listenerTracker) assertAllClosed(t *testing.T) {
	t.Helper()
	tracker.mu.Lock()
	listeners := append([]*trackedListener(nil), tracker.listeners...)
	tracker.mu.Unlock()
	for index, listener := range listeners {
		if listener.closeCalls.Load() == 0 {
			t.Errorf("listener %d was not closed", index)
			continue
		}
		connection, err := net.DialTimeout("tcp4", listener.Addr().String(), 20*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			t.Errorf("listener %d still accepts connections", index)
		}
	}
}

func (listener *trackedListener) Close() error {
	listener.closeCalls.Add(1)
	err := listener.Listener.Close()
	if listener.closeFailure {
		return testFailure(1)
	}
	return err
}

func trackedDependencies(tracker *listenerTracker) dependencies {
	return dependencies{
		listen: tracker.listen, caseTimeout: 500 * time.Millisecond,
		connectionTimeout: 200 * time.Millisecond, cleanupTimeout: 200 * time.Millisecond,
		delay: func(context.Context) bool { return true },
	}
}
