using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Text.Json;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Reporting;

[TestClass]
public sealed class ReportModelCorpusTests
{
    private const string Marker = "HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9";

    [TestMethod]
    public void InvalidReportModelVectorsAreRejected()
    {
        var positiveBytes = ReportingCorpusTestSupport.ReadProjection("positive", "report.json");
        var positive = ScenarioReports.Decode(positiveBytes);
        var mixed = ScenarioReports.Decode(
            ReportingCorpusTestSupport.ReadProjection("mixed", "report.json"));
        using var descriptor = JsonDocument.Parse(ReportingCorpusTestSupport.ReadInvalid("report-model.json"));
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var value in descriptor.RootElement.GetProperty("cases").EnumerateArray())
        {
            if (!value.GetProperty("representability").EnumerateArray().Any(item => item.GetString() == "csharp"))
            {
                continue;
            }
            var id = value.GetProperty("id").GetString()!;
            Assert.IsTrue(seen.Add(id), $"duplicate corpus case {id}");
            ReportingCorpusTestSupport.AssertReportFailure(() => Exercise(id, positive, mixed, positiveBytes));
        }
        Assert.AreEqual(20, seen.Count);
    }

    [TestMethod]
    public void NullAndImpossibleReportEntriesReturnTheDocumentedError()
    {
        var report = ScenarioReports.Decode(
            ReportingCorpusTestSupport.ReadProjection("positive", "report.json"));
        var row = report.Scenarios[0];
        var findingText = ScenarioExplanations.FindingText(FindingCode.AttemptNotObserved);
        ReportingCorpusTestSupport.AssertReportFailure(
            () => _ = new ReportFinding(FindingCode.AttemptNotObserved, null!));
        ReportingCorpusTestSupport.AssertReportFailure(
            () => _ = new ReportFinding((FindingCode)99, findingText));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new ReportObservation(
            false, 5, 2, string.Empty, 0, 0, 0, 0, 0, 0, 0, false, 0, true, true, true,
            CredentialState.NotObserved, CleanupState.Succeeded));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new ReportScenario(
            row.Scenario, null!, row.Assessment, row.AssessmentText, row.Observation, row.Findings));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new ReportScenario(
            row.Scenario, row.ScenarioText, row.Assessment, null!, row.Observation, row.Findings));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new ReportScenario(
            row.Scenario, row.ScenarioText, row.Assessment, row.AssessmentText, null!, row.Findings));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new ReportScenario(
            row.Scenario, row.ScenarioText, row.Assessment, row.AssessmentText, row.Observation, null!));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new Report(
            null!, report.SuiteIdentity, report.ExplanationIdentity, report.ClaimCeiling,
            report.Assessment, report.AssessmentText, report.Outcome, report.Summary, report.Scenarios));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new Report(
            report.SchemaVersion, null!, report.ExplanationIdentity, report.ClaimCeiling,
            report.Assessment, report.AssessmentText, report.Outcome, report.Summary, report.Scenarios));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new Report(
            report.SchemaVersion, report.SuiteIdentity, null!, report.ClaimCeiling,
            report.Assessment, report.AssessmentText, report.Outcome, report.Summary, report.Scenarios));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new Report(
            report.SchemaVersion, report.SuiteIdentity, report.ExplanationIdentity, null!,
            report.Assessment, report.AssessmentText, report.Outcome, report.Summary, report.Scenarios));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new Report(
            report.SchemaVersion, report.SuiteIdentity, report.ExplanationIdentity, report.ClaimCeiling,
            report.Assessment, null!, report.Outcome, report.Summary, report.Scenarios));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = new Report(
            report.SchemaVersion, report.SuiteIdentity, report.ExplanationIdentity, report.ClaimCeiling,
            report.Assessment, report.AssessmentText, report.Outcome, null!, report.Scenarios));
        ReportingCorpusTestSupport.AssertReportFailure(() => ScenarioReports.Validate(null!));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = ScenarioReports.Create(null!));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = ScenarioReports.Encode(null!));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = ScenarioReports.Decode(null!));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = ScenarioReports.JUnit(null!));
        ReportingCorpusTestSupport.AssertReportFailure(() => _ = ScenarioReports.GitHubSummary(null!));
    }

    [TestMethod]
    public void ReportConstructorsDoNotReuseInjectedArtifactErrors()
    {
        var report = ScenarioReports.Decode(
            ReportingCorpusTestSupport.ReadProjection("positive", "report.json"));
        var row = report.Scenarios[0];
        var injected = CaptureReportException(() => _ = ScenarioReports.BuildArtifact(null!));
        injected.HelpLink = Marker;
        injected.Data[Marker] = Marker;

        var rowFailure = CaptureReportException(() => _ = new ReportScenario(
            row.Scenario,
            row.ScenarioText,
            row.Assessment,
            row.AssessmentText,
            row.Observation,
            new ThrowingReadOnlyList<ReportFinding>(injected)));
        AssertFreshReportFailure(injected, rowFailure);

        var reportFailure = CaptureReportException(() => _ = new Report(
            report.SchemaVersion,
            report.SuiteIdentity,
            report.ExplanationIdentity,
            report.ClaimCeiling,
            report.Assessment,
            report.AssessmentText,
            report.Outcome,
            report.Summary,
            new ThrowingReadOnlyList<ReportScenario>(injected)));
        AssertFreshReportFailure(injected, reportFailure);
    }

    private static void Exercise(string id, Report positive, Report mixed, byte[] positiveBytes)
    {
        switch (id)
        {
            case "report_outcome_zero":
                _ = CopyReport(positive, outcome: (Outcome)0);
                return;
            case "report_outcome_unknown":
                _ = CopyReport(positive, outcome: (Outcome)99);
                return;
            case "report_outcome_successor":
                _ = CopyReport(positive, outcome: (Outcome)4);
                return;
            case "report_schema_drift":
                _ = CopyReport(positive, schemaVersion: "http_retry_check.report.v1");
                return;
            case "report_suite_drift":
                _ = CopyReport(positive, suiteIdentity: "http_retry_check.scenario_suite.v2");
                return;
            case "report_explanation_identity_drift":
                _ = CopyReport(positive, explanationIdentity: "http_retry_check.scenario_explanations.v2");
                return;
            case "report_claim_ceiling_drift":
                _ = CopyReport(positive, claimCeiling: Marker);
                return;
            case "report_scenario_text_drift":
                _ = CopyScenario(positive.Scenarios[0], scenarioText: Marker);
                return;
            case "report_assessment_text_drift":
                _ = CopyReport(positive, assessmentText: Marker);
                return;
            case "report_row_assessment_text_drift":
                _ = CopyScenario(positive.Scenarios[0], assessmentText: Marker);
                return;
            case "report_finding_text_drift":
                _ = new ReportFinding(mixed.Scenarios[5].Findings[0].Code, Marker);
                return;
            case "report_aggregate_drift":
                _ = CopyReport(
                    positive,
                    assessment: Assessment.Inconclusive,
                    assessmentText: ScenarioExplanations.AssessmentText(Assessment.Inconclusive),
                    outcome: Outcome.Inconclusive);
                return;
            case "report_summary_drift":
                _ = new ReportSummary(6, 5, 0, 0);
                return;
            case "report_scenarios_null":
                _ = new Report(
                    positive.SchemaVersion,
                    positive.SuiteIdentity,
                    positive.ExplanationIdentity,
                    positive.ClaimCeiling,
                    positive.Assessment,
                    positive.AssessmentText,
                    positive.Outcome,
                    positive.Summary,
                    null!);
                return;
            case "report_findings_null":
                _ = new ReportScenario(
                    positive.Scenarios[0].Scenario,
                    positive.Scenarios[0].ScenarioText,
                    positive.Scenarios[0].Assessment,
                    positive.Scenarios[0].AssessmentText,
                    positive.Scenarios[0].Observation,
                    null!);
                return;
            case "report_nested_missing":
                _ = new ReportScenario(
                    positive.Scenarios[0].Scenario,
                    positive.Scenarios[0].ScenarioText,
                    positive.Scenarios[0].Assessment,
                    positive.Scenarios[0].AssessmentText,
                    null!,
                    positive.Scenarios[0].Findings);
                return;
            case "report_nested_duplicate":
                var text = Encoding.UTF8.GetString(positiveBytes);
                const string assessment = "      \"assessment\": \"no_unsafe_behavior_observed\",\n";
                var duplicate = text.Replace(assessment, assessment + assessment, StringComparison.Ordinal);
                _ = ScenarioReports.Decode(Encoding.UTF8.GetBytes(duplicate));
                return;
            case "report_rows_reordered":
                var rows = positive.Scenarios.ToArray();
                (rows[0], rows[1]) = (rows[1], rows[0]);
                _ = CopyReport(positive, scenarios: rows);
                return;
            case "report_findings_reordered":
                var findings = mixed.Scenarios[5].Findings.Reverse().ToArray();
                _ = CopyScenario(mixed.Scenarios[5], findings: findings);
                return;
            case "report_constructor_invalid_result":
                _ = new ReportScenario(
                    positive.Scenarios[0].Scenario,
                    positive.Scenarios[0].ScenarioText,
                    Assessment.Inconclusive,
                    ScenarioExplanations.AssessmentText(Assessment.Inconclusive),
                    positive.Scenarios[0].Observation,
                    new[]
                    {
                        new ReportFinding(
                            FindingCode.ScenarioIncomplete,
                            ScenarioExplanations.FindingText(FindingCode.ScenarioIncomplete)),
                    });
                return;
            default:
                throw new InvalidOperationException($"uncovered corpus case {id}");
        }
    }

    private static Report CopyReport(
        Report source,
        string? schemaVersion = null,
        string? suiteIdentity = null,
        string? explanationIdentity = null,
        string? claimCeiling = null,
        Assessment? assessment = null,
        string? assessmentText = null,
        Outcome? outcome = null,
        ReportSummary? summary = null,
        IReadOnlyList<ReportScenario>? scenarios = null) => new(
            schemaVersion ?? source.SchemaVersion,
            suiteIdentity ?? source.SuiteIdentity,
            explanationIdentity ?? source.ExplanationIdentity,
            claimCeiling ?? source.ClaimCeiling,
            assessment ?? source.Assessment,
            assessmentText ?? source.AssessmentText,
            outcome ?? source.Outcome,
            summary ?? source.Summary,
            scenarios ?? source.Scenarios);

    private static ReportScenario CopyScenario(
        ReportScenario source,
        string? scenarioText = null,
        string? assessmentText = null,
        ReportObservation? observation = null,
        IReadOnlyList<ReportFinding>? findings = null) => new(
            source.Scenario,
            scenarioText ?? source.ScenarioText,
            source.Assessment,
            assessmentText ?? source.AssessmentText,
            observation ?? source.Observation,
            findings ?? source.Findings);

    private static ReportException CaptureReportException(Action operation)
    {
        try
        {
            operation();
        }
        catch (ReportException exception)
        {
            return exception;
        }

        Assert.Fail("Expected a fixed ReportException.");
        throw new InvalidOperationException("unreachable assertion state");
    }

    private static void AssertFreshReportFailure(ReportException injected, ReportException actual)
    {
        Assert.AreNotSame(injected, actual);
        Assert.AreEqual("HTTP retry scenario suite report is invalid", actual.Message);
        Assert.IsNull(actual.HelpLink);
        Assert.IsNull(actual.InnerException);
        Assert.AreEqual(0, actual.Data.Count);
        Assert.IsNull(actual.StackTrace);
        Assert.AreEqual(actual.Message, actual.ToString());
    }

    private sealed class ThrowingReadOnlyList<T> : IReadOnlyList<T>
    {
        private readonly Exception failure;

        internal ThrowingReadOnlyList(Exception failure)
        {
            this.failure = failure;
        }

        public int Count => throw failure;

        public T this[int index] => throw failure;

        public IEnumerator<T> GetEnumerator() => throw failure;

        System.Collections.IEnumerator System.Collections.IEnumerable.GetEnumerator() => throw failure;
    }
}
