using System;

namespace HttpRetryCheck.V1.Reporting;

/// <summary>Creates, validates, and exports HTTP Retry Check reports.</summary>
public static partial class ScenarioReports
{
    /// <summary>The report format identity required by this version.</summary>
    public const string SchemaVersion = "http_retry_check.report.v2";

    /// <summary>The URI identifier of the matching JSON Schema.</summary>
    public const string SchemaId = "urn:http-retry-check:schema:report:v2";

    /// <summary>The identity of the six-scenario semantic contract.</summary>
    public const string SuiteIdentity = "http_retry_check.scenario_suite.v1";

    /// <summary>The identity of the fixed explanatory text.</summary>
    public const string ExplanationIdentity = "http_retry_check.scenario_explanations.v1";

    /// <summary>The statement that bounds what generated evidence establishes.</summary>
    public const string ClaimCeiling = "This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.";

    /// <summary>The maximum encoded size of one evidence projection, in bytes.</summary>
    public const int MaxProjectionBytes = 262144;

    /// <summary>The maximum combined size of an evidence artifact, in bytes.</summary>
    public const int MaxArtifactBytes = 1048576;

    /// <summary>Creates a report from a validated suite result.</summary>
    public static Report Create(SuiteResult result)
    {
        try
        {
            if (result is null)
            {
                throw ReportingFailure.Report();
            }
            ScenarioSuite.Validate(result);

            var rows = new ReportScenario[result.Scenarios.Count];
            uint passed = 0;
            uint failed = 0;
            uint inconclusive = 0;
            for (var index = 0; index < rows.Length; index++)
            {
                var row = result.Scenarios[index];
                var findings = new ReportFinding[row.Findings.Count];
                for (var findingIndex = 0; findingIndex < findings.Length; findingIndex++)
                {
                    var code = row.Findings[findingIndex];
                    findings[findingIndex] = new ReportFinding(code, ScenarioExplanations.FindingText(code));
                }

                var observation = row.Observation;
                rows[index] = new ReportScenario(
                    row.Scenario,
                    ScenarioExplanations.ScenarioText(row.Scenario),
                    row.Assessment,
                    ScenarioExplanations.AssessmentText(row.Assessment),
                    new ReportObservation(
                        observation.CaptureComplete,
                        observation.AttemptCount,
                        observation.AttemptLimit,
                        observation.Protocol,
                        observation.EffectCount,
                        observation.OverlapCount,
                        observation.RetryAfterEffectCount,
                        observation.RetryAfterUnconfirmedCount,
                        observation.RetryBeforeResponseCount,
                        observation.ResponseAttemptCount,
                        observation.ResponseCompleteCount,
                        observation.FirstResponseComplete,
                        observation.DelayCompleteCount,
                        observation.MethodConsistent,
                        observation.DestinationConsistent,
                        observation.BodyConsistent,
                        observation.Credential,
                        observation.Cleanup),
                    new ReportReadOnlyList<ReportFinding>(findings, takeOwnership: true));

                switch (row.Assessment)
                {
                    case Assessment.NoUnsafeBehaviorObserved:
                        passed++;
                        break;
                    case Assessment.UnsafeBehaviorObserved:
                        failed++;
                        break;
                    case Assessment.Inconclusive:
                        inconclusive++;
                        break;
                    default:
                        throw ReportingFailure.Report();
                }
            }

            var assessment = result.Assessment;
            return new Report(
                SchemaVersion,
                SuiteIdentity,
                ExplanationIdentity,
                ClaimCeiling,
                assessment,
                ScenarioExplanations.AssessmentText(assessment),
                ReportWire.OutcomeFor(assessment),
                new ReportSummary(6, passed, failed, inconclusive),
                new ReportReadOnlyList<ReportScenario>(rows, takeOwnership: true));
        }
        catch (ReportException)
        {
            throw;
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }
    }

    /// <summary>Checks the report and recalculates its findings and result.</summary>
    public static void Validate(Report report)
    {
        try
        {
            if (report is null || report.SchemaVersion != SchemaVersion || report.SuiteIdentity != SuiteIdentity ||
                report.ExplanationIdentity != ExplanationIdentity || report.ClaimCeiling != ClaimCeiling ||
                report.AssessmentText != ScenarioExplanations.AssessmentText(report.Assessment) ||
                !ReportWire.IsDefined(report.Outcome) || report.Outcome != ReportWire.OutcomeFor(report.Assessment) ||
                report.Summary is null || report.Scenarios is null || report.Scenarios.Count != 6)
            {
                throw ReportingFailure.Report();
            }

            var semanticRows = new ScenarioResult[6];
            uint passed = 0;
            uint failed = 0;
            uint inconclusive = 0;
            for (var index = 0; index < semanticRows.Length; index++)
            {
                var row = report.Scenarios[index];
                if (row is null || row.ScenarioText != ScenarioExplanations.ScenarioText(row.Scenario) ||
                    row.AssessmentText != ScenarioExplanations.AssessmentText(row.Assessment) ||
                    row.Observation is null || row.Findings is null || row.Findings.Count > 18)
                {
                    throw ReportingFailure.Report();
                }

                var findingCodes = new FindingCode[row.Findings.Count];
                for (var findingIndex = 0; findingIndex < findingCodes.Length; findingIndex++)
                {
                    var finding = row.Findings[findingIndex];
                    if (finding is null || finding.Text != ScenarioExplanations.FindingText(finding.Code))
                    {
                        throw ReportingFailure.Report();
                    }
                    findingCodes[findingIndex] = finding.Code;
                }

                semanticRows[index] = new ScenarioResult(
                    row.Scenario,
                    row.Assessment,
                    row.Observation.ToSuiteObservation(),
                    new ReportReadOnlyList<FindingCode>(findingCodes, takeOwnership: true));

                switch (row.Assessment)
                {
                    case Assessment.NoUnsafeBehaviorObserved:
                        passed++;
                        break;
                    case Assessment.UnsafeBehaviorObserved:
                        failed++;
                        break;
                    case Assessment.Inconclusive:
                        inconclusive++;
                        break;
                    default:
                        throw ReportingFailure.Report();
                }
            }

            if (report.Summary.Scenarios != 6 || report.Summary.Passed != passed ||
                report.Summary.Failed != failed || report.Summary.Inconclusive != inconclusive)
            {
                throw ReportingFailure.Report();
            }

            ScenarioSuite.Validate(new SuiteResult(
                report.Assessment,
                new ReportReadOnlyList<ScenarioResult>(semanticRows, takeOwnership: true)));
        }
        catch (ReportException)
        {
            throw;
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }
    }
}
