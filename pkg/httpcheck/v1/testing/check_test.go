package httpchecktest

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
)

func TestReduceValidatedUsesOnlyFixedExplanations(t *testing.T) {
	positive := positiveResult()
	unsafe := unsafeResult()
	inconclusive := inconclusiveResult()
	for name, result := range map[string]httpcheck.Result{
		"positive": positive, "unsafe": unsafe, "inconclusive": inconclusive,
	} {
		if err := httpcheck.Validate(result); err != nil {
			t.Fatalf("%s fixture invalid: %v", name, err)
		}
	}
	tests := []struct {
		name      string
		result    httpcheck.Result
		lines     int
		failures  int
		firstText string
	}{
		{
			name: "positive", result: positive, lines: 6,
			firstText: positiveLinePrefix + string(httpcheck.ScenarioAcceptThenDisconnect),
		},
		{
			name: "unsafe", result: unsafe, lines: 7, failures: 2,
			firstText: unsafeLinePrefix + string(httpcheck.ScenarioAcceptThenDisconnect) + ": " +
				string(httpcheck.FindingRetryAfterAcceptedRequest) + ": " +
				httpcheck.FindingText(httpcheck.FindingRetryAfterAcceptedRequest),
		},
		{
			name: "inconclusive", result: inconclusive, lines: 6, failures: 1,
			firstText: inconclusivePrefix + string(httpcheck.ScenarioAcceptThenDisconnect) + ": " +
				string(httpcheck.FindingCaptureIncomplete) + ": " +
				httpcheck.FindingText(httpcheck.FindingCaptureIncomplete),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := cloneResult(test.result)
			plan := reduceValidated(test.result)
			if plan.fatal != "" || len(plan.lines) != test.lines || !reflect.DeepEqual(test.result, before) {
				t.Fatalf("plan/result = %#v/%#v", plan, test.result)
			}
			failures := 0
			for _, line := range plan.lines {
				if line.failure {
					failures++
				}
				if line.text == "" || strings.Contains(line.text, "127.0.0.1:") {
					t.Fatalf("unsafe helper line = %q", line.text)
				}
			}
			if failures != test.failures || plan.lines[0].text != test.firstText {
				t.Fatalf("plan = %#v", plan)
			}
		})
	}
}

func TestReduceValidatedIntegrityGuardFailsClosed(t *testing.T) {
	forged := positiveResult()
	forged.Assessment = httpcheck.AssessmentInconclusive
	plan := reduceValidated(forged)
	if plan.fatal != invalidResultText || len(plan.lines) != 0 {
		t.Fatalf("integrity plan = %#v", plan)
	}
}

func TestCheckPassesExplicitProxyFreeHTTP1Client(t *testing.T) {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{Proxy: nil, Protocols: protocols, DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}
	Check(t, client, httpcheck.WithQuietWindow(time.Millisecond))
}

func TestCheckReportsParentCancellationDirectly(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	reporter := new(recordingReporter)
	doer := new(countingDoer)

	check(reporter, parent, doer)

	if len(reporter.fatals) != 1 || len(reporter.fatals[0]) != 1 ||
		reporter.fatals[0][0] != context.Canceled || len(reporter.errors) != 0 ||
		len(reporter.logs) != 0 || reporter.helpers != 1 || doer.calls != 0 {
		t.Fatalf("reporter/doer = %#v/%#v", reporter, doer)
	}
}

func TestCheckKeepsGenericNonContextRunFailure(t *testing.T) {
	reporter := new(recordingReporter)

	check(reporter, context.Background(), nil)

	if len(reporter.fatals) != 1 || len(reporter.fatals[0]) != 1 ||
		reporter.fatals[0][0] != runFailureText || len(reporter.errors) != 0 ||
		len(reporter.logs) != 0 || reporter.helpers != 1 {
		t.Fatalf("reporter = %#v", reporter)
	}
}

type recordingReporter struct {
	helpers int
	fatals  [][]any
	errors  [][]any
	logs    [][]any
}

func (reporter *recordingReporter) Helper() {
	reporter.helpers++
}

func (reporter *recordingReporter) Fatal(values ...any) {
	reporter.fatals = append(reporter.fatals, values)
}

func (reporter *recordingReporter) Error(values ...any) {
	reporter.errors = append(reporter.errors, values)
}

func (reporter *recordingReporter) Log(values ...any) {
	reporter.logs = append(reporter.logs, values)
}

type countingDoer struct {
	calls int
}

func (doer *countingDoer) Do(request *http.Request) (*http.Response, error) {
	doer.calls++
	return nil, context.Canceled
}

func positiveResult() httpcheck.Result {
	order := []httpcheck.ScenarioID{
		httpcheck.ScenarioAcceptThenDisconnect,
		httpcheck.ScenarioDisconnectBeforeAcceptance,
		httpcheck.ScenarioChangedBodyRetry,
		httpcheck.ScenarioCrossOriginRedirectCredentials,
		httpcheck.ScenarioRetryLimit,
		httpcheck.ScenarioDelayedResponse,
	}
	observations := []httpcheck.Observation{
		positiveObservation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly),
		positiveObservation(1, 0, 0, 0, false, 0, httpcheck.CredentialSourceOnly),
		positiveObservation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly),
		positiveObservation(2, 1, 2, 2, true, 0, httpcheck.CredentialAbsentAtTarget),
		positiveObservation(1, 0, 1, 1, true, 0, httpcheck.CredentialSourceOnly),
		positiveObservation(1, 1, 1, 1, true, 1, httpcheck.CredentialSourceOnly),
	}
	rows := make([]httpcheck.ScenarioResult, len(order))
	for index, scenario := range order {
		rows[index] = httpcheck.ScenarioResult{
			Scenario: scenario, Assessment: httpcheck.AssessmentNoUnsafeBehaviorObserved,
			Observation: observations[index], Findings: make([]httpcheck.FindingCode, 0, 1),
		}
	}
	return httpcheck.Result{Assessment: httpcheck.AssessmentNoUnsafeBehaviorObserved, Scenarios: rows}
}

func unsafeResult() httpcheck.Result {
	result := positiveResult()
	row := &result.Scenarios[0]
	row.Assessment = httpcheck.AssessmentUnsafeBehaviorObserved
	row.Observation.AttemptCount = 2
	row.Observation.EffectCount = 2
	row.Observation.RetryAfterEffectCount = 1
	row.Findings = []httpcheck.FindingCode{
		httpcheck.FindingRetryAfterAcceptedRequest, httpcheck.FindingEffectLimitExceeded,
	}
	result.Assessment = httpcheck.AssessmentUnsafeBehaviorObserved
	return result
}

func inconclusiveResult() httpcheck.Result {
	result := positiveResult()
	row := &result.Scenarios[0]
	row.Assessment = httpcheck.AssessmentInconclusive
	row.Observation.CaptureComplete = false
	row.Findings = []httpcheck.FindingCode{httpcheck.FindingCaptureIncomplete}
	result.Assessment = httpcheck.AssessmentInconclusive
	return result
}

func positiveObservation(
	attempts uint32,
	effects uint64,
	responseAttempts uint32,
	responseComplete uint32,
	firstResponse bool,
	delayComplete uint32,
	credential httpcheck.CredentialState,
) httpcheck.Observation {
	return httpcheck.Observation{
		CaptureComplete: true, AttemptCount: attempts, AttemptLimit: 2, Protocol: "HTTP/1.1",
		EffectCount:          effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: firstResponse, DelayCompleteCount: delayComplete,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credential, Cleanup: httpcheck.CleanupSucceeded,
	}
}

func cloneResult(source httpcheck.Result) httpcheck.Result {
	cloned := httpcheck.Result{
		Assessment: source.Assessment, Scenarios: make([]httpcheck.ScenarioResult, len(source.Scenarios)),
	}
	copy(cloned.Scenarios, source.Scenarios)
	for index := range cloned.Scenarios {
		cloned.Scenarios[index].Findings = append(
			make([]httpcheck.FindingCode, 0, len(source.Scenarios[index].Findings)+1),
			source.Scenarios[index].Findings...,
		)
	}
	return cloned
}
