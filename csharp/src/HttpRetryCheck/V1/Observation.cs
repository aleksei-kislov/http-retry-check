namespace HttpRetryCheck.V1;

/// <summary>Contains the bounded facts recorded during one scenario.</summary>
public sealed class Observation
{
    /// <summary>Creates a validated scenario observation.</summary>
    /// <param name="captureComplete">Whether the scenario capture completed.</param>
    /// <param name="attemptCount">The number of connections admitted as request attempts.</param>
    /// <param name="attemptLimit">The selected maximum safe attempt count.</param>
    /// <param name="protocol">The captured HTTP protocol, or an empty string when unavailable.</param>
    /// <param name="effectCount">The number of server-side effects recorded.</param>
    /// <param name="overlapCount">The number of attempts that overlapped an earlier attempt.</param>
    /// <param name="retryAfterEffectCount">The number of retries begun after an effect.</param>
    /// <param name="retryAfterUnconfirmedCount">The number of retries begun while acceptance was uncertain.</param>
    /// <param name="retryBeforeResponseCount">The number of retries begun before an earlier response completed.</param>
    /// <param name="responseAttemptCount">The number of response attempts made by the origin.</param>
    /// <param name="responseCompleteCount">The number of responses completely written by the origin.</param>
    /// <param name="firstResponseComplete">Whether the first response completed.</param>
    /// <param name="delayCompleteCount">The number of completed controlled delays.</param>
    /// <param name="methodConsistent">Whether every observed request used the expected method.</param>
    /// <param name="destinationConsistent">Whether every observed request used an expected destination.</param>
    /// <param name="bodyConsistent">Whether every observed body matched the original body.</param>
    /// <param name="credential">Where the synthetic credential was observed.</param>
    /// <param name="cleanup">Whether resource cleanup was verified.</param>
    /// <exception cref="SuiteException">The values cannot form a valid suite observation.</exception>
    public Observation(
        bool captureComplete,
        uint attemptCount,
        uint attemptLimit,
        string protocol,
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
        AttemptLimit = attemptLimit;
        Protocol = protocol;
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

    /// <summary>Gets whether the scenario capture completed.</summary>
    public bool CaptureComplete { get; }

    /// <summary>Gets the number of connections admitted as request attempts.</summary>
    public uint AttemptCount { get; }

    /// <summary>Gets the selected maximum safe attempt count.</summary>
    public uint AttemptLimit { get; }

    /// <summary>Gets the captured HTTP protocol, or an empty string when unavailable.</summary>
    public string Protocol { get; }

    /// <summary>Gets the number of server-side effects recorded.</summary>
    public ulong EffectCount { get; }

    /// <summary>Gets the number of attempts that overlapped an earlier attempt.</summary>
    public uint OverlapCount { get; }

    /// <summary>Gets the number of retries begun after a server-side effect.</summary>
    public uint RetryAfterEffectCount { get; }

    /// <summary>Gets the number of retries begun while acceptance was uncertain.</summary>
    public uint RetryAfterUnconfirmedCount { get; }

    /// <summary>Gets the number of retries begun before an earlier response completed.</summary>
    public uint RetryBeforeResponseCount { get; }

    /// <summary>Gets the number of response attempts made by the origin.</summary>
    public uint ResponseAttemptCount { get; }

    /// <summary>Gets the number of responses completely written by the origin.</summary>
    public uint ResponseCompleteCount { get; }

    /// <summary>Gets whether the first response completed.</summary>
    public bool FirstResponseComplete { get; }

    /// <summary>Gets the number of completed controlled delays.</summary>
    public uint DelayCompleteCount { get; }

    /// <summary>Gets whether every observed request used the expected method.</summary>
    public bool MethodConsistent { get; }

    /// <summary>Gets whether every observed request used an expected destination.</summary>
    public bool DestinationConsistent { get; }

    /// <summary>Gets whether every observed body matched the original body.</summary>
    public bool BodyConsistent { get; }

    /// <summary>Gets where the synthetic credential was observed.</summary>
    public CredentialState Credential { get; }

    /// <summary>Gets whether resource cleanup was verified.</summary>
    public CleanupState Cleanup { get; }
}
