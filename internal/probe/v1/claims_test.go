// Package probe drives httpcheck.Run with custom Doers to verify the runtime
// claims listed in the implementation plan. Every test logs the observed
// result so the -v output can be quoted verbatim.
package probe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
	httpchecktest "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/testing"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// newHTTP1Client mirrors the guide's explicit HTTP/1 baseline transport.
func newHTTP1Client(t testing.TB, stripCrossOriginCredential bool) *http.Client {
	t.Helper()
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{Proxy: nil, Protocols: protocols, DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}
	if stripCrossOriginCredential {
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		}
	}
	return client
}

func closeResponse(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

func hasFinding(findings []httpcheck.FindingCode, wanted httpcheck.FindingCode) bool {
	for _, finding := range findings {
		if finding == wanted {
			return true
		}
	}
	return false
}

func describe(result httpcheck.Result) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "overall=%s", result.Assessment)
	for _, row := range result.Scenarios {
		o := row.Observation
		fmt.Fprintf(&builder,
			"\n  %-34s %-28s findings=%v attempts=%d/limit=%d protocol=%q capture=%t effects=%d overlap=%d "+
				"retryAfterEffect=%d retryAfterUnconfirmed=%d retryBeforeResponse=%d responses=%d/%d first=%t "+
				"delay=%d method=%t dest=%t body=%t credential=%s cleanup=%s",
			row.Scenario, row.Assessment, row.Findings, o.AttemptCount, o.AttemptLimit, o.Protocol,
			o.CaptureComplete, o.EffectCount, o.OverlapCount, o.RetryAfterEffectCount,
			o.RetryAfterUnconfirmedCount, o.RetryBeforeResponseCount, o.ResponseAttemptCount,
			o.ResponseCompleteCount, o.FirstResponseComplete, o.DelayCompleteCount, o.MethodConsistent,
			o.DestinationConsistent, o.BodyConsistent, o.Credential, o.Cleanup)
	}
	return builder.String()
}

func mustValidate(t *testing.T, result httpcheck.Result, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Run returned error: %v\n%s", err, describe(result))
	}
	if verr := httpcheck.Validate(result); verr != nil {
		t.Fatalf("Validate failed: %v\n%s", verr, describe(result))
	}
	if len(result.Scenarios) != 6 {
		t.Fatalf("scenario count = %d, want 6\n%s", len(result.Scenarios), describe(result))
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

type countingDoer struct{ calls atomic.Uint32 }

func (d *countingDoer) Do(request *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	_ = request.Body.Close()
	return nil, errors.New("probe: counting doer refuses every request")
}

// recordingBody buffers everything the transport reads from the request body
// so an identical copy can be re-sent later.
type recordingBody struct {
	inner io.ReadCloser
	mu    sync.Mutex
	buf   bytes.Buffer
}

func (b *recordingBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if n > 0 {
		b.mu.Lock()
		b.buf.Write(p[:n])
		b.mu.Unlock()
	}
	return n, err
}

func (b *recordingBody) Close() error { return b.inner.Close() }

func (b *recordingBody) snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

// rawRequest holds the parts needed to hand-write an HTTP/1 request equal in
// method, target, Host, Authorization, framing and body bytes to the one the
// transport sent.
type rawRequest struct {
	host, method, target, authorization, protocol string
	body                                          []byte
}

func rawFrom(request *http.Request, body []byte) rawRequest {
	return rawRequest{
		host: request.URL.Host, method: request.Method, target: request.URL.RequestURI(),
		authorization: request.Header.Get("Authorization"), protocol: "HTTP/1.1", body: body,
	}
}

// sendRaw dials the origin, writes the request, then drains until the origin
// closes so the attempt is fully recorded before it returns.
func sendRaw(raw rawRequest) error {
	conn, err := net.DialTimeout("tcp4", raw.host, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var wire bytes.Buffer
	fmt.Fprintf(&wire, "%s %s %s\r\nHost: %s\r\nAuthorization: %s\r\nContent-Length: %d\r\n\r\n",
		raw.method, raw.target, raw.protocol, raw.host, raw.authorization, len(raw.body))
	wire.Write(raw.body)
	if _, err := conn.Write(wire.Bytes()); err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, conn)
	if err != nil && errors.Is(err, syscall.ECONNRESET) {
		return nil
	}
	return err
}

// bareConnect dials the origin and closes without sending a byte.
func bareConnect(host string) error {
	conn, err := net.DialTimeout("tcp4", host, time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func cloneWithReplayBody(request *http.Request) (*http.Request, error) {
	if request.GetBody == nil {
		return nil, errors.New("probe: request has no GetBody")
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

// ---------------------------------------------------------------------------
// Claim 1: bare connect is not a replay; a full identical copy is.
// ---------------------------------------------------------------------------

type postErrorProbeDoer struct {
	client   *http.Client
	fullCopy bool
	calls    atomic.Uint32
	mu       sync.Mutex
	probes   []error
}

func (d *postErrorProbeDoer) Do(request *http.Request) (*http.Response, error) {
	call := d.calls.Add(1)
	if call > 3 {
		return d.client.Do(request)
	}
	recorder := &recordingBody{inner: request.Body}
	request.Body = recorder
	response, firstErr := d.client.Do(request)
	closeResponse(response)
	if firstErr == nil {
		return nil, errors.New("probe: disconnect scenario unexpectedly produced a response")
	}
	var probeErr error
	if d.fullCopy {
		probeErr = sendRaw(rawFrom(request, recorder.snapshot()))
	} else {
		probeErr = bareConnect(request.URL.Host)
	}
	d.mu.Lock()
	d.probes = append(d.probes, probeErr)
	d.mu.Unlock()
	return nil, firstErr
}

func (d *postErrorProbeDoer) probeErrors() []error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]error(nil), d.probes...)
}

func TestClaim1_BareConnectIsNotReplay(t *testing.T) {
	doer := &postErrorProbeDoer{client: newHTTP1Client(t, true)}
	result, err := httpcheck.Run(context.Background(), doer)
	mustValidate(t, result, err)
	t.Logf("bare-connect result:\n%s", describe(result))
	t.Logf("doer calls=%d probe errors=%v", doer.calls.Load(), doer.probeErrors())

	for _, probeErr := range doer.probeErrors() {
		if probeErr != nil {
			t.Errorf("bare connect probe failed: %v", probeErr)
		}
	}
	if len(doer.probeErrors()) != 3 {
		t.Errorf("bare probes = %d, want 3", len(doer.probeErrors()))
	}
	for index := 0; index < 3; index++ {
		row := result.Scenarios[index]
		if row.Assessment != httpcheck.AssessmentInconclusive {
			t.Errorf("row %d (%s) assessment = %s, want inconclusive", index, row.Scenario, row.Assessment)
		}
		if !hasFinding(row.Findings, httpcheck.FindingCaptureIncomplete) {
			t.Errorf("row %d (%s) findings %v lack capture_incomplete", index, row.Scenario, row.Findings)
		}
		if hasFinding(row.Findings, httpcheck.FindingRetryAfterAcceptedRequest) ||
			hasFinding(row.Findings, httpcheck.FindingRetryAfterUnconfirmedAcceptance) {
			t.Errorf("row %d (%s) has a replay finding: %v", index, row.Scenario, row.Findings)
		}
		if row.Observation.RetryAfterEffectCount != 0 || row.Observation.RetryAfterUnconfirmedCount != 0 {
			t.Errorf("row %d (%s) retry counters = %d/%d, want 0/0", index, row.Scenario,
				row.Observation.RetryAfterEffectCount, row.Observation.RetryAfterUnconfirmedCount)
		}
	}
	for index := 3; index < 6; index++ {
		if result.Scenarios[index].Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
			t.Errorf("later row %d (%s) = %s, want positive (run should continue)", index,
				result.Scenarios[index].Scenario, result.Scenarios[index].Assessment)
		}
	}
}

func TestClaim1_FullIdenticalCopyControlIsUnsafe(t *testing.T) {
	doer := &postErrorProbeDoer{client: newHTTP1Client(t, true), fullCopy: true}
	result, err := httpcheck.Run(context.Background(), doer)
	mustValidate(t, result, err)
	t.Logf("full-copy result:\n%s", describe(result))
	t.Logf("doer calls=%d probe errors=%v", doer.calls.Load(), doer.probeErrors())

	if result.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved {
		t.Errorf("overall = %s, want unsafe_behavior_observed", result.Assessment)
	}
	want := []httpcheck.FindingCode{
		httpcheck.FindingRetryAfterAcceptedRequest,
		httpcheck.FindingRetryAfterUnconfirmedAcceptance,
		httpcheck.FindingRetryAfterAcceptedRequest,
	}
	for index, finding := range want {
		row := result.Scenarios[index]
		if row.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved || !hasFinding(row.Findings, finding) {
			t.Errorf("row %d (%s) = %s %v, want unsafe with %s", index, row.Scenario, row.Assessment, row.Findings, finding)
		}
	}
}

// ---------------------------------------------------------------------------
// Claim 2: error completion accepted when self-consistent.
// ---------------------------------------------------------------------------

func TestClaim2a_CheckRedirectErrorIsPositiveAndContinues(t *testing.T) {
	variants := map[string]func() *http.Client{
		"explicit HTTP/1 transport": func() *http.Client { return newHTTP1Client(t, false) },
		"plain default client": func() *http.Client {
			client := &http.Client{}
			t.Cleanup(client.CloseIdleConnections)
			return client
		},
	}
	for name, build := range variants {
		t.Run(name, func(t *testing.T) {
			client := build()
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return errors.New("no redirects")
			}
			started := time.Now()
			result, err := httpcheck.Run(context.Background(), client)
			elapsed := time.Since(started)
			mustValidate(t, result, err)
			t.Logf("elapsed=%v result:\n%s", elapsed, describe(result))

			redirect := result.Scenarios[3]
			if redirect.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || len(redirect.Findings) != 0 {
				t.Errorf("redirect row = %s %v, want positive with no findings", redirect.Assessment, redirect.Findings)
			}
			for _, index := range []int{4, 5} {
				if result.Scenarios[index].Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
					t.Errorf("row %d (%s) = %s, want positive (run should continue)", index,
						result.Scenarios[index].Scenario, result.Scenarios[index].Assessment)
				}
			}
			if elapsed > 4*time.Second {
				t.Logf("NOTE: run took %v; a scenario-timeout stall would show here", elapsed)
			}
		})
	}
}

type retryOnceOn503Doer struct {
	client *http.Client
	calls  atomic.Uint32
}

var errServiceUnavailable = errors.New("service unavailable")

func (d *retryOnceOn503Doer) Do(request *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	response, err := d.client.Do(request)
	if err != nil || response.StatusCode != http.StatusServiceUnavailable {
		return response, err
	}
	closeResponse(response)
	replay, err := cloneWithReplayBody(request)
	if err != nil {
		return nil, err
	}
	response, err = d.client.Do(replay)
	if err != nil {
		return response, err
	}
	closeResponse(response)
	return nil, fmt.Errorf("retry policy exhausted after 2 attempts: %w", errServiceUnavailable)
}

func TestClaim2b_RetryOnceThenWrappedErrorIsPositiveRetryLimit(t *testing.T) {
	doer := &retryOnceOn503Doer{client: newHTTP1Client(t, true)}
	result, err := httpcheck.Run(context.Background(), doer)
	mustValidate(t, result, err)
	t.Logf("retry-once result:\n%s", describe(result))

	row := result.Scenarios[4]
	if row.Scenario != httpcheck.ScenarioRetryLimit {
		t.Fatalf("row 4 = %s", row.Scenario)
	}
	if row.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 {
		t.Errorf("retry_limit = %s %v, want positive", row.Assessment, row.Findings)
	}
	if row.Observation.AttemptCount != 2 {
		t.Errorf("retry_limit attempts = %d, want 2", row.Observation.AttemptCount)
	}
	if result.Scenarios[5].Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
		t.Errorf("delayed_response = %s, want positive (run should continue)", result.Scenarios[5].Assessment)
	}
	if result.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
		t.Errorf("overall = %s, want positive", result.Assessment)
	}
}

func TestClaim2c_ZeroAttemptErrorStaysInconclusive(t *testing.T) {
	for _, failAt := range []uint32{4, 5} {
		t.Run(fmt.Sprintf("error without attempts on call %d", failAt), func(t *testing.T) {
			client := newHTTP1Client(t, true)
			var calls atomic.Uint32
			result, err := httpcheck.Run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
				if calls.Add(1) == failAt {
					_ = request.Body.Close()
					return nil, errors.New("probe: refused before any attempt")
				}
				return client.Do(request)
			}))
			mustValidate(t, result, err)
			t.Logf("calls=%d result:\n%s", calls.Load(), describe(result))

			row := result.Scenarios[failAt-1]
			if row.Assessment != httpcheck.AssessmentInconclusive {
				t.Errorf("row %d (%s) = %s, want inconclusive", failAt-1, row.Scenario, row.Assessment)
			}
			if !hasFinding(row.Findings, httpcheck.FindingAttemptNotObserved) {
				t.Errorf("row %d findings %v lack attempt_not_observed", failAt-1, row.Findings)
			}
			if row.Observation.AttemptCount != 0 || row.Observation.Protocol != "" {
				t.Errorf("row %d attempts/protocol = %d/%q, want 0/\"\"", failAt-1, row.Observation.AttemptCount, row.Observation.Protocol)
			}
			for index := int(failAt); index < 6; index++ {
				if result.Scenarios[index].Assessment != httpcheck.AssessmentInconclusive {
					t.Errorf("later row %d = %s, want inconclusive (run should stop)", index, result.Scenarios[index].Assessment)
				}
			}
			if calls.Load() != failAt {
				t.Errorf("doer calls = %d, want %d (run should stop)", calls.Load(), failAt)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Claim 3: context cancellation surfaces.
// ---------------------------------------------------------------------------

func TestClaim3_CancellationDuringSecondScenario(t *testing.T) {
	client := newHTTP1Client(t, true)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Uint32
	result, err := httpcheck.Run(parent, doerFunc(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 2 {
			cancel()
			_ = request.Body.Close()
			return nil, request.Context().Err()
		}
		return client.Do(request)
	}))
	t.Logf("err=%v calls=%d result:\n%s", err, calls.Load(), describe(result))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want errors.Is(err, context.Canceled)", err)
	}
	if verr := httpcheck.Validate(result); verr != nil {
		t.Errorf("partial result invalid: %v", verr)
	}
	if len(result.Scenarios) != 6 || result.Scenarios[0].Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
		t.Errorf("first row not positive: %s", describe(result))
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestClaim3_AlreadyCancelledContextReturnsContextError(t *testing.T) {
	doer := new(countingDoer)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := httpcheck.Run(ctx, doer)
	t.Logf("cancelled: err=%v isCanceled=%t isInvalidCall=%t result=%#v calls=%d",
		err, errors.Is(err, context.Canceled), errors.Is(err, httpcheck.ErrInvalidCall), result, doer.calls.Load())
	if !errors.Is(err, context.Canceled) || errors.Is(err, httpcheck.ErrInvalidCall) {
		t.Errorf("err = %v, want context.Canceled and not ErrInvalidCall", err)
	}
	if !reflect.DeepEqual(result, httpcheck.Result{}) {
		t.Errorf("result = %#v, want zero", result)
	}
	if doer.calls.Load() != 0 {
		t.Errorf("Doer invoked %d times", doer.calls.Load())
	}

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	result, err = httpcheck.Run(expired, doer)
	t.Logf("expired: err=%v isDeadline=%t result=%#v calls=%d", err, errors.Is(err, context.DeadlineExceeded), result, doer.calls.Load())
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, httpcheck.ErrInvalidCall) {
		t.Errorf("expired err = %v, want context.DeadlineExceeded", err)
	}
}

// ---------------------------------------------------------------------------
// Claim 4: panic propagates after listeners close.
// ---------------------------------------------------------------------------

type panicSentinel struct{ id int }

func TestClaim4_PanicPropagatesAfterListenersClose(t *testing.T) {
	for _, panicAt := range []uint32{1, 5} {
		t.Run(fmt.Sprintf("panic on call %d", panicAt), func(t *testing.T) {
			client := newHTTP1Client(t, true)
			var mu sync.Mutex
			var seen []string
			record := func(host string) {
				mu.Lock()
				defer mu.Unlock()
				for _, existing := range seen {
					if existing == host {
						return
					}
				}
				seen = append(seen, host)
			}
			previous := client.CheckRedirect
			client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
				record(request.URL.Host)
				return previous(request, via)
			}
			sentinel := &panicSentinel{id: int(panicAt)}
			var calls atomic.Uint32
			var recovered any
			panicked := false
			func() {
				defer func() {
					recovered = recover()
					panicked = recovered != nil
				}()
				_, _ = httpcheck.Run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
					record(request.URL.Host)
					if calls.Add(1) == panicAt {
						_ = request.Body.Close()
						panic(sentinel)
					}
					return client.Do(request)
				}))
			}()
			if !panicked || recovered != sentinel {
				t.Fatalf("panic = %#v (panicked=%t), want sentinel %p", recovered, panicked, sentinel)
			}
			mu.Lock()
			hosts := append([]string(nil), seen...)
			mu.Unlock()
			t.Logf("recovered sentinel id=%d; hosts seen=%v", sentinel.id, hosts)
			if len(hosts) == 0 {
				t.Fatal("doer saw no addresses")
			}
			for _, host := range hosts {
				conn, dialErr := net.DialTimeout("tcp4", host, time.Second)
				if dialErr == nil {
					_ = conn.Close()
					t.Errorf("dial %s succeeded after panic; listener still open", host)
					continue
				}
				t.Logf("dial %s -> %v (ECONNREFUSED=%t)", host, dialErr, errors.Is(dialErr, syscall.ECONNREFUSED))
				if !errors.Is(dialErr, syscall.ECONNREFUSED) {
					t.Errorf("dial %s error = %v, want connection refused", host, dialErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Claim 5: quiet window observes a retry sent 50 ms after Do returns.
// ---------------------------------------------------------------------------

type lateRawReplayDoer struct {
	client *http.Client
	delay  time.Duration
	done   chan error
	calls  atomic.Uint32
}

func (d *lateRawReplayDoer) Do(request *http.Request) (*http.Response, error) {
	if d.calls.Add(1) != 1 {
		return d.client.Do(request)
	}
	recorder := &recordingBody{inner: request.Body}
	request.Body = recorder
	raw := rawFrom(request, nil)
	response, err := d.client.Do(request)
	go func() {
		timer := time.NewTimer(d.delay)
		defer timer.Stop()
		<-timer.C
		raw.body = recorder.snapshot()
		d.done <- sendRaw(raw)
	}()
	return response, err
}

func TestClaim5_QuietWindowObservesLateIdenticalRetry(t *testing.T) {
	doer := &lateRawReplayDoer{client: newHTTP1Client(t, true), delay: 50 * time.Millisecond, done: make(chan error, 1)}
	result, err := httpcheck.Run(context.Background(), doer)
	mustValidate(t, result, err)
	var lateErr error
	select {
	case lateErr = <-doer.done:
	case <-time.After(5 * time.Second):
		t.Fatal("late replay never finished")
	}
	t.Logf("late replay error=%v result:\n%s", lateErr, describe(result))

	row := result.Scenarios[0]
	if lateErr != nil {
		t.Errorf("late replay failed: %v", lateErr)
	}
	if row.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved {
		t.Errorf("accept_then_disconnect = %s, want unsafe", row.Assessment)
	}
	if !hasFinding(row.Findings, httpcheck.FindingRetryAfterAcceptedRequest) {
		t.Errorf("findings %v lack retry_after_accepted_request", row.Findings)
	}
	if row.Observation.RetryAfterEffectCount != 1 || row.Observation.AttemptCount != 2 || !row.Observation.CaptureComplete {
		t.Errorf("observation = %#v", row.Observation)
	}
}

func TestClaim5_RetryAfterQuietWindowIsOutsideObservation(t *testing.T) {
	doer := &lateRawReplayDoer{client: newHTTP1Client(t, true), delay: 700 * time.Millisecond, done: make(chan error, 1)}
	result, err := httpcheck.Run(context.Background(), doer)
	mustValidate(t, result, err)
	var lateErr error
	select {
	case lateErr = <-doer.done:
	case <-time.After(5 * time.Second):
		t.Fatal("late replay never finished")
	}
	t.Logf("late replay (700ms) error=%v result:\n%s", lateErr, describe(result))
	row := result.Scenarios[0]
	if lateErr == nil {
		t.Errorf("late replay after the quiet window reached an origin; expected refusal")
	}
	if row.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || row.Observation.AttemptCount != 1 {
		t.Errorf("accept_then_disconnect = %s attempts=%d, want positive with 1 attempt", row.Assessment, row.Observation.AttemptCount)
	}
}

// ---------------------------------------------------------------------------
// Claim 6: options.
// ---------------------------------------------------------------------------

func TestClaim6_InvalidOptionsAreRejected(t *testing.T) {
	doer := new(countingDoer)
	tests := []struct {
		name    string
		options []httpcheck.Option
	}{
		{"nil option", []httpcheck.Option{nil}},
		{"scenario timeout zero", []httpcheck.Option{httpcheck.WithScenarioTimeout(0)}},
		{"connection timeout zero", []httpcheck.Option{httpcheck.WithConnectionTimeout(0)}},
		{"quiet window zero", []httpcheck.Option{httpcheck.WithQuietWindow(0)}},
		{"attempt limit 0", []httpcheck.Option{httpcheck.WithAttemptLimit(0)}},
		{"attempt limit 4", []httpcheck.Option{httpcheck.WithAttemptLimit(4)}},
		{"scenario timeout above minute", []httpcheck.Option{httpcheck.WithScenarioTimeout(time.Minute + time.Nanosecond)}},
		{"last invalid wins", []httpcheck.Option{httpcheck.WithAttemptLimit(2), httpcheck.WithAttemptLimit(4)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := httpcheck.Run(context.Background(), doer, test.options...)
			t.Logf("err=%v (ErrInvalidCall=%t) result=%#v", err, errors.Is(err, httpcheck.ErrInvalidCall), result)
			if !errors.Is(err, httpcheck.ErrInvalidCall) || !reflect.DeepEqual(result, httpcheck.Result{}) {
				t.Errorf("Run = %#v/%v, want zero result and ErrInvalidCall", result, err)
			}
		})
	}
	if doer.calls.Load() != 0 {
		t.Errorf("Doer invoked %d times with invalid options", doer.calls.Load())
	}
}

type retryOn503Doer struct {
	client      *http.Client
	maxAttempts int
	calls       atomic.Uint32
}

func (d *retryOn503Doer) Do(request *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	current := request
	for attempt := 1; ; attempt++ {
		response, err := d.client.Do(current)
		if err != nil || response.StatusCode != http.StatusServiceUnavailable || attempt == d.maxAttempts {
			return response, err
		}
		closeResponse(response)
		current, err = cloneWithReplayBody(request)
		if err != nil {
			return nil, err
		}
	}
}

func TestClaim6_AttemptLimitThreeAllowsThreeAttempts(t *testing.T) {
	doer := &retryOn503Doer{client: newHTTP1Client(t, true), maxAttempts: 3}
	result, err := httpcheck.Run(context.Background(), doer, httpcheck.WithAttemptLimit(3))
	mustValidate(t, result, err)
	t.Logf("limit=3 result:\n%s", describe(result))

	row := result.Scenarios[4]
	if row.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 {
		t.Errorf("retry_limit = %s %v, want positive", row.Assessment, row.Findings)
	}
	if row.Observation.AttemptCount != 3 || row.Observation.AttemptLimit != 3 {
		t.Errorf("retry_limit attempts/limit = %d/%d, want 3/3", row.Observation.AttemptCount, row.Observation.AttemptLimit)
	}
	for index, row := range result.Scenarios {
		if row.Observation.AttemptLimit != 3 {
			t.Errorf("row %d AttemptLimit = %d, want 3", index, row.Observation.AttemptLimit)
		}
	}
	if result.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
		t.Errorf("overall = %s, want positive", result.Assessment)
	}
}

func TestClaim6_DefaultLimitTwoFlagsThreeAttempts(t *testing.T) {
	doer := &retryOn503Doer{client: newHTTP1Client(t, true), maxAttempts: 3}
	result, err := httpcheck.Run(context.Background(), doer)
	mustValidate(t, result, err)
	t.Logf("default-limit result:\n%s", describe(result))
	row := result.Scenarios[4]
	if row.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved || !hasFinding(row.Findings, httpcheck.FindingAttemptLimitExceeded) {
		t.Errorf("retry_limit = %s %v, want unsafe with attempt_limit_exceeded", row.Assessment, row.Findings)
	}
	if row.Observation.AttemptCount != 3 || row.Observation.AttemptLimit != 2 {
		t.Errorf("retry_limit attempts/limit = %d/%d, want 3/2", row.Observation.AttemptCount, row.Observation.AttemptLimit)
	}
}

func TestClaim6_LastValidOptionWins(t *testing.T) {
	doer := &retryOn503Doer{client: newHTTP1Client(t, true), maxAttempts: 3}
	result, err := httpcheck.Run(context.Background(), doer,
		httpcheck.WithAttemptLimit(4), httpcheck.WithAttemptLimit(3),
		httpcheck.WithQuietWindow(0), httpcheck.WithQuietWindow(100*time.Millisecond))
	mustValidate(t, result, err)
	t.Logf("last-wins result:\n%s", describe(result))
	if result.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || result.Scenarios[4].Observation.AttemptLimit != 3 {
		t.Errorf("last-wins = %s limit=%d, want positive with limit 3", result.Assessment, result.Scenarios[4].Observation.AttemptLimit)
	}
}

// ---------------------------------------------------------------------------
// Claim 7: protocol recorded.
// ---------------------------------------------------------------------------

func TestClaim7_ProtocolIsHTTP11OnCompletedRows(t *testing.T) {
	result, err := httpcheck.Run(context.Background(), newHTTP1Client(t, true))
	mustValidate(t, result, err)
	t.Logf("positive result:\n%s", describe(result))
	if result.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
		t.Errorf("overall = %s, want positive", result.Assessment)
	}
	for index, row := range result.Scenarios {
		if row.Observation.Protocol != "HTTP/1.1" {
			t.Errorf("row %d (%s) protocol = %q, want HTTP/1.1", index, row.Scenario, row.Observation.Protocol)
		}
	}
}

func TestClaim7_ProtocolEmptyWithoutCapturedHead(t *testing.T) {
	t.Run("zero attempts on first scenario", func(t *testing.T) {
		doer := new(countingDoer)
		result, err := httpcheck.Run(context.Background(), doer)
		mustValidate(t, result, err)
		t.Logf("zero-attempt result:\n%s", describe(result))
		for index, row := range result.Scenarios {
			if row.Observation.Protocol != "" || row.Observation.AttemptCount != 0 {
				t.Errorf("row %d protocol/attempts = %q/%d, want \"\"/0", index, row.Observation.Protocol, row.Observation.AttemptCount)
			}
		}
	})
	t.Run("raw HTTP/1.0 head on first scenario", func(t *testing.T) {
		client := newHTTP1Client(t, true)
		var calls atomic.Uint32
		var rawErr error
		result, err := httpcheck.Run(context.Background(), doerFunc(func(request *http.Request) (*http.Response, error) {
			if calls.Add(1) != 1 {
				return client.Do(request)
			}
			body, readErr := io.ReadAll(request.Body)
			_ = request.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			raw := rawFrom(request, body)
			raw.protocol = "HTTP/1.0"
			rawErr = sendRaw(raw)
			return nil, errors.New("probe: sent a raw HTTP/1.0 request")
		}))
		mustValidate(t, result, err)
		t.Logf("raw send error=%v result:\n%s", rawErr, describe(result))
		row := result.Scenarios[0]
		if row.Observation.AttemptCount != 1 || row.Observation.Protocol != "" || row.Observation.CaptureComplete {
			t.Errorf("HTTP/1.0 row = attempts %d protocol %q capture %t, want 1/\"\"/false", row.Observation.AttemptCount, row.Observation.Protocol, row.Observation.CaptureComplete)
		}
		for index := 1; index < 6; index++ {
			if result.Scenarios[index].Observation.Protocol != "HTTP/1.1" {
				t.Errorf("row %d protocol = %q, want HTTP/1.1", index, result.Scenarios[index].Observation.Protocol)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Claim 8: plain default client matches the guide's explicit transport.
// ---------------------------------------------------------------------------

func TestClaim8_PlainDefaultClientMatchesExplicitTransport(t *testing.T) {
	plain := &http.Client{}
	t.Cleanup(plain.CloseIdleConnections)
	plainResult, err := httpcheck.Run(context.Background(), plain)
	mustValidate(t, plainResult, err)
	t.Logf("plain &http.Client{} result:\n%s", describe(plainResult))

	explicitResult, err := httpcheck.Run(context.Background(), newHTTP1Client(t, false))
	mustValidate(t, explicitResult, err)
	t.Logf("explicit HTTP/1 transport result:\n%s", describe(explicitResult))

	for name, result := range map[string]httpcheck.Result{"plain": plainResult, "explicit": explicitResult} {
		if result.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved {
			t.Errorf("%s overall = %s, want unsafe", name, result.Assessment)
		}
		for index, row := range result.Scenarios {
			if index == 3 {
				if row.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved ||
					!reflect.DeepEqual(row.Findings, []httpcheck.FindingCode{httpcheck.FindingCredentialExposedAtTarget}) ||
					row.Observation.Credential != httpcheck.CredentialExposedAtTarget {
					t.Errorf("%s redirect row = %s %v credential=%s", name, row.Assessment, row.Findings, row.Observation.Credential)
				}
				continue
			}
			if row.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 {
				t.Errorf("%s row %d (%s) = %s %v, want positive", name, index, row.Scenario, row.Assessment, row.Findings)
			}
		}
	}
	if !reflect.DeepEqual(plainResult, explicitResult) {
		t.Errorf("plain and explicit results differ:\nplain:    %s\nexplicit: %s", describe(plainResult), describe(explicitResult))
	}
}

// ---------------------------------------------------------------------------
// Claim 9: testing helper.
// ---------------------------------------------------------------------------

// Compile-time proof that Check accepts variadic options.
var _ = func(t *testing.T, doer httpcheck.Doer, options ...httpcheck.Option) {
	httpchecktest.Check(t, doer, options...)
}

func TestClaim9_CheckAcceptsOptionsAndPasses(t *testing.T) {
	httpchecktest.Check(t, newHTTP1Client(t, true), httpcheck.WithQuietWindow(100*time.Millisecond), httpcheck.WithAttemptLimit(2))
}

// TestHelperCheckCancelledContext is only meaningful inside the subprocess
// spawned by TestClaim9_CheckUnderCancelledContext. t.Context() is cancelled
// just before Cleanup functions run, so calling Check from a Cleanup exercises
// the helper under an already-cancelled context inside a real go test run.
func TestHelperCheckCancelledContext(t *testing.T) {
	if os.Getenv("PROBE_HELPER_CANCELLED") != "1" {
		t.Skip("helper: run via TestClaim9_CheckUnderCancelledContext")
	}
	doer := new(countingDoer)
	t.Cleanup(func() { t.Logf("doer calls after Check: %d", doer.calls.Load()) }) // registered first, runs last
	t.Cleanup(func() {
		t.Logf("context error before Check: %v", t.Context().Err())
		httpchecktest.Check(t, doer)
	})
}

func TestClaim9_CheckUnderCancelledContext(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperCheckCancelledContext$", "-test.v", "-test.count=1", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "PROBE_HELPER_CANCELLED=1")
	output, runErr := cmd.CombinedOutput()
	text := string(output)
	t.Logf("subprocess exit=%v output:\n%s", runErr, text)

	if runErr == nil {
		t.Errorf("subprocess passed; expected the helper to fail the test")
	}
	if !strings.Contains(text, "--- FAIL: TestHelperCheckCancelledContext") {
		t.Errorf("helper test did not fail")
	}
	canceledLines := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "context canceled") && !strings.Contains(line, "context error before Check") {
			canceledLines++
		}
	}
	if canceledLines != 1 {
		t.Errorf("lines naming the context error = %d, want exactly 1", canceledLines)
	}
	if strings.Contains(text, "HTTP Retry Check") {
		t.Errorf("output contains per-scenario or generic run-failure lines")
	}
	if !strings.Contains(text, "doer calls after Check: 0") {
		t.Errorf("Doer was invoked under a cancelled context (or the log line is missing)")
	}
}
