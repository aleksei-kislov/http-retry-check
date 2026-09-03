using System;
using System.Collections;
using System.Collections.Generic;

namespace HttpRetryCheck.V1.Reporting;

internal static class ReportingFailure
{
    internal static ReportException Report() => new(artifact: false);

    internal static ReportException Artifact() => new(artifact: true);

    internal static bool IsRecoverable(Exception exception) =>
        exception is not OutOfMemoryException and
        not StackOverflowException and
        not AccessViolationException and
        not AppDomainUnloadedException;
}

internal sealed class ReportReadOnlyList<T> : IReadOnlyList<T>
{
    private readonly T[] _items;

    internal ReportReadOnlyList(IReadOnlyList<T> source)
    {
        _items = new T[source.Count];
        for (var index = 0; index < _items.Length; index++)
        {
            _items[index] = source[index];
        }
    }

    internal ReportReadOnlyList(T[] source, bool takeOwnership)
    {
        _items = takeOwnership ? source : (T[])source.Clone();
    }

    public int Count => _items.Length;

    public T this[int index] => _items[index];

    public IEnumerator<T> GetEnumerator() => ((IEnumerable<T>)_items).GetEnumerator();

    IEnumerator IEnumerable.GetEnumerator() => _items.GetEnumerator();
}

internal static class ReportWire
{
    internal static bool IsDefined(Outcome value) =>
        value is Outcome.Pass or Outcome.Fail or Outcome.Inconclusive;

    internal static string OutcomeName(Outcome value) => value switch
    {
        Outcome.Pass => "pass",
        Outcome.Fail => "fail",
        Outcome.Inconclusive => "inconclusive",
        _ => throw ReportingFailure.Report(),
    };

    internal static bool TryParseOutcome(string value, out Outcome outcome)
    {
        outcome = value switch
        {
            "pass" => Outcome.Pass,
            "fail" => Outcome.Fail,
            "inconclusive" => Outcome.Inconclusive,
            _ => 0,
        };
        return IsDefined(outcome);
    }

    internal static string ScenarioName(ScenarioId value) => value switch
    {
        ScenarioId.AcceptThenDisconnect => "accept_then_disconnect",
        ScenarioId.DisconnectBeforeAcceptance => "disconnect_before_acceptance",
        ScenarioId.ChangedBodyRetry => "changed_body_retry",
        ScenarioId.CrossOriginRedirectCredentials => "cross_origin_redirect_credentials",
        ScenarioId.RetryLimit => "retry_limit",
        ScenarioId.DelayedResponse => "delayed_response",
        _ => throw ReportingFailure.Report(),
    };

    internal static bool TryParseScenario(string value, out ScenarioId scenario)
    {
        scenario = value switch
        {
            "accept_then_disconnect" => ScenarioId.AcceptThenDisconnect,
            "disconnect_before_acceptance" => ScenarioId.DisconnectBeforeAcceptance,
            "changed_body_retry" => ScenarioId.ChangedBodyRetry,
            "cross_origin_redirect_credentials" => ScenarioId.CrossOriginRedirectCredentials,
            "retry_limit" => ScenarioId.RetryLimit,
            "delayed_response" => ScenarioId.DelayedResponse,
            _ => 0,
        };
        return scenario is >= ScenarioId.AcceptThenDisconnect and <= ScenarioId.DelayedResponse;
    }

    internal static string AssessmentName(Assessment value) => value switch
    {
        Assessment.NoUnsafeBehaviorObserved => "no_unsafe_behavior_observed",
        Assessment.UnsafeBehaviorObserved => "unsafe_behavior_observed",
        Assessment.Inconclusive => "inconclusive",
        _ => throw ReportingFailure.Report(),
    };

    internal static bool TryParseAssessment(string value, out Assessment assessment)
    {
        assessment = value switch
        {
            "no_unsafe_behavior_observed" => Assessment.NoUnsafeBehaviorObserved,
            "unsafe_behavior_observed" => Assessment.UnsafeBehaviorObserved,
            "inconclusive" => Assessment.Inconclusive,
            _ => 0,
        };
        return assessment is >= Assessment.NoUnsafeBehaviorObserved and <= Assessment.Inconclusive;
    }

    internal static string CredentialName(CredentialState value) => value switch
    {
        CredentialState.NotObserved => "not_observed",
        CredentialState.SourceOnly => "source_only",
        CredentialState.AbsentAtTarget => "absent_at_target",
        CredentialState.ExposedAtTarget => "exposed_at_target",
        CredentialState.Missing => "missing",
        _ => throw ReportingFailure.Report(),
    };

    internal static bool TryParseCredential(string value, out CredentialState credential)
    {
        credential = value switch
        {
            "not_observed" => CredentialState.NotObserved,
            "source_only" => CredentialState.SourceOnly,
            "absent_at_target" => CredentialState.AbsentAtTarget,
            "exposed_at_target" => CredentialState.ExposedAtTarget,
            "missing" => CredentialState.Missing,
            _ => 0,
        };
        return credential is >= CredentialState.NotObserved and <= CredentialState.Missing;
    }

    internal static string CleanupName(CleanupState value) => value switch
    {
        CleanupState.Succeeded => "succeeded",
        CleanupState.Failed => "failed",
        _ => throw ReportingFailure.Report(),
    };

    internal static bool TryParseCleanup(string value, out CleanupState cleanup)
    {
        cleanup = value switch
        {
            "succeeded" => CleanupState.Succeeded,
            "failed" => CleanupState.Failed,
            _ => 0,
        };
        return cleanup is CleanupState.Succeeded or CleanupState.Failed;
    }

    internal static string FindingName(FindingCode value) => value switch
    {
        FindingCode.AttemptNotObserved => "attempt_not_observed",
        FindingCode.CaptureIncomplete => "capture_incomplete",
        FindingCode.ResponseIncomplete => "response_incomplete",
        FindingCode.DelayIncomplete => "delay_incomplete",
        FindingCode.AttemptLimitExceeded => "attempt_limit_exceeded",
        FindingCode.RetryBeforeResponse => "retry_before_response",
        FindingCode.RetryAfterAcceptedRequest => "retry_after_accepted_request",
        FindingCode.RetryAfterUnconfirmedAcceptance => "retry_after_unconfirmed_acceptance",
        FindingCode.MethodChanged => "method_changed",
        FindingCode.DestinationChanged => "destination_changed",
        FindingCode.BodyChanged => "body_changed",
        FindingCode.CredentialNotObserved => "credential_not_observed",
        FindingCode.CredentialMissing => "credential_missing",
        FindingCode.CredentialExposedAtTarget => "credential_exposed_at_target",
        FindingCode.EffectNotObserved => "effect_not_observed",
        FindingCode.EffectLimitExceeded => "effect_limit_exceeded",
        FindingCode.CleanupUnverified => "cleanup_unverified",
        FindingCode.ScenarioIncomplete => "scenario_incomplete",
        _ => throw ReportingFailure.Report(),
    };

    internal static bool TryParseFinding(string value, out FindingCode finding)
    {
        finding = value switch
        {
            "attempt_not_observed" => FindingCode.AttemptNotObserved,
            "capture_incomplete" => FindingCode.CaptureIncomplete,
            "response_incomplete" => FindingCode.ResponseIncomplete,
            "delay_incomplete" => FindingCode.DelayIncomplete,
            "attempt_limit_exceeded" => FindingCode.AttemptLimitExceeded,
            "retry_before_response" => FindingCode.RetryBeforeResponse,
            "retry_after_accepted_request" => FindingCode.RetryAfterAcceptedRequest,
            "retry_after_unconfirmed_acceptance" => FindingCode.RetryAfterUnconfirmedAcceptance,
            "method_changed" => FindingCode.MethodChanged,
            "destination_changed" => FindingCode.DestinationChanged,
            "body_changed" => FindingCode.BodyChanged,
            "credential_not_observed" => FindingCode.CredentialNotObserved,
            "credential_missing" => FindingCode.CredentialMissing,
            "credential_exposed_at_target" => FindingCode.CredentialExposedAtTarget,
            "effect_not_observed" => FindingCode.EffectNotObserved,
            "effect_limit_exceeded" => FindingCode.EffectLimitExceeded,
            "cleanup_unverified" => FindingCode.CleanupUnverified,
            "scenario_incomplete" => FindingCode.ScenarioIncomplete,
            _ => 0,
        };
        return finding is >= FindingCode.AttemptNotObserved and <= FindingCode.ScenarioIncomplete;
    }

    internal static Outcome OutcomeFor(Assessment assessment) => assessment switch
    {
        Assessment.NoUnsafeBehaviorObserved => Outcome.Pass,
        Assessment.UnsafeBehaviorObserved => Outcome.Fail,
        Assessment.Inconclusive => Outcome.Inconclusive,
        _ => throw ReportingFailure.Report(),
    };
}
