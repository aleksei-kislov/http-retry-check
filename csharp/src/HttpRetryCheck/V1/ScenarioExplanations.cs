namespace HttpRetryCheck.V1;

public static class ScenarioExplanations
{
    public static string ScenarioText(ScenarioId scenario)
    {
        return scenario switch
        {
            ScenarioId.AcceptThenDisconnect =>
                "Tests whether the client retries after the origin accepts a request and disconnects before responding.",
            ScenarioId.DisconnectBeforeAcceptance =>
                "Tests whether the client retries after the connection ends before request acceptance can be confirmed.",
            ScenarioId.ChangedBodyRetry =>
                "Tests whether a retry changes the body after the origin accepts a request and disconnects.",
            ScenarioId.CrossOriginRedirectCredentials =>
                "Tests redirect behavior and whether the synthetic credential reaches a different controlled origin.",
            ScenarioId.RetryLimit =>
                "Tests how many attempts the client makes after a controlled service-unavailable response.",
            ScenarioId.DelayedResponse =>
                "Tests whether the client retries while an accepted request is waiting for a delayed response.",
            _ => "Unknown HTTP client scenario.",
        };
    }

    public static string AssessmentText(Assessment assessment)
    {
        return assessment switch
        {
            Assessment.NoUnsafeBehaviorObserved =>
                "No unsafe HTTP behavior was observed.",
            Assessment.UnsafeBehaviorObserved =>
                "Unsafe HTTP behavior was observed.",
            Assessment.Inconclusive =>
                "The result is inconclusive.",
            _ => "Unknown HTTP client scenario assessment.",
        };
    }

    public static string FindingText(FindingCode finding)
    {
        return finding switch
        {
            FindingCode.AttemptNotObserved =>
                "No request reached the test server.",
            FindingCode.CaptureIncomplete =>
                "The test did not capture a complete scenario result.",
            FindingCode.ResponseIncomplete =>
                "The required response attempt or completion was not observed.",
            FindingCode.DelayIncomplete =>
                "The test server did not finish its delayed-response phase.",
            FindingCode.AttemptLimitExceeded =>
                "The client made more than two attempts.",
            FindingCode.RetryBeforeResponse =>
                "The client retried while an earlier response was still pending.",
            FindingCode.RetryAfterAcceptedRequest =>
                "The client retried after the server accepted the request.",
            FindingCode.RetryAfterUnconfirmedAcceptance =>
                "The client retried without knowing whether the server accepted the earlier request.",
            FindingCode.MethodChanged =>
                "The request method did not match the expected method.",
            FindingCode.DestinationChanged =>
                "The request was sent to an unexpected destination.",
            FindingCode.BodyChanged =>
                "A request body differed from the original.",
            FindingCode.CredentialNotObserved =>
                "The test could not determine where the synthetic credential was sent.",
            FindingCode.CredentialMissing =>
                "The original request did not contain exactly one expected synthetic Authorization value.",
            FindingCode.CredentialExposedAtTarget =>
                "The synthetic Authorization value reached the redirect target.",
            FindingCode.EffectNotObserved =>
                "The test did not observe the expected server-side effect.",
            FindingCode.EffectLimitExceeded =>
                "The test server recorded more effects than this scenario allows.",
            FindingCode.CleanupUnverified =>
                "The test could not verify that all scenario resources were cleaned up.",
            FindingCode.ScenarioIncomplete =>
                "The scenario did not collect enough evidence to pass.",
            _ => "Unknown HTTP client scenario finding.",
        };
    }
}
