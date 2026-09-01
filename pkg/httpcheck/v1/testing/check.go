package httpchecktest

import (
	"testing"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
)

const (
	runFailureText     = "HTTP Retry Check scenario suite could not run."
	invalidResultText  = "HTTP Retry Check scenario suite returned an invalid result."
	positiveLinePrefix = "HTTP Retry Check PASS "
	unsafeLinePrefix   = "HTTP Retry Check UNSAFE "
	inconclusivePrefix = "HTTP Retry Check INCONCLUSIVE "
)

// Check runs all six scenarios and fails the test for an unsafe or inconclusive result.
func Check(t *testing.T, doer httpcheck.Doer) {
	t.Helper()
	result, err := httpcheck.Run(t.Context(), doer)
	if err != nil {
		t.Fatal(runFailureText)
	}
	if httpcheck.Validate(result) != nil {
		t.Fatal(invalidResultText)
	}
	plan := reduceValidated(result)
	if plan.fatal != "" {
		t.Fatal(plan.fatal)
	}
	for _, line := range plan.lines {
		if line.failure {
			t.Error(line.text)
		} else {
			t.Log(line.text)
		}
	}
}

type checkLine struct {
	failure bool
	text    string
}

type checkPlan struct {
	fatal string
	lines []checkLine
}

func reduceValidated(result httpcheck.Result) checkPlan {
	plan := checkPlan{lines: make([]checkLine, 0, len(result.Scenarios))}
	positive := result.Assessment == httpcheck.AssessmentNoUnsafeBehaviorObserved
	failureLine := false
	for _, row := range result.Scenarios {
		if row.Assessment == httpcheck.AssessmentNoUnsafeBehaviorObserved {
			plan.lines = append(plan.lines, checkLine{
				text: positiveLinePrefix + string(row.Scenario),
			})
			continue
		}
		positive = false
		prefix := inconclusivePrefix
		if row.Assessment == httpcheck.AssessmentUnsafeBehaviorObserved {
			prefix = unsafeLinePrefix
		}
		for _, finding := range row.Findings {
			failureLine = true
			plan.lines = append(plan.lines, checkLine{
				failure: true,
				text: prefix + string(row.Scenario) + ": " + string(finding) + ": " +
					httpcheck.FindingText(finding),
			})
		}
	}
	if !positive && !failureLine {
		return checkPlan{fatal: invalidResultText}
	}
	return plan
}
