namespace HttpRetryCheck.V1;

public sealed class Observation
{
    public Observation(
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

        if (!SemanticEvaluator.IsValidForAnyScenario(this))
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }
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
}
