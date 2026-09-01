package scenariosuite

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

const (
	syntheticBodyText        = "{\"http_retry_check\":\"scenario-suite-original\"}\n"
	changedSyntheticBodyText = "{\"http_retry_check\":\"scenario-suite-modified\"}\n"
	syntheticCredential      = "Bearer http-retry-check-synthetic-scenario-suite-v1"
	controlledPath           = "/case"
	maxObservedAttempts      = 3

	defaultCaseTimeout       = 5 * time.Second
	defaultConnectionTimeout = 2 * time.Second
	defaultCleanupTimeout    = 2 * time.Second
	delayedResponseDuration  = 250 * time.Millisecond
)

type dependencies struct {
	listen            func(context.Context, string, string) (net.Listener, error)
	caseTimeout       time.Duration
	connectionTimeout time.Duration
	cleanupTimeout    time.Duration
	delay             func(context.Context) bool
}

func productionDependencies() dependencies {
	return dependencies{
		listen: func(ctx context.Context, network, address string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, network, address)
		},
		caseTimeout:       defaultCaseTimeout,
		connectionTimeout: defaultConnectionTimeout,
		cleanupTimeout:    defaultCleanupTimeout,
		delay: func(ctx context.Context) bool {
			timer := time.NewTimer(delayedResponseDuration)
			defer timer.Stop()
			select {
			case <-timer.C:
				return true
			case <-ctx.Done():
				return false
			}
		},
	}
}

type admittedScenario struct {
	scenario  ScenarioID
	listeners []net.Listener
}

func run(parent context.Context, doer Doer, dependency dependencies) (Result, error) {
	if parent == nil || doer == nil || parent.Err() != nil || !validDependencies(dependency) {
		return Result{}, ErrInvalidCall
	}

	admitted, admissionFailure := admitSuite(parent, dependency)
	if admissionFailure != 0 {
		return Result{}, admissionFailure
	}
	rows := make([]ScenarioResult, 0, len(orderedScenarios()))
	stop := false
	for _, item := range admitted {
		if stop {
			closed := closeAdmitted([]admittedScenario{item})
			row := unavailableRow(item.scenario, cleanupState(closed))
			rows = append(rows, row)
			continue
		}
		observation, settled := executeScenario(parent, doer, item, dependency)
		row, valid := newScenarioResult(item.scenario, observation)
		if !valid {
			row = unavailableRow(item.scenario, observation.Cleanup)
			stop = true
		}
		rows = append(rows, row)
		if !settled {
			stop = true
		}
	}
	result, valid := newResult(rows)
	if !valid {
		return unavailableResult(), nil
	}
	return result, nil
}

func validDependencies(dependency dependencies) bool {
	return dependency.listen != nil && dependency.delay != nil && dependency.caseTimeout > 0 &&
		dependency.connectionTimeout > 0 && dependency.cleanupTimeout > 0
}

func admitSuite(ctx context.Context, dependency dependencies) ([]admittedScenario, RunError) {
	order := orderedScenarios()
	admitted := make([]admittedScenario, 0, len(order))
	seen := make(map[netip.AddrPort]struct{}, 7)
	for _, scenario := range order {
		count := 1
		if scenario == ScenarioCrossOriginRedirectCredentials {
			count = 2
		}
		item := admittedScenario{scenario: scenario, listeners: make([]net.Listener, 0, count)}
		for range count {
			listener, err := dependency.listen(ctx, "tcp4", "127.0.0.1:0")
			address, valid := listenerAddress(listener)
			valid = err == nil && valid
			duplicate := false
			if valid {
				_, duplicate = seen[address]
				if !duplicate {
					seen[address] = struct{}{}
				}
			}
			if !valid || duplicate {
				closed := true
				if listener != nil {
					closed = closeListener(listener)
				}
				closed = closeAdmitted(admitted) && closed
				closed = closeAdmitted([]admittedScenario{item}) && closed
				if !closed {
					return nil, ErrInternalFailure
				}
				return nil, ErrSuiteUnavailable
			}
			item.listeners = append(item.listeners, listener)
		}
		admitted = append(admitted, item)
	}
	return admitted, 0
}

func listenerAddress(listener net.Listener) (netip.AddrPort, bool) {
	if listener == nil {
		return netip.AddrPort{}, false
	}
	listenerAddress := listener.Addr()
	if listenerAddress == nil {
		return netip.AddrPort{}, false
	}
	address, err := netip.ParseAddrPort(listenerAddress.String())
	return address, err == nil && address.Addr() == netip.MustParseAddr("127.0.0.1") &&
		address.Port() != 0
}

func closeAdmitted(items []admittedScenario) bool {
	closed := true
	for _, item := range items {
		for _, listener := range item.listeners {
			if listener != nil && !closeListener(listener) {
				closed = false
			}
		}
	}
	return closed
}

func closeListener(listener net.Listener) bool {
	err := listener.Close()
	return err == nil || errors.Is(err, net.ErrClosed)
}

func executeScenario(
	parent context.Context,
	doer Doer,
	item admittedScenario,
	dependency dependencies,
) (Observation, bool) {
	caseContext, cancelCase := context.WithTimeout(parent, dependency.caseTimeout)
	defer cancelCase()
	origin, ok := newScenarioOrigin(
		item.scenario, item.listeners, dependency.connectionTimeout, dependency.delay, cancelCase,
	)
	if !ok {
		closed := closeAdmitted([]admittedScenario{item})
		return unavailableObservation(cleanupState(closed)), false
	}
	origin.start(caseContext)
	request, bodies, ok := newScenarioRequest(caseContext, origin.sourceTarget(), item.scenario)
	invocation := invocationOutcome{bodiesQuiesced: true}
	if ok {
		invocation = invokeDoer(caseContext, doer, request, bodies)
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), dependency.cleanupTimeout)
	cleanupSucceeded := origin.close(cleanupContext)
	cancelCleanup()
	observation := origin.snapshot()
	if !cleanupSucceeded || !invocation.bodiesQuiesced {
		observation.Cleanup = CleanupFailed
	}
	invocationAccepted := acceptedInvocation(item.scenario, observation, invocation.completion)
	if !invocationAccepted || !invocation.bodiesQuiesced {
		observation.CaptureComplete = false
	}
	caseEnded := caseContext.Err() != nil
	if caseEnded {
		observation.CaptureComplete = false
	}
	settled := cleanupSucceeded && invocation.bodiesQuiesced && invocationAccepted &&
		!caseEnded && parent.Err() == nil
	return observation, settled
}

func acceptedInvocation(
	scenario ScenarioID,
	observation Observation,
	completion invocationCompletion,
) bool {
	if completion == invocationResponseCompletion {
		return true
	}
	if completion != invocationErrorCompletion {
		return false
	}
	switch scenario {
	case ScenarioAcceptThenDisconnect, ScenarioDisconnectBeforeAcceptance, ScenarioChangedBodyRetry:
		return observation.AttemptCount != 0
	case ScenarioDelayedResponse:
		return exactDelayedErrorObservation(observation)
	default:
		return false
	}
}

func exactDelayedErrorObservation(observation Observation) bool {
	return observation.CaptureComplete &&
		observation.AttemptCount == 1 &&
		observation.EffectCount == 1 &&
		observation.OverlapCount == 0 &&
		observation.RetryAfterEffectCount == 0 &&
		observation.RetryAfterUnconfirmedCount == 0 &&
		observation.RetryBeforeResponseCount == 0 &&
		observation.ResponseAttemptCount == 1 &&
		observation.ResponseCompleteCount <= 1 &&
		observation.FirstResponseComplete == (observation.ResponseCompleteCount == 1) &&
		observation.DelayCompleteCount == 1 &&
		observation.MethodConsistent &&
		observation.DestinationConsistent &&
		observation.BodyConsistent &&
		observation.Credential == CredentialSourceOnly &&
		observation.Cleanup == CleanupSucceeded
}

func cleanupState(succeeded bool) CleanupState {
	if succeeded {
		return CleanupSucceeded
	}
	return CleanupFailed
}

func unavailableObservation(cleanup CleanupState) Observation {
	return Observation{
		CaptureComplete: false, MethodConsistent: true, DestinationConsistent: true,
		BodyConsistent: true, Credential: CredentialNotObserved, Cleanup: cleanup,
	}
}

func unavailableRow(scenario ScenarioID, cleanup CleanupState) ScenarioResult {
	observation := unavailableObservation(cleanup)
	assessment, findings := assess(scenario, observation)
	return ScenarioResult{
		Scenario: scenario, Assessment: assessment, Observation: observation, Findings: findings,
	}
}

func unavailableResult() Result {
	order := orderedScenarios()
	rows := make([]ScenarioResult, 0, len(order))
	for _, scenario := range order {
		rows = append(rows, unavailableRow(scenario, CleanupFailed))
	}
	return Result{Assessment: AssessmentInconclusive, Scenarios: rows}
}
