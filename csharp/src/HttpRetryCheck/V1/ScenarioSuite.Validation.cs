namespace HttpRetryCheck.V1;

public static partial class ScenarioSuite
{
    public static void Validate(SuiteResult result)
    {
        if (result is null || !SemanticEvaluator.IsValidSuite(result.Assessment, result.Scenarios))
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }
    }
}
