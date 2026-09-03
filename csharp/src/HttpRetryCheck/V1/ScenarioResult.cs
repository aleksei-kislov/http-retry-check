using System.Collections.Generic;

namespace HttpRetryCheck.V1;

/// <summary>Contains the assessment, observation, and findings for one scenario.</summary>
public sealed class ScenarioResult
{
    /// <summary>Creates a validated scenario result.</summary>
    /// <param name="scenario">The scenario that produced the result.</param>
    /// <param name="assessment">The evaluated assessment.</param>
    /// <param name="observation">The facts recorded by the scenario.</param>
    /// <param name="findings">The findings that explain the assessment.</param>
    /// <exception cref="SuiteException">The values do not form a valid scenario result.</exception>
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

    /// <summary>Gets the scenario that produced the result.</summary>
    public ScenarioId Scenario { get; }

    /// <summary>Gets the evaluated assessment.</summary>
    public Assessment Assessment { get; }

    /// <summary>Gets the facts recorded by the scenario.</summary>
    public Observation Observation { get; }

    /// <summary>Gets the findings that explain the assessment.</summary>
    public IReadOnlyList<FindingCode> Findings { get; }
}
