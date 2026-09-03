// Package report turns a validated suite result into JSON, JUnit, Markdown,
// and a portable evidence artifact.
package report

import httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"

const (
	// SchemaVersion identifies the report format.
	SchemaVersion = "http_retry_check.report.v2"
	// SchemaID identifies the report's JSON Schema.
	SchemaID = "urn:http-retry-check:schema:report:v2"
	// SuiteIdentity identifies the scenario suite used by the report.
	SuiteIdentity = "http_retry_check.scenario_suite.v1"
	// ExplanationIdentity identifies the explanation set used by the report.
	ExplanationIdentity = "http_retry_check.scenario_explanations.v1"
	// ClaimCeiling states what the evidence does and does not prove.
	ClaimCeiling = "This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations."

	// MaxProjectionBytes bounds each report, projection, manifest, or artifact file.
	MaxProjectionBytes = 256 << 10
	// MaxArtifactBytes bounds the complete four-file in-memory artifact set.
	MaxArtifactBytes = 1 << 20
)

// Outcome is the report result used by CI systems.
type Outcome string

const (
	// OutcomePass is the evidence outcome for an all-positive result.
	OutcomePass Outcome = "pass"
	// OutcomeFail is the evidence outcome for any unsafe result.
	OutcomeFail Outcome = "fail"
	// OutcomeInconclusive is the evidence outcome for an uncertain result.
	OutcomeInconclusive Outcome = "inconclusive"
)

// Summary is recomputed from the six ordered scenario rows.
type Summary struct {
	Scenarios    uint32 `json:"scenarios"`
	Passed       uint32 `json:"passed"`
	Failed       uint32 `json:"failed"`
	Inconclusive uint32 `json:"inconclusive"`
}

// Observation contains the measurements recorded for one scenario.
type Observation struct {
	CaptureComplete            bool                      `json:"capture_complete"`
	AttemptCount               uint32                    `json:"attempt_count"`
	AttemptLimit               uint32                    `json:"attempt_limit"`
	Protocol                   string                    `json:"protocol"`
	EffectCount                uint64                    `json:"effect_count"`
	OverlapCount               uint32                    `json:"overlap_count"`
	RetryAfterEffectCount      uint32                    `json:"retry_after_effect_count"`
	RetryAfterUnconfirmedCount uint32                    `json:"retry_after_unconfirmed_count"`
	RetryBeforeResponseCount   uint32                    `json:"retry_before_response_count"`
	ResponseAttemptCount       uint32                    `json:"response_attempt_count"`
	ResponseCompleteCount      uint32                    `json:"response_complete_count"`
	FirstResponseComplete      bool                      `json:"first_response_complete"`
	DelayCompleteCount         uint32                    `json:"delay_complete_count"`
	MethodConsistent           bool                      `json:"method_consistent"`
	DestinationConsistent      bool                      `json:"destination_consistent"`
	BodyConsistent             bool                      `json:"body_consistent"`
	Credential                 httpcheck.CredentialState `json:"credential"`
	Cleanup                    httpcheck.CleanupState    `json:"cleanup"`
}

// Finding pairs a finding code with its explanation.
type Finding struct {
	Code httpcheck.FindingCode `json:"code"`
	Text string                `json:"text"`
}

// Scenario contains the report data for one scenario.
type Scenario struct {
	Scenario       httpcheck.ScenarioID `json:"scenario"`
	ScenarioText   string               `json:"scenario_text"`
	Assessment     httpcheck.Assessment `json:"assessment"`
	AssessmentText string               `json:"assessment_text"`
	Observation    Observation          `json:"observation"`
	Findings       []Finding            `json:"findings"`
}

// Report contains the validated results for the complete suite.
type Report struct {
	SchemaVersion       string               `json:"schema_version"`
	SuiteIdentity       string               `json:"suite_identity"`
	ExplanationIdentity string               `json:"explanation_identity"`
	ClaimCeiling        string               `json:"claim_ceiling"`
	Assessment          httpcheck.Assessment `json:"assessment"`
	AssessmentText      string               `json:"assessment_text"`
	Outcome             Outcome              `json:"outcome"`
	Summary             Summary              `json:"summary"`
	Scenarios           []Scenario           `json:"scenarios"`
}

// ArtifactFile contains one file in an evidence artifact.
type ArtifactFile struct {
	Name      string
	MediaType string
	Contents  []byte
}
