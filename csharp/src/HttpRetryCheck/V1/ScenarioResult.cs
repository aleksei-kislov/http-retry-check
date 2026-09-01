using System.Collections.Generic;

namespace HttpRetryCheck.V1;

public sealed class ScenarioResult
{
    public ScenarioResult(
        ScenarioId scenario,
        Assessment assessment,
        Observation observation,
        IReadOnlyList<FindingCode> findings)
    {
        if (observation is null ||
            !DetachedReadOnlyList<FindingCode>.TryCopy(findings, 18, out var detached) ||
            detached is null ||
            !SemanticEvaluator.TryEvaluate(scenario, observation, out var expectedAssessment, out var expectedFindings) ||
            assessment != expectedAssessment ||
            !SemanticEvaluator.FindingsEqual(detached, expectedFindings))
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }

        Scenario = scenario;
        Assessment = assessment;
        Observation = observation;
        Findings = detached;
    }

    public ScenarioId Scenario { get; }

    public Assessment Assessment { get; }

    public Observation Observation { get; }

    public IReadOnlyList<FindingCode> Findings { get; }
}
