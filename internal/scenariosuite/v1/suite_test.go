package scenariosuite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	suite "github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1"
)

const rawMarker = "RAW-SCENARIO-SUITE-MARKER-127.0.0.1:49152"

func TestRunSafeControlsProducesPositiveSuite(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()

	result, err := suite.Run(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := suite.Validate(result); err != nil {
		t.Fatal(err)
	}
	if result.Assessment != suite.AssessmentNoUnsafeBehaviorObserved || len(result.Scenarios) != 6 {
		t.Fatalf("suite result = %#v", result)
	}
	want := []suite.Observation{
		positiveObservation(1, 1, 0, 0, false, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 0, 0, 0, false, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 1, 0, 0, false, 0, suite.CredentialSourceOnly),
		positiveObservation(2, 1, 2, 2, true, 0, suite.CredentialAbsentAtTarget),
		positiveObservation(1, 0, 1, 1, true, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 1, 1, 1, true, 1, suite.CredentialSourceOnly),
	}
	for index, row := range result.Scenarios {
		if row.Assessment != suite.AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 ||
			!reflect.DeepEqual(row.Observation, want[index]) {
			t.Fatalf("row %d = %#v, want observation %#v", index, row, want[index])
		}
	}
	assertSanitized(t, result)
}

func TestRunRedirectRefusalSettlesAndContinues(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	started := time.Now()
	result, err := suite.Run(context.Background(), client)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("redirect-refusal suite took %v", elapsed)
	}
	if err := suite.Validate(result); err != nil {
		t.Fatal(err)
	}
	if result.Assessment != suite.AssessmentNoUnsafeBehaviorObserved || len(result.Scenarios) != 6 {
		t.Fatalf("suite result = %#v", result)
	}
	want := []suite.Observation{
		positiveObservation(1, 1, 0, 0, false, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 0, 0, 0, false, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 1, 0, 0, false, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 0, 1, 1, true, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 0, 1, 1, true, 0, suite.CredentialSourceOnly),
		positiveObservation(1, 1, 1, 1, true, 1, suite.CredentialSourceOnly),
	}
	for index, row := range result.Scenarios {
		if row.Assessment != suite.AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 ||
			!reflect.DeepEqual(row.Observation, want[index]) {
			t.Fatalf("row %d = %#v, want observation %#v", index, row, want[index])
		}
	}
	assertSanitized(t, result)
}

func TestRunObservesRetryStartedAfterDoReturns(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	doer := &lateIndependentRetryDoer{
		client: client, delay: 50 * time.Millisecond, done: make(chan error, 1),
	}

	result, err := suite.Run(context.Background(), doer)
	if err != nil || suite.Validate(result) != nil {
		t.Fatalf("late-retry result = %#v/%v", result, err)
	}
	row := result.Scenarios[0]
	if result.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
		row.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
		!row.Observation.CaptureComplete || row.Observation.AttemptCount != 2 ||
		row.Observation.RetryAfterEffectCount != 1 ||
		!containsFinding(row.Findings, suite.FindingRetryAfterAcceptedRequest) {
		t.Fatalf("late-retry row = %#v", row)
	}
	select {
	case <-doer.done:
	default:
		t.Fatal("late retry did not finish during the quiet observation window")
	}
}

func TestRunInvocationErrorsAreScenarioAware(t *testing.T) {
	t.Run("custom redirect error is positive and continues", func(t *testing.T) {
		client, transport := newClient(true)
		defer transport.CloseIdleConnections()
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return errors.New("no redirects")
		}
		doer := &postResponseErrorDoer{client: client, targetCall: 4, attempts: 1}

		result, err := suite.Run(context.Background(), doer)
		if err != nil || suite.Validate(result) != nil {
			t.Fatalf("redirect error result = %#v/%v", result, err)
		}
		row := result.Scenarios[3]
		if result.Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
			row.Assessment != suite.AssessmentNoUnsafeBehaviorObserved || !row.Observation.CaptureComplete ||
			row.Observation.AttemptCount != 1 || row.Observation.ResponseCompleteCount != 1 ||
			len(row.Findings) != 0 || doer.calls.Load() != 6 ||
			result.Scenarios[4].Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
			result.Scenarios[5].Assessment != suite.AssessmentNoUnsafeBehaviorObserved {
			t.Fatalf("redirect error reduction = %#v, calls=%d", result, doer.calls.Load())
		}
		assertSanitized(t, result)
	})

	t.Run("two-response wrapped retry error is positive", func(t *testing.T) {
		client, transport := newClient(true)
		defer transport.CloseIdleConnections()
		doer := &postResponseErrorDoer{client: client, targetCall: 5, attempts: 2}

		result, err := suite.Run(context.Background(), doer)
		if err != nil || suite.Validate(result) != nil {
			t.Fatalf("retry error result = %#v/%v", result, err)
		}
		row := result.Scenarios[4]
		if result.Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
			row.Assessment != suite.AssessmentNoUnsafeBehaviorObserved || !row.Observation.CaptureComplete ||
			row.Observation.AttemptCount != 2 || row.Observation.ResponseCompleteCount != 2 ||
			len(row.Findings) != 0 || doer.calls.Load() != 6 ||
			result.Scenarios[5].Assessment != suite.AssessmentNoUnsafeBehaviorObserved {
			t.Fatalf("retry error reduction = %#v, calls=%d", result, doer.calls.Load())
		}
		assertSanitized(t, result)
	})

	t.Run("zero-attempt retry error remains inconclusive", func(t *testing.T) {
		client, transport := newClient(true)
		defer transport.CloseIdleConnections()
		doer := &postResponseErrorDoer{client: client, targetCall: 5, attempts: 0}

		result, err := suite.Run(context.Background(), doer)
		if err != nil || suite.Validate(result) != nil {
			t.Fatalf("zero-attempt retry result = %#v/%v", result, err)
		}
		row := result.Scenarios[4]
		if result.Assessment != suite.AssessmentInconclusive ||
			row.Assessment != suite.AssessmentInconclusive || row.Observation.CaptureComplete ||
			row.Observation.AttemptCount != 0 ||
			!containsFinding(row.Findings, suite.FindingAttemptNotObserved) ||
			!containsFinding(row.Findings, suite.FindingCaptureIncomplete) ||
			doer.calls.Load() != 5 || result.Scenarios[5].Observation.AttemptCount != 0 {
			t.Fatalf("zero-attempt retry reduction = %#v, calls=%d", result, doer.calls.Load())
		}
		assertSanitized(t, result)
	})

	t.Run("three-response retry error preserves unsafe evidence", func(t *testing.T) {
		client, transport := newClient(true)
		defer transport.CloseIdleConnections()
		doer := &postResponseErrorDoer{client: client, targetCall: 5, attempts: 3}

		result, err := suite.Run(context.Background(), doer)
		if err != nil || suite.Validate(result) != nil {
			t.Fatalf("unsafe retry error result = %#v/%v", result, err)
		}
		row := result.Scenarios[4]
		if result.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
			row.Assessment != suite.AssessmentUnsafeBehaviorObserved || !row.Observation.CaptureComplete ||
			row.Observation.AttemptCount != 3 || row.Observation.ResponseCompleteCount != 3 ||
			containsFinding(row.Findings, suite.FindingCaptureIncomplete) ||
			!containsFinding(row.Findings, suite.FindingAttemptLimitExceeded) ||
			doer.calls.Load() != 6 ||
			result.Scenarios[5].Assessment != suite.AssessmentNoUnsafeBehaviorObserved {
			t.Fatalf("unsafe retry error reduction = %#v, calls=%d", result, doer.calls.Load())
		}
		assertSanitized(t, result)
	})

	t.Run("exact delayed error remains positive", func(t *testing.T) {
		client, transport := newClient(true)
		defer transport.CloseIdleConnections()
		doer := &postResponseErrorDoer{client: client, targetCall: 6, attempts: 1}

		result, err := suite.Run(context.Background(), doer)
		if err != nil || suite.Validate(result) != nil {
			t.Fatalf("delayed error result = %#v/%v", result, err)
		}
		row := result.Scenarios[5]
		if result.Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
			row.Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
			!row.Observation.CaptureComplete || row.Observation.AttemptCount != 1 ||
			row.Observation.DelayCompleteCount != 1 || row.Observation.ResponseAttemptCount != 1 ||
			doer.calls.Load() != 6 {
			t.Fatalf("delayed error reduction = %#v, calls=%d", result, doer.calls.Load())
		}
		assertSanitized(t, result)
	})
}

func TestRunUnsafeControlsFindEveryScenario(t *testing.T) {
	client, transport := newClient(false)
	defer transport.CloseIdleConnections()
	doer := &adversarialDoer{client: client}

	result, err := suite.Run(context.Background(), doer)
	if err != nil {
		t.Fatal(err)
	}
	if err := suite.Validate(result); err != nil {
		t.Fatal(err)
	}
	if result.Assessment != suite.AssessmentUnsafeBehaviorObserved || doer.calls.Load() != 6 {
		t.Fatalf("suite assessment/calls = %s/%d", result.Assessment, doer.calls.Load())
	}
	wantFindings := [][]suite.FindingCode{
		{suite.FindingRetryAfterAcceptedRequest, suite.FindingEffectLimitExceeded},
		{suite.FindingRetryAfterUnconfirmedAcceptance},
		{suite.FindingRetryAfterAcceptedRequest, suite.FindingBodyChanged, suite.FindingEffectLimitExceeded},
		{suite.FindingCredentialExposedAtTarget},
		{suite.FindingAttemptLimitExceeded},
		{suite.FindingCaptureIncomplete, suite.FindingRetryBeforeResponse,
			suite.FindingRetryAfterAcceptedRequest, suite.FindingEffectLimitExceeded},
	}
	for index, row := range result.Scenarios {
		if row.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
			!reflect.DeepEqual(row.Findings, wantFindings[index]) {
			t.Fatalf("unsafe row %d = %#v, want findings %v", index, row, wantFindings[index])
		}
	}
	changed := result.Scenarios[2].Observation
	if changed.AttemptCount != 2 || changed.EffectCount != 2 || changed.BodyConsistent {
		t.Fatalf("changed-body observation = %#v", changed)
	}
	redirect := result.Scenarios[3].Observation
	if redirect.Credential != suite.CredentialExposedAtTarget || redirect.AttemptCount != 2 {
		t.Fatalf("redirect observation = %#v", redirect)
	}
	retry := result.Scenarios[4].Observation
	if retry.AttemptCount != 3 || retry.EffectCount != 0 {
		t.Fatalf("retry-limit observation = %#v", retry)
	}
	delayed := result.Scenarios[5].Observation
	if delayed.AttemptCount != 2 || delayed.EffectCount != 2 || delayed.OverlapCount == 0 || delayed.CaptureComplete {
		t.Fatalf("delayed observation = %#v", delayed)
	}
	assertSanitized(t, result)
}

func TestRunBareConnectionsAfterErrorsDoNotCreateReplayFindings(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	doer := &postErrorConnectionDoer{
		client:      client,
		mode:        bareConnectionProbe,
		probeErrors: make(chan error, 3),
	}

	result, err := suite.Run(context.Background(), doer)
	if err != nil || suite.Validate(result) != nil {
		t.Fatalf("bare-connection result = %#v/%v", result, err)
	}
	if result.Assessment != suite.AssessmentInconclusive || doer.calls.Load() != 6 {
		t.Fatalf("suite assessment/calls = %s/%d", result.Assessment, doer.calls.Load())
	}
	if len(doer.probeErrors) != 3 {
		t.Fatalf("bare probes completed = %d, want 3", len(doer.probeErrors))
	}
	for range 3 {
		if probeErr := <-doer.probeErrors; probeErr != nil {
			t.Fatalf("bare probe failed: %v", probeErr)
		}
	}
	for index := range 3 {
		row := result.Scenarios[index]
		if row.Assessment != suite.AssessmentInconclusive || row.Observation.CaptureComplete ||
			row.Observation.AttemptCount != 2 || row.Observation.RetryAfterEffectCount != 0 ||
			row.Observation.RetryAfterUnconfirmedCount != 0 || row.Observation.RetryBeforeResponseCount != 0 ||
			!reflect.DeepEqual(row.Findings, []suite.FindingCode{suite.FindingCaptureIncomplete}) {
			t.Fatalf("bare-connection row %d = %#v", index, row)
		}
	}
	for index := 3; index < len(result.Scenarios); index++ {
		if result.Scenarios[index].Assessment != suite.AssessmentNoUnsafeBehaviorObserved {
			t.Fatalf("later row %d did not run: %#v", index, result.Scenarios[index])
		}
	}
}

func TestRunFullReplaysAfterErrorsStillCreateReplayFindings(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	doer := &postErrorConnectionDoer{client: client, mode: fullRequestReplay}

	result, err := suite.Run(context.Background(), doer)
	if err != nil || suite.Validate(result) != nil {
		t.Fatalf("full-replay result = %#v/%v", result, err)
	}
	if result.Assessment != suite.AssessmentUnsafeBehaviorObserved || doer.calls.Load() != 6 {
		t.Fatalf("suite assessment/calls = %s/%d", result.Assessment, doer.calls.Load())
	}
	wantRetry := []suite.FindingCode{
		suite.FindingRetryAfterAcceptedRequest,
		suite.FindingRetryAfterUnconfirmedAcceptance,
		suite.FindingRetryAfterAcceptedRequest,
	}
	for index, finding := range wantRetry {
		row := result.Scenarios[index]
		if row.Assessment != suite.AssessmentUnsafeBehaviorObserved || !row.Observation.CaptureComplete ||
			row.Observation.AttemptCount != 2 || !containsFinding(row.Findings, finding) {
			t.Fatalf("full-replay row %d = %#v", index, row)
		}
	}
	if result.Scenarios[0].Observation.RetryAfterEffectCount != 1 ||
		result.Scenarios[1].Observation.RetryAfterUnconfirmedCount != 1 ||
		result.Scenarios[2].Observation.RetryAfterEffectCount != 1 {
		t.Fatalf("full-replay counters = %#v/%#v/%#v",
			result.Scenarios[0].Observation,
			result.Scenarios[1].Observation,
			result.Scenarios[2].Observation)
	}
}

func TestRunDetectsCredentialMarkerInsideRedirectAuthorization(t *testing.T) {
	tests := []struct {
		name        string
		target      func(string) []string
		wantExposed bool
	}{
		{name: "exact", target: func(value string) []string { return []string{value} }, wantExposed: true},
		{
			name: "prefix", target: func(value string) []string { return []string{"forwarded " + value} },
			wantExposed: true,
		},
		{
			name: "suffix", target: func(value string) []string { return []string{value + " transformed"} },
			wantExposed: true,
		},
		{
			name: "unrelated", target: func(string) []string { return []string{"Bearer unrelated-credential"} },
		},
		{
			name: "multiple values", target: func(value string) []string {
				return []string{"Bearer unrelated-credential", value + " transformed"}
			},
			wantExposed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, transport := newClient(false)
			defer transport.CloseIdleConnections()
			client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
				if len(via) != 0 && request.URL.Host != via[0].URL.Host {
					credential := request.Header.Get("Authorization")
					request.Header["Authorization"] = test.target(credential)
				}
				return nil
			}

			result, err := suite.Run(context.Background(), client)
			if err != nil || suite.Validate(result) != nil {
				t.Fatalf("redirect result = %#v/%v", result, err)
			}
			row := result.Scenarios[3]
			if test.wantExposed {
				if result.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
					row.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
					row.Observation.Credential != suite.CredentialExposedAtTarget ||
					!containsFinding(row.Findings, suite.FindingCredentialExposedAtTarget) {
					t.Fatalf("redirect exposure row = %#v", row)
				}
				return
			}
			if result.Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
				row.Assessment != suite.AssessmentNoUnsafeBehaviorObserved ||
				row.Observation.Credential != suite.CredentialAbsentAtTarget ||
				len(row.Findings) != 0 {
				t.Fatalf("unrelated redirect row = %#v", row)
			}
		})
	}
}

func TestRunDetectsCredentialMarkerAnywhereInRedirectHead(t *testing.T) {
	const marker = "http-retry-check-synthetic-scenario-suite-v1"
	tests := []struct {
		name string
		move func(*http.Request)
	}{
		{name: "cookie", move: func(request *http.Request) {
			request.Header.Del("Authorization")
			request.Header.Set("Cookie", "session="+marker)
		}},
		{name: "custom header", move: func(request *http.Request) {
			request.Header.Del("Authorization")
			request.Header.Set("X-Copied-Value", "prefix-"+marker+"-suffix")
		}},
		{name: "raw target", move: func(request *http.Request) {
			request.Header.Del("Authorization")
			request.URL.RawQuery = "copied=" + marker
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, transport := newClient(false)
			defer transport.CloseIdleConnections()
			client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
				if len(via) != 0 && request.URL.Host != via[0].URL.Host {
					test.move(request)
				}
				return nil
			}

			result, err := suite.Run(context.Background(), client)
			if err != nil || suite.Validate(result) != nil {
				t.Fatalf("redirect result = %#v/%v", result, err)
			}
			row := result.Scenarios[3]
			if result.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
				row.Assessment != suite.AssessmentUnsafeBehaviorObserved ||
				row.Observation.Credential != suite.CredentialExposedAtTarget ||
				!containsFinding(row.Findings, suite.FindingCredentialExposedAtTarget) {
				t.Fatalf("redirect exposure row = %#v", row)
			}
		})
	}
}

func TestValidateRejectsForgedAssessmentFindingsAndTuples(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	valid, err := suite.Run(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*suite.Result)
	}{
		{name: "aggregate", edit: func(result *suite.Result) { result.Assessment = suite.AssessmentInconclusive }},
		{name: "row assessment", edit: func(result *suite.Result) {
			result.Scenarios[0].Assessment = suite.AssessmentUnsafeBehaviorObserved
		}},
		{name: "nil findings", edit: func(result *suite.Result) { result.Scenarios[0].Findings = nil }},
		{name: "invented finding", edit: func(result *suite.Result) {
			result.Scenarios[0].Findings = []suite.FindingCode{suite.FindingBodyChanged}
		}},
		{name: "redirect source-only with target attempt", edit: func(result *suite.Result) {
			result.Scenarios[3].Observation.Credential = suite.CredentialSourceOnly
		}},
		{name: "later response without first", edit: func(result *suite.Result) {
			result.Scenarios[3].Observation.FirstResponseComplete = false
		}},
		{name: "redirect source commits effect", edit: func(result *suite.Result) {
			row := &result.Scenarios[3]
			row.Observation.AttemptCount = 1
			row.Observation.EffectCount = 1
			row.Observation.ResponseAttemptCount = 1
			row.Observation.ResponseCompleteCount = 1
			row.Observation.FirstResponseComplete = true
			row.Observation.Credential = suite.CredentialSourceOnly
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			forged := cloneResult(valid)
			test.edit(&forged)
			if err := suite.Validate(forged); err != suite.ErrInvalidResult {
				t.Fatalf("Validate error = %v, want %v", err, suite.ErrInvalidResult)
			}
		})
	}
}

func TestRunRejectsInvalidCallsWithZeroResult(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	tests := []struct {
		name string
		ctx  context.Context
		doer suite.Doer
	}{
		{name: "nil context", doer: client},
		{name: "nil doer", ctx: context.Background()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := suite.Run(test.ctx, test.doer)
			if err != suite.ErrInvalidCall || !reflect.DeepEqual(result, suite.Result{}) {
				t.Fatalf("Run = %#v/%v", result, err)
			}
		})
	}
}

func TestRunReturnsPreexistingParentCancellationWithZeroResult(t *testing.T) {
	client, transport := newClient(true)
	defer transport.CloseIdleConnections()
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := suite.Run(parent, client)
	if err != parent.Err() || !reflect.DeepEqual(result, suite.Result{}) {
		t.Fatalf("Run = %#v/%v, want zero result/%v", result, err, parent.Err())
	}
}

func TestRunIsRepeatableAndConcurrent(t *testing.T) {
	const callers = 4
	results := make([]suite.Result, callers)
	errorsObserved := make([]error, callers)
	var wait sync.WaitGroup
	for index := range callers {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			client, transport := newClient(true)
			defer transport.CloseIdleConnections()
			results[index], errorsObserved[index] = suite.Run(context.Background(), client)
		}(index)
	}
	wait.Wait()
	for index := range callers {
		if errorsObserved[index] != nil || suite.Validate(results[index]) != nil ||
			!reflect.DeepEqual(results[index], results[0]) {
			t.Fatalf("concurrent result %d = %#v/%v", index, results[index], errorsObserved[index])
		}
	}
}

func positiveObservation(
	attempts uint32,
	effects uint64,
	responseAttempts uint32,
	responseComplete uint32,
	firstResponse bool,
	delayComplete uint32,
	credential suite.CredentialState,
) suite.Observation {
	return suite.Observation{
		CaptureComplete: true, AttemptCount: attempts, AttemptLimit: 2, Protocol: "HTTP/1.1",
		EffectCount:          effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: firstResponse, DelayCompleteCount: delayComplete,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credential, Cleanup: suite.CleanupSucceeded,
	}
}

func newClient(stripCrossOriginCredential bool) (*http.Client, *http.Transport) {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy: nil, Protocols: protocols, DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport}
	if stripCrossOriginCredential {
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		}
	}
	return client, transport
}

type adversarialDoer struct {
	client *http.Client
	calls  atomic.Uint32
}

type postResponseErrorDoer struct {
	client     *http.Client
	targetCall uint32
	attempts   int
	calls      atomic.Uint32
}

type connectionProbeMode uint8

const (
	bareConnectionProbe connectionProbeMode = iota + 1
	fullRequestReplay
)

type postErrorConnectionDoer struct {
	client      *http.Client
	mode        connectionProbeMode
	calls       atomic.Uint32
	probeErrors chan error
}

type lateIndependentRetryDoer struct {
	client *http.Client
	delay  time.Duration
	done   chan error
	calls  atomic.Uint32
}

func (doer *lateIndependentRetryDoer) Do(request *http.Request) (*http.Response, error) {
	if doer.calls.Add(1) != 1 {
		return doer.client.Do(request)
	}
	replayBody, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(replayBody)
	closeErr := replayBody.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	method := request.Method
	target := request.URL.String()
	header := request.Header.Clone()
	contentLength := request.ContentLength

	response, invocationErr := doer.client.Do(request)
	go func() {
		timer := time.NewTimer(doer.delay)
		defer timer.Stop()
		<-timer.C
		retry, requestErr := http.NewRequestWithContext(
			context.Background(), method, target, bytes.NewReader(body),
		)
		if requestErr != nil {
			doer.done <- requestErr
			return
		}
		retry.Header = header
		retry.ContentLength = contentLength
		lateResponse, lateErr := doer.client.Do(retry)
		closeResponse(lateResponse)
		doer.done <- lateErr
	}()
	return response, invocationErr
}

func (doer *postErrorConnectionDoer) Do(request *http.Request) (*http.Response, error) {
	call := doer.calls.Add(1)
	if call > 3 {
		return doer.client.Do(request)
	}
	response, firstErr := doer.client.Do(request)
	closeResponse(response)
	if firstErr == nil {
		return nil, errors.New(rawMarker)
	}
	if doer.mode == fullRequestReplay {
		replay, err := replayRequest(request)
		if err != nil {
			return nil, err
		}
		return doer.client.Do(replay)
	}
	connection, probeErr := net.DialTimeout("tcp4", request.URL.Host, time.Second)
	if probeErr == nil {
		probeErr = connection.SetDeadline(time.Now().Add(time.Second))
		if tcpConnection, ok := connection.(*net.TCPConn); probeErr == nil && ok {
			probeErr = tcpConnection.CloseWrite()
		} else if probeErr == nil {
			probeErr = errors.New(rawMarker)
		}
		if probeErr == nil {
			_, probeErr = io.Copy(io.Discard, connection)
		}
		if closeErr := connection.Close(); probeErr == nil {
			probeErr = closeErr
		}
	}
	if doer.probeErrors != nil {
		doer.probeErrors <- probeErr
	}
	return nil, firstErr
}

func (doer *postResponseErrorDoer) Do(request *http.Request) (*http.Response, error) {
	call := doer.calls.Add(1)
	if call != doer.targetCall {
		return doer.client.Do(request)
	}
	if doer.attempts == 0 {
		if err := request.Body.Close(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("retry policy rejected request: %w", errors.New(rawMarker))
	}
	response, err := repeatRequest(doer.client, request, doer.attempts)
	if err != nil {
		return response, err
	}
	closeResponse(response)
	return nil, fmt.Errorf("retry policy exhausted: %w", errors.New(rawMarker))
}

func (doer *adversarialDoer) Do(request *http.Request) (*http.Response, error) {
	call := doer.calls.Add(1)
	switch call {
	case 1, 2, 3:
		return retryAfterError(doer.client, request)
	case 4:
		return doer.client.Do(request)
	case 5:
		return repeatRequest(doer.client, request, 3)
	case 6:
		return overlapRequest(doer.client, request)
	default:
		return nil, errors.New(rawMarker)
	}
}

func retryAfterError(client *http.Client, request *http.Request) (*http.Response, error) {
	first, firstErr := client.Do(request)
	closeResponse(first)
	if firstErr == nil {
		return nil, errors.New(rawMarker)
	}
	replay, err := replayRequest(request)
	if err != nil {
		return nil, err
	}
	return client.Do(replay)
}

func repeatRequest(client *http.Client, request *http.Request, count int) (*http.Response, error) {
	current := request
	for index := 0; index < count; index++ {
		response, err := client.Do(current)
		if index == count-1 || err != nil {
			return response, err
		}
		closeResponse(response)
		current, err = replayRequest(request)
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New(rawMarker)
}

type responseResult struct {
	response *http.Response
	err      error
}

func overlapRequest(client *http.Client, request *http.Request) (*http.Response, error) {
	firstResult := make(chan responseResult, 1)
	go func() {
		response, err := client.Do(request)
		firstResult <- responseResult{response: response, err: err}
	}()
	timer := time.NewTimer(80 * time.Millisecond)
	<-timer.C
	replay, err := replayRequest(request)
	if err != nil {
		first := <-firstResult
		closeResponse(first.response)
		return nil, err
	}
	secondResult := make(chan responseResult, 1)
	go func() {
		response, requestErr := client.Do(replay)
		secondResult <- responseResult{response: response, err: requestErr}
	}()
	first := <-firstResult
	second := <-secondResult
	closeResponse(first.response)
	if second.err != nil {
		closeResponse(second.response)
		return nil, second.err
	}
	return second.response, nil
}

func replayRequest(request *http.Request) (*http.Request, error) {
	if request.GetBody == nil {
		return nil, errors.New(rawMarker)
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	replay := request.Clone(request.Context())
	replay.Body = body
	replay.GetBody = request.GetBody
	replay.ContentLength = request.ContentLength
	return replay, nil
}

func closeResponse(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

func containsFinding(findings []suite.FindingCode, expected suite.FindingCode) bool {
	for _, finding := range findings {
		if finding == expected {
			return true
		}
	}
	return false
}

func cloneResult(source suite.Result) suite.Result {
	cloned := suite.Result{Assessment: source.Assessment, Scenarios: make([]suite.ScenarioResult, len(source.Scenarios))}
	copy(cloned.Scenarios, source.Scenarios)
	for index := range cloned.Scenarios {
		cloned.Scenarios[index].Findings = append([]suite.FindingCode(nil), source.Scenarios[index].Findings...)
	}
	return cloned
}

func assertSanitized(t *testing.T, result suite.Result) {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	printed := strings.Join([]string{string(encoded), reflect.TypeOf(result).String()}, "\n")
	for _, forbidden := range []string{
		rawMarker,
		"Authorization",
		"Bearer http-retry-check-synthetic-scenario-suite-v1",
		"http://127.0.0.1:",
		"scenario-suite-original",
		"scenario-suite-modified",
	} {
		if strings.Contains(printed, forbidden) || bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("result exposed forbidden runtime material %q", forbidden)
		}
	}
}
