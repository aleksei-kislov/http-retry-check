// Package evidence owns the bounded, deterministic, runtime-independent v1
// HTTP Retry Check evidence codec used by the command-line process boundary.
//
// It deliberately does not import either native HTTP runtime. Its validation
// is an independent reconstruction of the frozen language-neutral semantics.
package evidence

const (
	SchemaVersion       = "http_retry_check.report.v2"
	SuiteIdentity       = "http_retry_check.scenario_suite.v1"
	ExplanationIdentity = "http_retry_check.scenario_explanations.v1"
	ClaimCeiling        = "This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations."

	ArtifactManifestSchema = "http_retry_check.artifact_manifest.v1"
	ArtifactDigestDomain   = "http_retry_check.artifact_set.v1"

	MaxProjectionBytes = 256 << 10
	MaxArtifactBytes   = 1 << 20
)

const (
	OutcomePass         = "pass"
	OutcomeFail         = "fail"
	OutcomeInconclusive = "inconclusive"
)

type Summary struct {
	Scenarios    uint32 `json:"scenarios"`
	Passed       uint32 `json:"passed"`
	Failed       uint32 `json:"failed"`
	Inconclusive uint32 `json:"inconclusive"`
}

type Observation struct {
	CaptureComplete            bool   `json:"capture_complete"`
	AttemptCount               uint32 `json:"attempt_count"`
	AttemptLimit               uint32 `json:"attempt_limit"`
	Protocol                   string `json:"protocol"`
	EffectCount                uint64 `json:"effect_count"`
	OverlapCount               uint32 `json:"overlap_count"`
	RetryAfterEffectCount      uint32 `json:"retry_after_effect_count"`
	RetryAfterUnconfirmedCount uint32 `json:"retry_after_unconfirmed_count"`
	RetryBeforeResponseCount   uint32 `json:"retry_before_response_count"`
	ResponseAttemptCount       uint32 `json:"response_attempt_count"`
	ResponseCompleteCount      uint32 `json:"response_complete_count"`
	FirstResponseComplete      bool   `json:"first_response_complete"`
	DelayCompleteCount         uint32 `json:"delay_complete_count"`
	MethodConsistent           bool   `json:"method_consistent"`
	DestinationConsistent      bool   `json:"destination_consistent"`
	BodyConsistent             bool   `json:"body_consistent"`
	Credential                 string `json:"credential"`
	Cleanup                    string `json:"cleanup"`
}

type Finding struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

type Scenario struct {
	Scenario       string      `json:"scenario"`
	ScenarioText   string      `json:"scenario_text"`
	Assessment     string      `json:"assessment"`
	AssessmentText string      `json:"assessment_text"`
	Observation    Observation `json:"observation"`
	Findings       []Finding   `json:"findings"`
}

type Report struct {
	SchemaVersion       string     `json:"schema_version"`
	SuiteIdentity       string     `json:"suite_identity"`
	ExplanationIdentity string     `json:"explanation_identity"`
	ClaimCeiling        string     `json:"claim_ceiling"`
	Assessment          string     `json:"assessment"`
	AssessmentText      string     `json:"assessment_text"`
	Outcome             string     `json:"outcome"`
	Summary             Summary    `json:"summary"`
	Scenarios           []Scenario `json:"scenarios"`
}

type ArtifactFile struct {
	Name      string
	MediaType string
	Contents  []byte
}

type evidenceError uint8

const (
	invalidReport evidenceError = iota + 1
	invalidArtifact
)

func (failure evidenceError) Error() string {
	switch failure {
	case invalidReport:
		return "HTTP retry scenario suite report is invalid"
	case invalidArtifact:
		return "HTTP retry scenario suite artifact is invalid"
	default:
		return "HTTP retry scenario suite evidence failure is invalid"
	}
}
