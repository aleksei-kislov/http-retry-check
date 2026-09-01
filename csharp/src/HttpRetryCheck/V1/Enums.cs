namespace HttpRetryCheck.V1;

public enum ScenarioId
{
    AcceptThenDisconnect = 1,
    DisconnectBeforeAcceptance = 2,
    ChangedBodyRetry = 3,
    CrossOriginRedirectCredentials = 4,
    RetryLimit = 5,
    DelayedResponse = 6,
}

public enum Assessment
{
    NoUnsafeBehaviorObserved = 1,
    UnsafeBehaviorObserved = 2,
    Inconclusive = 3,
}

public enum CleanupState
{
    Succeeded = 1,
    Failed = 2,
}

public enum CredentialState
{
    NotObserved = 1,
    SourceOnly = 2,
    AbsentAtTarget = 3,
    ExposedAtTarget = 4,
    Missing = 5,
}

public enum FindingCode
{
    AttemptNotObserved = 1,
    CaptureIncomplete = 2,
    ResponseIncomplete = 3,
    DelayIncomplete = 4,
    AttemptLimitExceeded = 5,
    RetryBeforeResponse = 6,
    RetryAfterAcceptedRequest = 7,
    RetryAfterUnconfirmedAcceptance = 8,
    MethodChanged = 9,
    DestinationChanged = 10,
    BodyChanged = 11,
    CredentialNotObserved = 12,
    CredentialMissing = 13,
    CredentialExposedAtTarget = 14,
    EffectNotObserved = 15,
    EffectLimitExceeded = 16,
    CleanupUnverified = 17,
    ScenarioIncomplete = 18,
}

public enum SuiteFailureCode
{
    InvalidCall = 1,
    SuiteUnavailable = 2,
    InternalFailure = 3,
    InvalidResult = 4,
}
