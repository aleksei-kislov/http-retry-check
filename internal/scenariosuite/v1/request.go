package scenariosuite

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
)

type privateFailure uint8

const invocationFailure privateFailure = 1

func (privateFailure) Error() string {
	return "HTTP scenario suite invocation failed"
}

func newScenarioRequest(
	ctx context.Context,
	target string,
	scenario ScenarioID,
) (*http.Request, *requestBodies, bool) {
	body := []byte(syntheticBodyText)
	replay := bytes.Clone(body)
	if scenario == ScenarioChangedBodyRetry {
		replay = []byte(changedSyntheticBodyText)
	}
	bodies := newRequestBodies(
		body, replay, scenario == ScenarioCrossOriginRedirectCredentials,
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return nil, nil, false
	}
	request.Body, err = bodies.openOriginal()
	if err != nil {
		return nil, nil, false
	}
	request.GetBody = bodies.openReplay
	request.ContentLength = int64(len(body))
	request.Header.Set("Authorization", syntheticCredential)
	clear(body)
	clear(replay)
	return request, bodies, true
}

type invocationCompletion uint8

const (
	invocationInvalidCompletion invocationCompletion = iota
	invocationResponseCompletion
	invocationErrorCompletion
)

type invocationOutcome struct {
	completion     invocationCompletion
	bodiesQuiesced bool
}

func invokeDoer(
	ctx context.Context,
	doer Doer,
	request *http.Request,
	bodies *requestBodies,
) (outcome invocationOutcome) {
	completion := invocationInvalidCompletion
	defer func() {
		if recover() != nil {
			completion = invocationInvalidCompletion
		}
		bodies.seal()
		outcome.bodiesQuiesced = bodies.wait(ctx) == nil
		outcome.completion = completion
	}()

	response, invocationErr := doer.Do(request)
	if invocationErr != nil {
		completion = invocationErrorCompletion
		return outcome
	}
	if response == nil || response.Body == nil {
		return outcome
	}
	if response.Body.Close() != nil {
		return outcome
	}
	bodies.permitUnusedRedirectReplayClose(response.StatusCode)
	completion = invocationResponseCompletion
	return outcome
}

type requestBodies struct {
	mu                         sync.Mutex
	original                   []byte
	replay                     []byte
	openBodies                 map[*requestBody]struct{}
	replayAcquisitions         uint8
	redirectScenario           bool
	closeOneUnusedReplayOnSeal bool
	sealed                     bool
	doneClosed                 bool
	done                       chan struct{}
}

func newRequestBodies(original, replay []byte, redirectScenario bool) *requestBodies {
	return &requestBodies{
		original:         bytes.Clone(original),
		replay:           bytes.Clone(replay),
		openBodies:       make(map[*requestBody]struct{}),
		redirectScenario: redirectScenario,
		done:             make(chan struct{}),
	}
}

func (bodies *requestBodies) openOriginal() (io.ReadCloser, error) {
	return bodies.open(false)
}

func (bodies *requestBodies) openReplay() (io.ReadCloser, error) {
	return bodies.open(true)
}

func (bodies *requestBodies) open(replay bool) (io.ReadCloser, error) {
	bodies.mu.Lock()
	defer bodies.mu.Unlock()
	if bodies.sealed {
		return nil, invocationFailure
	}
	contents := bodies.original
	if replay {
		contents = bodies.replay
	}
	body := &requestBody{reader: bytes.NewReader(contents), owner: bodies, replay: replay}
	bodies.openBodies[body] = struct{}{}
	if replay && bodies.replayAcquisitions < 2 {
		bodies.replayAcquisitions++
	}
	return body, nil
}

func (bodies *requestBodies) permitUnusedRedirectReplayClose(statusCode int) {
	bodies.mu.Lock()
	defer bodies.mu.Unlock()
	if !bodies.sealed && bodies.redirectScenario && statusCode == http.StatusTemporaryRedirect {
		bodies.closeOneUnusedReplayOnSeal = true
	}
}

func (bodies *requestBodies) seal() {
	bodies.mu.Lock()
	defer bodies.mu.Unlock()
	if bodies.sealed {
		return
	}
	bodies.sealed = true
	if bodies.closeOneUnusedReplayOnSeal {
		// net/http creates the controlled 307 replay body before CheckRedirect. When
		// ErrUseLastResponse refuses that hop, one never-read handle is left
		// unclosed. Close only that inert suite-owned shape; started or multiple
		// replay handles remain tracked and therefore fail closed.
		var unusedReplay *requestBody
		unusedReplayCount := 0
		for body := range bodies.openBodies {
			if body.replay && !body.started {
				unusedReplay = body
				unusedReplayCount++
			}
		}
		if bodies.replayAcquisitions == 1 && unusedReplayCount == 1 {
			unusedReplay.closeLocked()
		}
	}
	if len(bodies.openBodies) == 0 {
		bodies.closeDone()
	}
}

func (bodies *requestBodies) wait(ctx context.Context) error {
	select {
	case <-bodies.done:
		return nil
	default:
	}
	select {
	case <-bodies.done:
		return nil
	case <-ctx.Done():
		return invocationFailure
	}
}

func (bodies *requestBodies) closeDone() {
	if bodies.doneClosed {
		return
	}
	bodies.doneClosed = true
	close(bodies.done)
}

type requestBody struct {
	reader  *bytes.Reader
	owner   *requestBodies
	replay  bool
	started bool
	closed  bool
	once    sync.Once
}

func (body *requestBody) Read(destination []byte) (int, error) {
	body.owner.mu.Lock()
	if body.closed {
		body.owner.mu.Unlock()
		return 0, invocationFailure
	}
	if len(destination) != 0 {
		body.started = true
	}
	body.owner.mu.Unlock()
	return body.reader.Read(destination)
}

func (body *requestBody) Close() error {
	body.owner.mu.Lock()
	defer body.owner.mu.Unlock()
	body.closeLocked()
	return nil
}

func (body *requestBody) closeLocked() {
	body.once.Do(func() {
		body.closed = true
		delete(body.owner.openBodies, body)
		if body.owner.sealed && len(body.owner.openBodies) == 0 {
			body.owner.closeDone()
		}
	})
}
