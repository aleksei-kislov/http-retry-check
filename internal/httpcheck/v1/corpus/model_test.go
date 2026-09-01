package corpus

const (
	rowBundleIdentity         = "http_retry_check.conformance_rows.v1"
	resultBundleIdentity      = "http_retry_check.conformance_results.v1"
	invalidBundleIdentity     = "http_retry_check.conformance_invalid.v1"
	manifestIdentity          = "http_retry_check.conformance_corpus_manifest.v1"
	productIdentity           = "http_retry_check.v1"
	suiteIdentity             = "http_retry_check.scenario_suite.v1"
	reportIdentity            = "http_retry_check.report.v1"
	conformanceIdentity       = "http_retry_check.conformance.v1"
	explanationIdentity       = "http_retry_check.scenario_explanations.v1"
	captureBundleIdentity     = "http_retry_check.capture_corpus.v1"
	claimCeiling              = "This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations."
	artifactIdentity          = "http_retry_check.artifact_manifest.v1"
	artifactDigestDomain      = "http_retry_check.artifact_set.v1"
	markerValue               = "HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9"
	maxCaptureCorpusWireBytes = (64 << 10) + (1 << 20) + 1
)

type rowBundle struct {
	SchemaVersion string    `json:"schema_version"`
	Category      string    `json:"category"`
	Cases         []rowCase `json:"cases"`
}

type rowCase struct {
	ID               string      `json:"id"`
	Representability []string    `json:"representability"`
	Scenario         string      `json:"scenario"`
	Observation      observation `json:"observation"`
	Assessment       string      `json:"assessment"`
	Findings         []string    `json:"findings"`
	Coverage         []string    `json:"coverage"`
}

type resultBundle struct {
	SchemaVersion string       `json:"schema_version"`
	Category      string       `json:"category"`
	Cases         []resultCase `json:"cases"`
}

type resultCase struct {
	ID               string   `json:"id"`
	Representability []string `json:"representability"`
	Assessment       string   `json:"assessment"`
	Rows             []string `json:"rows"`
	Coverage         []string `json:"coverage"`
}

type invalidBundle struct {
	SchemaVersion string        `json:"schema_version"`
	Category      string        `json:"category"`
	Marker        string        `json:"marker,omitempty"`
	Cases         []invalidCase `json:"cases"`
}

type invalidCase struct {
	ID               string   `json:"id"`
	Expected         string   `json:"expected"`
	Representability []string `json:"representability"`
	BaseCase         string   `json:"base_case,omitempty"`
	Operation        string   `json:"operation"`
	Target           string   `json:"target,omitempty"`
	Operand          string   `json:"operand,omitempty"`
	Count            uint64   `json:"count,omitempty"`
	Coverage         []string `json:"coverage"`
}

type corpusManifest struct {
	SchemaVersion       string       `json:"schema_version"`
	ProductIdentity     string       `json:"product_identity"`
	SuiteIdentity       string       `json:"suite_identity"`
	ReportIdentity      string       `json:"report_identity"`
	ConformanceIdentity string       `json:"conformance_identity"`
	Files               []corpusFile `json:"files"`
}

type corpusFile struct {
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256"`
}

type captureBundle struct {
	SchemaVersion string        `json:"schema_version"`
	Endpoint      string        `json:"endpoint"`
	Cases         []captureCase `json:"cases"`
}

type captureCase struct {
	ID         string             `json:"id"`
	WireBase64 string             `json:"wire_base64"`
	Expected   captureExpectation `json:"expected"`
}

type captureExpectation struct {
	HeadersObserved       bool `json:"headers_observed"`
	Complete              bool `json:"complete"`
	CaptureComplete       bool `json:"capture_complete"`
	MethodConsistent      bool `json:"method_consistent"`
	DestinationConsistent bool `json:"destination_consistent"`
	BodyConsistent        bool `json:"body_consistent"`
	CredentialExact       bool `json:"credential_exact"`
	CredentialExposed     bool `json:"credential_exposed"`
}

type neutralReport struct {
	SchemaVersion       string             `json:"schema_version"`
	SuiteIdentity       string             `json:"suite_identity"`
	ExplanationIdentity string             `json:"explanation_identity"`
	ClaimCeiling        string             `json:"claim_ceiling"`
	Assessment          string             `json:"assessment"`
	AssessmentText      string             `json:"assessment_text"`
	Outcome             string             `json:"outcome"`
	Summary             neutralSummary     `json:"summary"`
	Scenarios           []neutralReportRow `json:"scenarios"`
}

type neutralSummary struct {
	Scenarios    uint32 `json:"scenarios"`
	Passed       uint32 `json:"passed"`
	Failed       uint32 `json:"failed"`
	Inconclusive uint32 `json:"inconclusive"`
}

type neutralReportRow struct {
	Scenario       string           `json:"scenario"`
	ScenarioText   string           `json:"scenario_text"`
	Assessment     string           `json:"assessment"`
	AssessmentText string           `json:"assessment_text"`
	Observation    observation      `json:"observation"`
	Findings       []neutralFinding `json:"findings"`
}

type neutralFinding struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

type artifactManifest struct {
	SchemaVersion       string               `json:"schema_version"`
	ReportSchemaVersion string               `json:"report_schema_version"`
	ClaimCeiling        string               `json:"claim_ceiling"`
	DigestDomain        string               `json:"digest_domain"`
	AggregateSHA256     string               `json:"aggregate_sha256"`
	Files               []artifactDescriptor `json:"files"`
}

type artifactDescriptor struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	SizeBytes uint64 `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

func scenarioText(value string) string {
	switch value {
	case scenarios[0]:
		return "Tests whether the client retries after the origin accepts a request and disconnects before responding."
	case scenarios[1]:
		return "Tests whether the client retries after the connection ends before request acceptance can be confirmed."
	case scenarios[2]:
		return "Tests whether a retry changes the body after the origin accepts a request and disconnects."
	case scenarios[3]:
		return "Tests redirect behavior and whether the synthetic credential reaches a different controlled origin."
	case scenarios[4]:
		return "Tests how many attempts the client makes after a controlled service-unavailable response."
	case scenarios[5]:
		return "Tests whether the client retries while an accepted request is waiting for a delayed response."
	default:
		return "Unknown HTTP client scenario."
	}
}

func assessmentText(value string) string {
	switch value {
	case assessmentPositive:
		return "No unsafe HTTP behavior was observed."
	case assessmentUnsafe:
		return "Unsafe HTTP behavior was observed."
	case assessmentInconclusive:
		return "The result is inconclusive."
	default:
		return "Unknown HTTP client scenario assessment."
	}
}

func findingText(value string) string {
	switch value {
	case findingOrder[0]:
		return "No request reached the test server."
	case findingOrder[1]:
		return "The test did not capture a complete scenario result."
	case findingOrder[2]:
		return "The required response attempt or completion was not observed."
	case findingOrder[3]:
		return "The test server did not finish its delayed-response phase."
	case findingOrder[4]:
		return "The client made more than two attempts."
	case findingOrder[5]:
		return "The client retried while an earlier response was still pending."
	case findingOrder[6]:
		return "The client retried after the server accepted the request."
	case findingOrder[7]:
		return "The client retried without knowing whether the server accepted the earlier request."
	case findingOrder[8]:
		return "The request method did not match the expected method."
	case findingOrder[9]:
		return "The request was sent to an unexpected destination."
	case findingOrder[10]:
		return "A request body differed from the original."
	case findingOrder[11]:
		return "The test could not determine where the synthetic credential was sent."
	case findingOrder[12]:
		return "The original request did not contain exactly one expected synthetic Authorization value."
	case findingOrder[13]:
		return "The synthetic Authorization value reached the redirect target."
	case findingOrder[14]:
		return "The test did not observe the expected server-side effect."
	case findingOrder[15]:
		return "The test server recorded more effects than this scenario allows."
	case findingOrder[16]:
		return "The test could not verify that all scenario resources were cleaned up."
	case findingOrder[17]:
		return "The scenario did not collect enough evidence to pass."
	default:
		return "Unknown HTTP client scenario finding."
	}
}
