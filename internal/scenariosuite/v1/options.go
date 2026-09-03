package scenariosuite

import "time"

const (
	maximumOptionDuration = time.Minute
	minimumAttemptLimit   = uint32(1)
	maximumAttemptLimit   = uint32(3)
)

// Option changes one setting for a scenario-suite run.
type Option func(*dependencies)

// WithScenarioTimeout sets the maximum time allowed for each scenario.
func WithScenarioTimeout(duration time.Duration) Option {
	return func(dependency *dependencies) {
		dependency.caseTimeout = duration
	}
}

// WithConnectionTimeout sets the maximum time allowed for one connection.
func WithConnectionTimeout(duration time.Duration) Option {
	return func(dependency *dependencies) {
		dependency.connectionTimeout = duration
	}
}

// WithQuietWindow sets how long a scenario observes retries after Do returns.
func WithQuietWindow(duration time.Duration) Option {
	return func(dependency *dependencies) {
		dependency.quietWindow = duration
	}
}

// WithAttemptLimit sets the number of attempts allowed before a finding is recorded.
func WithAttemptLimit(limit uint32) Option {
	return func(dependency *dependencies) {
		dependency.attemptLimit = limit
	}
}

func applyOptions(dependency dependencies, options []Option) (dependencies, bool) {
	for _, option := range options {
		if option == nil {
			return dependencies{}, false
		}
		option(&dependency)
	}
	return dependency, validDependencies(dependency) && dependency.quietWindow > 0
}
