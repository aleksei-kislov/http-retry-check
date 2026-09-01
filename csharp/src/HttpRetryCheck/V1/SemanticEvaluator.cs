using System;
using System.Collections.Generic;

namespace HttpRetryCheck.V1;

internal static class SemanticEvaluator
{
    private const uint MaximumObservedAttempts = 3;

    internal const int ScenarioCount = 6;

    internal static IEnumerable<ScenarioId> OrderedScenarios
    {
        get
        {
            for (var index = 0; index < ScenarioCount; index++)
            {
                yield return ScenarioAt(index);
            }
        }
    }

    internal static bool IsValidForAnyScenario(Observation observation)
    {
        for (var index = 0; index < ScenarioCount; index++)
        {
            if (IsValidObservation(ScenarioAt(index), observation))
            {
                return true;
            }
        }

        return false;
    }

    internal static bool TryEvaluate(
        ScenarioId scenario,
        Observation observation,
        out Assessment assessment,
        out FindingCode[] findings)
    {
        if (!IsValidObservation(scenario, observation))
        {
            assessment = default;
            findings = Array.Empty<FindingCode>();
            return false;
        }

        (assessment, findings) = Assess(scenario, observation);
        return true;
    }

    internal static ScenarioResult CreateScenarioResult(ScenarioId scenario, Observation observation)
    {
        if (!TryEvaluate(scenario, observation, out var assessment, out var findings))
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }

        return new ScenarioResult(scenario, assessment, observation, findings);
    }

    internal static SuiteResult CreateSuiteResult(IReadOnlyList<ScenarioResult> scenarios)
    {
        if (scenarios is null)
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }

        Assessment aggregate;
        try
        {
            if (scenarios.Count != ScenarioCount)
            {
                throw new SuiteException(SuiteFailureCode.InvalidResult);
            }

            aggregate = Assessment.NoUnsafeBehaviorObserved;
            for (var index = 0; index < ScenarioCount; index++)
            {
                var row = scenarios[index];
                if (row is null)
                {
                    throw new SuiteException(SuiteFailureCode.InvalidResult);
                }

                aggregate = CombineAssessment(aggregate, row.Assessment);
            }
        }
        catch (Exception exception) when (RecoverableException.IsRecoverable(exception) && exception is not SuiteException)
        {
            throw new SuiteException(SuiteFailureCode.InvalidResult);
        }

        return new SuiteResult(aggregate, scenarios);
    }

    internal static bool IsValidSuite(Assessment assessment, IReadOnlyList<ScenarioResult> scenarios)
    {
        if (scenarios is null || scenarios.Count != ScenarioCount)
        {
            return false;
        }

        var aggregate = Assessment.NoUnsafeBehaviorObserved;
        for (var index = 0; index < ScenarioCount; index++)
        {
            var row = scenarios[index];
            if (row is null || row.Scenario != ScenarioAt(index) ||
                row.Observation is null || row.Findings is null ||
                !TryEvaluate(row.Scenario, row.Observation, out var expectedAssessment, out var expectedFindings) ||
                row.Assessment != expectedAssessment || !FindingsEqual(row.Findings, expectedFindings))
            {
                return false;
            }

            aggregate = CombineAssessment(aggregate, expectedAssessment);
        }

        return assessment == aggregate;
    }

    internal static bool FindingsEqual(IReadOnlyList<FindingCode> left, FindingCode[] right)
    {
        if (left.Count != right.Length)
        {
            return false;
        }

        for (var index = 0; index < right.Length; index++)
        {
            if (left[index] != right[index])
            {
                return false;
            }
        }

        return true;
    }

    internal static ScenarioId ScenarioAt(int index)
    {
        return index switch
        {
            0 => ScenarioId.AcceptThenDisconnect,
            1 => ScenarioId.DisconnectBeforeAcceptance,
            2 => ScenarioId.ChangedBodyRetry,
            3 => ScenarioId.CrossOriginRedirectCredentials,
            4 => ScenarioId.RetryLimit,
            5 => ScenarioId.DelayedResponse,
            _ => default,
        };
    }

    private static bool IsValidObservation(ScenarioId scenario, Observation observation)
    {
        var laterAdmissions = observation.AttemptCount == 0 ? 0 : observation.AttemptCount - 1;
        if (!IsKnownScenario(scenario) || observation.AttemptCount > MaximumObservedAttempts ||
            observation.EffectCount > observation.AttemptCount ||
            (observation.AttemptCount != 0 && observation.OverlapCount >= observation.AttemptCount) ||
            observation.RetryAfterEffectCount > laterAdmissions ||
            observation.RetryAfterUnconfirmedCount > laterAdmissions ||
            observation.RetryAfterEffectCount + observation.RetryAfterUnconfirmedCount > laterAdmissions ||
            observation.RetryBeforeResponseCount > observation.RetryAfterEffectCount ||
            (observation.RetryAfterEffectCount != 0 && observation.EffectCount == 0) ||
            observation.ResponseAttemptCount > observation.AttemptCount ||
            observation.ResponseCompleteCount > observation.ResponseAttemptCount ||
            (observation.FirstResponseComplete && observation.ResponseCompleteCount == 0) ||
            (observation.AttemptCount == 1 && observation.ResponseCompleteCount > 0 &&
                !observation.FirstResponseComplete) ||
            observation.DelayCompleteCount > observation.AttemptCount ||
            !IsKnownCredentialState(observation.Credential) || !IsKnownCleanup(observation.Cleanup))
        {
            return false;
        }

        if (observation.OverlapCount != 0 && observation.CaptureComplete)
        {
            return false;
        }

        if (observation.CaptureComplete)
        {
            var later = observation.AttemptCount == 0 ? 0 : observation.AttemptCount - 1;
            switch (scenario)
            {
                case ScenarioId.AcceptThenDisconnect:
                case ScenarioId.ChangedBodyRetry:
                    if (observation.EffectCount != observation.AttemptCount ||
                        observation.RetryAfterEffectCount != later)
                    {
                        return false;
                    }

                    break;
                case ScenarioId.DisconnectBeforeAcceptance:
                    if (observation.RetryAfterUnconfirmedCount != later)
                    {
                        return false;
                    }

                    break;
                case ScenarioId.CrossOriginRedirectCredentials:
                case ScenarioId.RetryLimit:
                    if (observation.ResponseAttemptCount != observation.AttemptCount)
                    {
                        return false;
                    }

                    break;
                case ScenarioId.DelayedResponse:
                    if (observation.EffectCount != observation.AttemptCount ||
                        observation.DelayCompleteCount != observation.AttemptCount ||
                        observation.ResponseAttemptCount != observation.AttemptCount ||
                        observation.RetryAfterEffectCount != later ||
                        observation.RetryBeforeResponseCount != 0)
                    {
                        return false;
                    }

                    break;
            }
        }

        if (observation.AttemptCount == 0 &&
            (observation.EffectCount != 0 || observation.OverlapCount != 0 ||
                observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
                observation.RetryBeforeResponseCount != 0 || observation.ResponseAttemptCount != 0 ||
                observation.ResponseCompleteCount != 0 || observation.FirstResponseComplete ||
                observation.DelayCompleteCount != 0 || !observation.MethodConsistent ||
                !observation.DestinationConsistent || !observation.BodyConsistent ||
                observation.Credential != CredentialState.NotObserved))
        {
            return false;
        }

        switch (scenario)
        {
            case ScenarioId.AcceptThenDisconnect:
            case ScenarioId.ChangedBodyRetry:
                if (observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
                    observation.DelayCompleteCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
                    observation.RetryBeforeResponseCount != 0)
                {
                    return false;
                }

                break;
            case ScenarioId.DisconnectBeforeAcceptance:
                if (observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
                    observation.DelayCompleteCount != 0 || observation.RetryAfterEffectCount != 0 ||
                    observation.RetryBeforeResponseCount != 0)
                {
                    return false;
                }

                break;
            case ScenarioId.CrossOriginRedirectCredentials:
                if (observation.DelayCompleteCount != 0 || observation.RetryAfterEffectCount != 0 ||
                    observation.RetryAfterUnconfirmedCount != 0 || observation.RetryBeforeResponseCount != 0 ||
                    observation.EffectCount > observation.ResponseAttemptCount ||
                    (observation.AttemptCount == 1 && observation.EffectCount != 0) ||
                    (observation.AttemptCount == 1 &&
                        (observation.Credential == CredentialState.AbsentAtTarget ||
                            observation.Credential == CredentialState.ExposedAtTarget)) ||
                    (observation.Credential == CredentialState.SourceOnly && observation.EffectCount != 0) ||
                    (observation.Credential == CredentialState.AbsentAtTarget &&
                        (observation.AttemptCount < 2 || observation.EffectCount == 0 ||
                            observation.ResponseAttemptCount < observation.EffectCount + 1)) ||
                    ((observation.Credential == CredentialState.SourceOnly ||
                        observation.Credential == CredentialState.Missing) &&
                        observation.ResponseAttemptCount == 0) ||
                    (observation.Credential == CredentialState.Missing && observation.EffectCount != 0 &&
                        observation.ResponseAttemptCount < observation.EffectCount + 1))
                {
                    return false;
                }

                break;
            case ScenarioId.RetryLimit:
                if (observation.DelayCompleteCount != 0 || observation.EffectCount != 0 ||
                    observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
                    observation.RetryBeforeResponseCount != 0)
                {
                    return false;
                }

                break;
            case ScenarioId.DelayedResponse:
                if (observation.RetryAfterUnconfirmedCount != 0 ||
                    observation.DelayCompleteCount > observation.EffectCount ||
                    observation.ResponseAttemptCount > observation.DelayCompleteCount)
                {
                    return false;
                }

                break;
            default:
                return false;
        }

        if (scenario == ScenarioId.DisconnectBeforeAcceptance && observation.EffectCount != 0)
        {
            return false;
        }

        if (scenario != ScenarioId.CrossOriginRedirectCredentials &&
            (observation.Credential == CredentialState.AbsentAtTarget ||
                observation.Credential == CredentialState.ExposedAtTarget))
        {
            return false;
        }

        var completeEffectScenario = scenario == ScenarioId.AcceptThenDisconnect ||
            scenario == ScenarioId.ChangedBodyRetry || scenario == ScenarioId.DelayedResponse;
        var completeResponseScenario = scenario == ScenarioId.CrossOriginRedirectCredentials ||
            scenario == ScenarioId.RetryLimit;
        if ((observation.Credential == CredentialState.SourceOnly ||
                observation.Credential == CredentialState.Missing) &&
            ((completeEffectScenario && observation.EffectCount == 0) ||
                (completeResponseScenario && observation.ResponseAttemptCount == 0)))
        {
            return false;
        }

        if (!observation.BodyConsistent &&
            ((completeEffectScenario && observation.EffectCount == 0) ||
                (completeResponseScenario && observation.ResponseAttemptCount == 0) ||
                (scenario == ScenarioId.DisconnectBeforeAcceptance &&
                    observation.Credential != CredentialState.SourceOnly &&
                    observation.Credential != CredentialState.Missing)))
        {
            return false;
        }

        if (observation.Credential == CredentialState.NotObserved)
        {
            switch (scenario)
            {
                case ScenarioId.AcceptThenDisconnect:
                case ScenarioId.ChangedBodyRetry:
                case ScenarioId.DelayedResponse:
                    if (observation.EffectCount != 0)
                    {
                        return false;
                    }

                    break;
                case ScenarioId.DisconnectBeforeAcceptance:
                    if (observation.RetryAfterUnconfirmedCount != 0 ||
                        (observation.CaptureComplete && observation.AttemptCount != 0))
                    {
                        return false;
                    }

                    break;
                case ScenarioId.CrossOriginRedirectCredentials:
                    if (observation.ResponseAttemptCount > observation.EffectCount ||
                        observation.FirstResponseComplete)
                    {
                        return false;
                    }

                    break;
                case ScenarioId.RetryLimit:
                    if (observation.ResponseAttemptCount != 0)
                    {
                        return false;
                    }

                    break;
            }
        }

        return true;
    }

    private static (Assessment Assessment, FindingCode[] Findings) Assess(
        ScenarioId scenario,
        Observation observation)
    {
        var findings = new List<FindingCode>(8);
        var unsafeEvidence = false;
        var inconclusiveEvidence = false;

        if (observation.AttemptCount == 0)
        {
            AddInconclusive(FindingCode.AttemptNotObserved);
        }

        if (!observation.CaptureComplete)
        {
            AddInconclusive(FindingCode.CaptureIncomplete);
        }

        var (responseIncomplete, delayIncomplete) = IncompleteStimulus(scenario, observation);
        if (responseIncomplete)
        {
            AddInconclusive(FindingCode.ResponseIncomplete);
        }

        if (delayIncomplete)
        {
            AddInconclusive(FindingCode.DelayIncomplete);
        }

        if (observation.AttemptCount > 2)
        {
            AddUnsafe(FindingCode.AttemptLimitExceeded);
        }

        if (scenario == ScenarioId.DelayedResponse && observation.RetryBeforeResponseCount != 0)
        {
            AddUnsafe(FindingCode.RetryBeforeResponse);
        }

        if ((scenario == ScenarioId.AcceptThenDisconnect || scenario == ScenarioId.ChangedBodyRetry ||
                scenario == ScenarioId.DelayedResponse) && observation.RetryAfterEffectCount != 0)
        {
            AddUnsafe(FindingCode.RetryAfterAcceptedRequest);
        }

        if (scenario == ScenarioId.DisconnectBeforeAcceptance &&
            observation.RetryAfterUnconfirmedCount != 0)
        {
            AddUnsafe(FindingCode.RetryAfterUnconfirmedAcceptance);
        }

        if (observation.AttemptCount != 0 && !observation.MethodConsistent)
        {
            AddUnsafe(FindingCode.MethodChanged);
        }

        if (observation.AttemptCount != 0 && !observation.DestinationConsistent)
        {
            AddUnsafe(FindingCode.DestinationChanged);
        }

        if (observation.AttemptCount != 0 && !observation.BodyConsistent)
        {
            AddUnsafe(FindingCode.BodyChanged);
        }

        switch (observation.Credential)
        {
            case CredentialState.NotObserved when observation.AttemptCount != 0:
                AddInconclusive(FindingCode.CredentialNotObserved);
                break;
            case CredentialState.Missing:
                AddInconclusive(FindingCode.CredentialMissing);
                break;
            case CredentialState.ExposedAtTarget:
                AddUnsafe(FindingCode.CredentialExposedAtTarget);
                break;
        }

        if (EffectNotObserved(scenario, observation))
        {
            AddInconclusive(FindingCode.EffectNotObserved);
        }

        if (EffectLimitExceeded(scenario, observation.EffectCount))
        {
            AddUnsafe(FindingCode.EffectLimitExceeded);
        }

        if (observation.Cleanup != CleanupState.Succeeded)
        {
            AddInconclusive(FindingCode.CleanupUnverified);
        }

        if (!unsafeEvidence && !inconclusiveEvidence && !IsPositiveTuple(scenario, observation))
        {
            AddInconclusive(FindingCode.ScenarioIncomplete);
        }

        var assessment = unsafeEvidence
            ? Assessment.UnsafeBehaviorObserved
            : inconclusiveEvidence
                ? Assessment.Inconclusive
                : Assessment.NoUnsafeBehaviorObserved;
        return (assessment, findings.ToArray());

        void AddInconclusive(FindingCode finding)
        {
            findings.Add(finding);
            inconclusiveEvidence = true;
        }

        void AddUnsafe(FindingCode finding)
        {
            findings.Add(finding);
            unsafeEvidence = true;
        }
    }

    private static (bool Response, bool Delay) IncompleteStimulus(
        ScenarioId scenario,
        Observation observation)
    {
        if (observation.AttemptCount == 0)
        {
            return (false, false);
        }

        return scenario switch
        {
            ScenarioId.CrossOriginRedirectCredentials or ScenarioId.RetryLimit =>
                (observation.ResponseAttemptCount < observation.AttemptCount ||
                    !observation.FirstResponseComplete ||
                    observation.ResponseCompleteCount < observation.AttemptCount, false),
            ScenarioId.DelayedResponse =>
                (observation.ResponseAttemptCount < observation.AttemptCount,
                    observation.DelayCompleteCount < observation.AttemptCount),
            _ => (false, false),
        };
    }

    private static bool EffectLimitExceeded(ScenarioId scenario, ulong effects)
    {
        return scenario switch
        {
            ScenarioId.DisconnectBeforeAcceptance or ScenarioId.RetryLimit => effects != 0,
            _ => effects > 1,
        };
    }

    private static bool EffectNotObserved(ScenarioId scenario, Observation observation)
    {
        return scenario switch
        {
            ScenarioId.AcceptThenDisconnect or ScenarioId.ChangedBodyRetry or ScenarioId.DelayedResponse =>
                observation.AttemptCount != 0 && observation.EffectCount == 0,
            ScenarioId.CrossOriginRedirectCredentials =>
                observation.AttemptCount >= 2 && observation.EffectCount == 0,
            _ => false,
        };
    }

    private static bool IsPositiveTuple(ScenarioId scenario, Observation observation)
    {
        if (!observation.CaptureComplete || observation.OverlapCount != 0 ||
            observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
            observation.RetryBeforeResponseCount != 0 || !observation.MethodConsistent ||
            !observation.DestinationConsistent || !observation.BodyConsistent ||
            observation.Cleanup != CleanupState.Succeeded)
        {
            return false;
        }

        switch (scenario)
        {
            case ScenarioId.AcceptThenDisconnect:
            case ScenarioId.ChangedBodyRetry:
                return observation.AttemptCount == 1 && observation.EffectCount == 1 &&
                    observation.ResponseAttemptCount == 0 && observation.ResponseCompleteCount == 0 &&
                    !observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
                    observation.Credential == CredentialState.SourceOnly;
            case ScenarioId.DisconnectBeforeAcceptance:
                return observation.AttemptCount == 1 && observation.EffectCount == 0 &&
                    observation.ResponseAttemptCount == 0 && observation.ResponseCompleteCount == 0 &&
                    !observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
                    observation.Credential == CredentialState.SourceOnly;
            case ScenarioId.CrossOriginRedirectCredentials:
                var refused = observation.AttemptCount == 1 && observation.EffectCount == 0 &&
                    observation.ResponseAttemptCount == 1 && observation.ResponseCompleteCount == 1 &&
                    observation.FirstResponseComplete && observation.Credential == CredentialState.SourceOnly;
                var followed = observation.AttemptCount == 2 && observation.EffectCount == 1 &&
                    observation.ResponseAttemptCount == 2 && observation.ResponseCompleteCount == 2 &&
                    observation.FirstResponseComplete &&
                    observation.Credential == CredentialState.AbsentAtTarget;
                return observation.DelayCompleteCount == 0 && (refused || followed);
            case ScenarioId.RetryLimit:
                return observation.AttemptCount >= 1 && observation.AttemptCount <= 2 &&
                    observation.EffectCount == 0 &&
                    observation.ResponseAttemptCount == observation.AttemptCount &&
                    observation.ResponseCompleteCount == observation.AttemptCount &&
                    observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
                    observation.Credential == CredentialState.SourceOnly;
            case ScenarioId.DelayedResponse:
                return observation.AttemptCount == 1 && observation.EffectCount == 1 &&
                    observation.ResponseAttemptCount == 1 && observation.ResponseCompleteCount <= 1 &&
                    observation.FirstResponseComplete == (observation.ResponseCompleteCount == 1) &&
                    observation.DelayCompleteCount == 1 &&
                    observation.Credential == CredentialState.SourceOnly;
            default:
                return false;
        }
    }

    private static Assessment CombineAssessment(Assessment left, Assessment right)
    {
        if (left == Assessment.UnsafeBehaviorObserved || right == Assessment.UnsafeBehaviorObserved)
        {
            return Assessment.UnsafeBehaviorObserved;
        }

        if (left == Assessment.Inconclusive || right == Assessment.Inconclusive)
        {
            return Assessment.Inconclusive;
        }

        return Assessment.NoUnsafeBehaviorObserved;
    }

    private static bool IsKnownScenario(ScenarioId value)
    {
        return value >= ScenarioId.AcceptThenDisconnect && value <= ScenarioId.DelayedResponse;
    }

    private static bool IsKnownCredentialState(CredentialState value)
    {
        return value >= CredentialState.NotObserved && value <= CredentialState.Missing;
    }

    private static bool IsKnownCleanup(CleanupState value)
    {
        return value is CleanupState.Succeeded or CleanupState.Failed;
    }
}
