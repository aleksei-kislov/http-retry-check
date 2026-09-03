namespace HttpRetryCheck.V1;

public static partial class ScenarioSuite
{
    /// <summary>Recalculates and validates a complete suite result.</summary>
    /// <param name="result">The result to validate.</param>
    /// <exception cref="SuiteException">The result is null or semantically invalid.</exception>
    public static void Validate(SuiteResult result)
    {
        if (result is null || !SemanticEvaluator.IsValidSuite(result.Assessment, result.Scenarios))
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }
    }
}
