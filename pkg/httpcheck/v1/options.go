package httpcheck

import (
	"time"

	scenariosuite "github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1"
)

type options struct {
	suiteOptions []scenariosuite.Option
}

// Option changes one setting for a Run call.
type Option func(*options)

// WithScenarioTimeout sets the maximum time allowed for each scenario.
// The duration must be positive and no greater than one minute.
func WithScenarioTimeout(duration time.Duration) Option {
	return func(value *options) {
		value.suiteOptions = append(value.suiteOptions, scenariosuite.WithScenarioTimeout(duration))
	}
}

// WithConnectionTimeout sets the maximum time allowed for one connection.
// The duration must be positive and no greater than one minute.
func WithConnectionTimeout(duration time.Duration) Option {
	return func(value *options) {
		value.suiteOptions = append(value.suiteOptions, scenariosuite.WithConnectionTimeout(duration))
	}
}

// WithQuietWindow sets how long a scenario observes retries after Do returns.
// The duration must be positive and no greater than one minute.
func WithQuietWindow(duration time.Duration) Option {
	return func(value *options) {
		value.suiteOptions = append(value.suiteOptions, scenariosuite.WithQuietWindow(duration))
	}
}

// WithAttemptLimit sets the number of attempts allowed before a finding is recorded.
// The limit must be between one and three.
func WithAttemptLimit(limit uint32) Option {
	return func(value *options) {
		value.suiteOptions = append(value.suiteOptions, scenariosuite.WithAttemptLimit(limit))
	}
}

func applyOptions(values []Option) ([]scenariosuite.Option, bool) {
	configured := options{suiteOptions: make([]scenariosuite.Option, 0, len(values))}
	for _, option := range values {
		if option == nil {
			return nil, false
		}
		option(&configured)
	}
	return configured.suiteOptions, true
}
