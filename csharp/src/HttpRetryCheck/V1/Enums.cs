namespace HttpRetryCheck.V1;

/// <summary>Identifies one of the six HTTP retry scenarios.</summary>
public enum ScenarioId
{
    /// <summary>The origin accepts a request, then disconnects before responding.</summary>
    AcceptThenDisconnect = 1,

    /// <summary>The origin disconnects while request acceptance remains uncertain.</summary>
    DisconnectBeforeAcceptance = 2,

    /// <summary>The origin checks whether a replay changes the accepted request body.</summary>
    ChangedBodyRetry = 3,

    /// <summary>A second controlled origin checks whether a redirect exposes a credential.</summary>
    CrossOriginRedirectCredentials = 4,

    /// <summary>The origin returns retryable responses and counts client attempts.</summary>
    RetryLimit = 5,

    /// <summary>The origin delays its response after accepting the request.</summary>
    DelayedResponse = 6,
}

/// <summary>Describes the evaluated result of a scenario or suite.</summary>
public enum Assessment
{
    /// <summary>No suite-defined unsafe behavior was observed.</summary>
    NoUnsafeBehaviorObserved = 1,

    /// <summary>Suite-defined unsafe behavior was observed.</summary>
    UnsafeBehaviorObserved = 2,

    /// <summary>The available observations cannot establish a safe or unsafe result.</summary>
    Inconclusive = 3,
}

/// <summary>Describes whether the suite verified cleanup of scenario resources.</summary>
public enum CleanupState
{
    /// <summary>Cleanup completed and was verified.</summary>
    Succeeded = 1,

    /// <summary>Cleanup could not be verified.</summary>
    Failed = 2,
}

/// <summary>Describes where the suite's synthetic credential was observed.</summary>
public enum CredentialState
{
    /// <summary>No credential location was established.</summary>
    NotObserved = 1,

    /// <summary>The credential was observed only at the source origin.</summary>
    SourceOnly = 2,

    /// <summary>The credential was absent at the redirect target.</summary>
    AbsentAtTarget = 3,

    /// <summary>The credential reached the redirect target.</summary>
    ExposedAtTarget = 4,

    /// <summary>The source request did not contain exactly one expected credential.</summary>
    Missing = 5,
}

/// <summary>Identifies one fact used to explain an unsafe or inconclusive assessment.</summary>
public enum FindingCode
{
    /// <summary>No request reached the controlled origin.</summary>
    AttemptNotObserved = 1,

    /// <summary>The suite did not capture a complete scenario result.</summary>
    CaptureIncomplete = 2,

    /// <summary>A required response attempt or completion was not observed.</summary>
    ResponseIncomplete = 3,

    /// <summary>The controlled delayed-response phase did not finish.</summary>
    DelayIncomplete = 4,

    /// <summary>The client made more attempts than the selected limit.</summary>
    AttemptLimitExceeded = 5,

    /// <summary>The client retried while an earlier response was pending.</summary>
    RetryBeforeResponse = 6,

    /// <summary>The client retried after the origin accepted the request.</summary>
    RetryAfterAcceptedRequest = 7,

    /// <summary>The client retried without knowing whether the earlier request was accepted.</summary>
    RetryAfterUnconfirmedAcceptance = 8,

    /// <summary>An observed request used an unexpected method.</summary>
    MethodChanged = 9,

    /// <summary>An observed request used an unexpected destination.</summary>
    DestinationChanged = 10,

    /// <summary>An observed request body differed from the original body.</summary>
    BodyChanged = 11,

    /// <summary>The suite could not determine where the synthetic credential was sent.</summary>
    CredentialNotObserved = 12,

    /// <summary>The original request did not contain exactly one expected credential.</summary>
    CredentialMissing = 13,

    /// <summary>The synthetic credential reached the redirect target.</summary>
    CredentialExposedAtTarget = 14,

    /// <summary>The expected server-side effect was not observed.</summary>
    EffectNotObserved = 15,

    /// <summary>The origin recorded more effects than the scenario permits.</summary>
    EffectLimitExceeded = 16,

    /// <summary>The suite could not verify cleanup of all scenario resources.</summary>
    CleanupUnverified = 17,

    /// <summary>The scenario did not collect enough evidence to pass.</summary>
    ScenarioIncomplete = 18,
}

/// <summary>Identifies why the scenario suite could not return or validate a result.</summary>
public enum SuiteFailureCode
{
    /// <summary>The caller supplied an invalid argument or option.</summary>
    InvalidCall = 1,

    /// <summary>The local scenario suite could not be made available.</summary>
    SuiteUnavailable = 2,

    /// <summary>The suite failed because of an internal error.</summary>
    InternalFailure = 3,

    /// <summary>A result did not satisfy the suite's semantic rules.</summary>
    InvalidResult = 4,
}
