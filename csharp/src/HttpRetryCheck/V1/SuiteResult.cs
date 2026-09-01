using System.Collections.Generic;

namespace HttpRetryCheck.V1;

public sealed class SuiteResult
{
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

    public Assessment Assessment { get; }

    public IReadOnlyList<ScenarioResult> Scenarios { get; }
}
