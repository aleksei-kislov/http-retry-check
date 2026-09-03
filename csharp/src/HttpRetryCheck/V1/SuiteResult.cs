using System.Collections.Generic;

namespace HttpRetryCheck.V1;

/// <summary>Contains the aggregate assessment and six ordered scenario results.</summary>
public sealed class SuiteResult
{
    /// <summary>Creates a validated six-scenario suite result.</summary>
    /// <param name="assessment">The aggregate suite assessment.</param>
    /// <param name="scenarios">The six ordered scenario results.</param>
    /// <exception cref="SuiteException">The values do not form a valid suite result.</exception>
    public SuiteResult(
        Assessment assessment,
        IReadOnlyList<ScenarioResult> scenarios)
    {
        if (!DetachedReadOnlyList<ScenarioResult>.TryCopy(scenarios, SemanticEvaluator.ScenarioCount, out var detached) ||
            detached is null ||
            !SemanticEvaluator.IsValidSuite(assessment, detached))
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }

        Assessment = assessment;
        Scenarios = detached;
    }

    /// <summary>Gets the aggregate suite assessment.</summary>
    public Assessment Assessment { get; }

    /// <summary>Gets the six ordered scenario results.</summary>
    public IReadOnlyList<ScenarioResult> Scenarios { get; }
}
