using System;
using System.Collections.Generic;

namespace HttpRetryCheck.V1.Reporting;

/// <summary>Contains the outcome counts for a six-scenario report.</summary>
public sealed class ReportSummary
{
    /// <summary>Creates and validates the report counts.</summary>
    public ReportSummary(uint scenarios, uint passed, uint failed, uint inconclusive)
    {
        if (scenarios != 6 || passed > 6 || failed > 6 || inconclusive > 6 ||
            (ulong)passed + failed + inconclusive != scenarios)
        {
            throw ReportingFailure.Report();
        }

        Scenarios = scenarios;
        Passed = passed;
        Failed = failed;
        Inconclusive = inconclusive;
    }

    public uint Scenarios { get; }
    public uint Passed { get; }
    public uint Failed { get; }
    public uint Inconclusive { get; }
}

/// <summary>Contains the facts recorded for one scenario.</summary>
public sealed class ReportObservation
{
    /// <summary>Creates and validates a report observation.</summary>
    public ReportObservation(
        bool captureComplete,
        uint attemptCount,
        ulong effectCount,
        uint overlapCount,
        uint retryAfterEffectCount,
        uint retryAfterUnconfirmedCount,
        uint retryBeforeResponseCount,
        uint responseAttemptCount,
        uint responseCompleteCount,
        bool firstResponseComplete,
        uint delayCompleteCount,
        bool methodConsistent,
        bool destinationConsistent,
        bool bodyConsistent,
        CredentialState credential,
        CleanupState cleanup)
    {
        try
        {
            _ = new Observation(
                captureComplete,
                attemptCount,
                effectCount,
                overlapCount,
                retryAfterEffectCount,
                retryAfterUnconfirmedCount,
                retryBeforeResponseCount,
                responseAttemptCount,
                responseCompleteCount,
                firstResponseComplete,
                delayCompleteCount,
                methodConsistent,
                destinationConsistent,
                bodyConsistent,
                credential,
                cleanup);
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }

        CaptureComplete = captureComplete;
        AttemptCount = attemptCount;
        EffectCount = effectCount;
        OverlapCount = overlapCount;
        RetryAfterEffectCount = retryAfterEffectCount;
        RetryAfterUnconfirmedCount = retryAfterUnconfirmedCount;
        RetryBeforeResponseCount = retryBeforeResponseCount;
        ResponseAttemptCount = responseAttemptCount;
        ResponseCompleteCount = responseCompleteCount;
        FirstResponseComplete = firstResponseComplete;
        DelayCompleteCount = delayCompleteCount;
        MethodConsistent = methodConsistent;
        DestinationConsistent = destinationConsistent;
        BodyConsistent = bodyConsistent;
        Credential = credential;
        Cleanup = cleanup;
    }

    public bool CaptureComplete { get; }
    public uint AttemptCount { get; }
    public ulong EffectCount { get; }
    public uint OverlapCount { get; }
    public uint RetryAfterEffectCount { get; }
    public uint RetryAfterUnconfirmedCount { get; }
    public uint RetryBeforeResponseCount { get; }
    public uint ResponseAttemptCount { get; }
    public uint ResponseCompleteCount { get; }
    public bool FirstResponseComplete { get; }
    public uint DelayCompleteCount { get; }
    public bool MethodConsistent { get; }
    public bool DestinationConsistent { get; }
    public bool BodyConsistent { get; }
    public CredentialState Credential { get; }
    public CleanupState Cleanup { get; }

    internal Observation ToSuiteObservation() => new(
        CaptureComplete,
        AttemptCount,
        EffectCount,
        OverlapCount,
        RetryAfterEffectCount,
        RetryAfterUnconfirmedCount,
        RetryBeforeResponseCount,
        ResponseAttemptCount,
        ResponseCompleteCount,
        FirstResponseComplete,
        DelayCompleteCount,
        MethodConsistent,
        DestinationConsistent,
        BodyConsistent,
        Credential,
        Cleanup);
}

/// <summary>Pairs a finding code with its description.</summary>
public sealed class ReportFinding
{
    /// <summary>Creates and validates a report finding.</summary>
    public ReportFinding(FindingCode code, string text)
    {
        if (text is null || text != ScenarioExplanations.FindingText(code) ||
            code is < FindingCode.AttemptNotObserved or > FindingCode.ScenarioIncomplete)
        {
            throw ReportingFailure.Report();
        }

        Code = code;
        Text = text;
    }

    public FindingCode Code { get; }
    public string Text { get; }
}

/// <summary>Contains the report data for one scenario.</summary>
public sealed class ReportScenario
{
    /// <summary>Creates and validates a scenario row.</summary>
    public ReportScenario(
        ScenarioId scenario,
        string scenarioText,
        Assessment assessment,
        string assessmentText,
        ReportObservation observation,
        IReadOnlyList<ReportFinding> findings)
    {
        try
        {
            if (scenarioText is null || assessmentText is null || observation is null || findings is null ||
                scenarioText != ScenarioExplanations.ScenarioText(scenario) ||
                assessmentText != ScenarioExplanations.AssessmentText(assessment) || findings.Count > 18)
            {
                throw ReportingFailure.Report();
            }

            var reportFindings = new ReportFinding[findings.Count];
            var semanticFindings = new FindingCode[findings.Count];
            for (var index = 0; index < findings.Count; index++)
            {
                var finding = findings[index];
                if (finding is null)
                {
                    throw ReportingFailure.Report();
                }
                reportFindings[index] = finding;
                semanticFindings[index] = finding.Code;
            }

            _ = new ScenarioResult(
                scenario,
                assessment,
                observation.ToSuiteObservation(),
                new ReportReadOnlyList<FindingCode>(semanticFindings, takeOwnership: true));

            Scenario = scenario;
            ScenarioText = scenarioText;
            Assessment = assessment;
            AssessmentText = assessmentText;
            Observation = observation;
            Findings = new ReportReadOnlyList<ReportFinding>(reportFindings, takeOwnership: true);
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }
    }

    public ScenarioId Scenario { get; }
    public string ScenarioText { get; }
    public Assessment Assessment { get; }
    public string AssessmentText { get; }
    public ReportObservation Observation { get; }
    public IReadOnlyList<ReportFinding> Findings { get; }

    internal ScenarioResult ToSuiteScenario()
    {
        var findings = new FindingCode[Findings.Count];
        for (var index = 0; index < findings.Length; index++)
        {
            findings[index] = Findings[index].Code;
        }
        return new ScenarioResult(
            Scenario,
            Assessment,
            Observation.ToSuiteObservation(),
            new ReportReadOnlyList<FindingCode>(findings, takeOwnership: true));
    }
}

/// <summary>Contains the complete six-scenario evidence report.</summary>
public sealed class Report
{
    /// <summary>Creates and validates a report.</summary>
    public Report(
        string schemaVersion,
        string suiteIdentity,
        string explanationIdentity,
        string claimCeiling,
        Assessment assessment,
        string assessmentText,
        Outcome outcome,
        ReportSummary summary,
        IReadOnlyList<ReportScenario> scenarios)
    {
        try
        {
            if (schemaVersion is null || suiteIdentity is null || explanationIdentity is null || claimCeiling is null ||
                assessmentText is null || summary is null || scenarios is null ||
                schemaVersion != ScenarioReports.SchemaVersion || suiteIdentity != ScenarioReports.SuiteIdentity ||
                explanationIdentity != ScenarioReports.ExplanationIdentity || claimCeiling != ScenarioReports.ClaimCeiling ||
                assessmentText != ScenarioExplanations.AssessmentText(assessment) ||
                !ReportWire.IsDefined(outcome) || outcome != ReportWire.OutcomeFor(assessment) || scenarios.Count != 6)
            {
                throw ReportingFailure.Report();
            }

            var reportScenarios = new ReportScenario[scenarios.Count];
            var semanticScenarios = new ScenarioResult[scenarios.Count];
            uint passed = 0;
            uint failed = 0;
            uint inconclusive = 0;
            for (var index = 0; index < scenarios.Count; index++)
            {
                var scenario = scenarios[index];
                if (scenario is null)
                {
                    throw ReportingFailure.Report();
                }
                reportScenarios[index] = scenario;
                semanticScenarios[index] = scenario.ToSuiteScenario();
                switch (scenario.Assessment)
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

            if (summary.Scenarios != 6 || summary.Passed != passed || summary.Failed != failed ||
                summary.Inconclusive != inconclusive)
            {
                throw ReportingFailure.Report();
            }

            var semanticResult = new SuiteResult(
                assessment,
                new ReportReadOnlyList<ScenarioResult>(semanticScenarios, takeOwnership: true));
            ScenarioSuite.Validate(semanticResult);

            SchemaVersion = schemaVersion;
            SuiteIdentity = suiteIdentity;
            ExplanationIdentity = explanationIdentity;
            ClaimCeiling = claimCeiling;
            Assessment = assessment;
            AssessmentText = assessmentText;
            Outcome = outcome;
            Summary = summary;
            Scenarios = new ReportReadOnlyList<ReportScenario>(reportScenarios, takeOwnership: true);
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }
    }

    public string SchemaVersion { get; }
    public string SuiteIdentity { get; }
    public string ExplanationIdentity { get; }
    public string ClaimCeiling { get; }
    public Assessment Assessment { get; }
    public string AssessmentText { get; }
    public Outcome Outcome { get; }
    public ReportSummary Summary { get; }
    public IReadOnlyList<ReportScenario> Scenarios { get; }
}
