using System;
using System.Collections;
using System.Collections.Generic;
using HttpRetryCheck.V1;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Semantics;

[TestClass]
public sealed class SemanticModelTests
{
    [TestMethod]
    public void ConstructorsAcceptAllNinePassingObservations()
    {
        var cases = new (ScenarioId Scenario, Observation Observation)[]
        {
            (ScenarioId.AcceptThenDisconnect, PositiveObservation(1, 1, 0, 0, false, 0, CredentialState.SourceOnly)),
            (ScenarioId.DisconnectBeforeAcceptance, PositiveObservation(1, 0, 0, 0, false, 0, CredentialState.SourceOnly)),
            (ScenarioId.ChangedBodyRetry, PositiveObservation(1, 1, 0, 0, false, 0, CredentialState.SourceOnly)),
            (ScenarioId.CrossOriginRedirectCredentials, PositiveObservation(1, 0, 1, 1, true, 0, CredentialState.SourceOnly)),
            (ScenarioId.CrossOriginRedirectCredentials, PositiveObservation(2, 1, 2, 2, true, 0, CredentialState.AbsentAtTarget)),
            (ScenarioId.RetryLimit, PositiveObservation(1, 0, 1, 1, true, 0, CredentialState.SourceOnly)),
            (ScenarioId.RetryLimit, PositiveObservation(2, 0, 2, 2, true, 0, CredentialState.SourceOnly)),
            (ScenarioId.DelayedResponse, PositiveObservation(1, 1, 1, 1, true, 1, CredentialState.SourceOnly)),
            (ScenarioId.DelayedResponse, PositiveObservation(1, 1, 1, 0, false, 1, CredentialState.SourceOnly)),
        };

        foreach (var item in cases)
        {
            var row = new ScenarioResult(
                item.Scenario,
                Assessment.NoUnsafeBehaviorObserved,
                item.Observation,
                new List<FindingCode>());

            Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, row.Assessment);
            Assert.AreEqual(0, row.Findings.Count);
        }
    }

    [TestMethod]
    public void EvaluationPreservesCanonicalFindingOrderAndUnsafePrecedence()
    {
        var observation = new Observation(
            captureComplete: false,
            attemptCount: 2,
            effectCount: 2,
            overlapCount: 0,
            retryAfterEffectCount: 1,
            retryAfterUnconfirmedCount: 0,
            retryBeforeResponseCount: 1,
            responseAttemptCount: 2,
            responseCompleteCount: 1,
            firstResponseComplete: false,
            delayCompleteCount: 2,
            methodConsistent: true,
            destinationConsistent: true,
            bodyConsistent: true,
            credential: CredentialState.SourceOnly,
            cleanup: CleanupState.Succeeded);
        var findings = new List<FindingCode>
        {
            FindingCode.CaptureIncomplete,
            FindingCode.RetryBeforeResponse,
            FindingCode.RetryAfterAcceptedRequest,
            FindingCode.EffectLimitExceeded,
        };

        var row = new ScenarioResult(
            ScenarioId.DelayedResponse,
            Assessment.UnsafeBehaviorObserved,
            observation,
            findings);

        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, row.Assessment);
        CollectionAssert.AreEqual(findings, Copy(row.Findings));
    }

    [TestMethod]
    public void IncompleteChangedBodyEvidenceRemainsUnsafeWithoutACompletedEffectOrResponse()
    {
        var observation = new Observation(
            captureComplete: false,
            attemptCount: 1,
            effectCount: 0,
            overlapCount: 0,
            retryAfterEffectCount: 0,
            retryAfterUnconfirmedCount: 0,
            retryBeforeResponseCount: 0,
            responseAttemptCount: 0,
            responseCompleteCount: 0,
            firstResponseComplete: false,
            delayCompleteCount: 0,
            methodConsistent: true,
            destinationConsistent: true,
            bodyConsistent: false,
            credential: CredentialState.NotObserved,
            cleanup: CleanupState.Succeeded);
        var cases = new (ScenarioId Scenario, FindingCode[] Findings)[]
        {
            (
                ScenarioId.ChangedBodyRetry,
                [
                    FindingCode.CaptureIncomplete,
                    FindingCode.BodyChanged,
                    FindingCode.CredentialNotObserved,
                    FindingCode.EffectNotObserved,
                ]),
            (
                ScenarioId.RetryLimit,
                [
                    FindingCode.CaptureIncomplete,
                    FindingCode.ResponseIncomplete,
                    FindingCode.BodyChanged,
                    FindingCode.CredentialNotObserved,
                ]),
            (
                ScenarioId.DisconnectBeforeAcceptance,
                [
                    FindingCode.CaptureIncomplete,
                    FindingCode.BodyChanged,
                    FindingCode.CredentialNotObserved,
                ]),
        };

        foreach (var item in cases)
        {
            var row = new ScenarioResult(
                item.Scenario,
                Assessment.UnsafeBehaviorObserved,
                observation,
                item.Findings);

            Assert.AreEqual(Assessment.UnsafeBehaviorObserved, row.Assessment, item.Scenario.ToString());
            CollectionAssert.Contains(Copy(row.Findings), FindingCode.BodyChanged);
        }
    }

    [TestMethod]
    public void SuiteValidationRecomputesUnsafeOverInconclusiveAggregate()
    {
        var rows = PositiveRows();
        rows[3] = new ScenarioResult(
            ScenarioId.CrossOriginRedirectCredentials,
            Assessment.UnsafeBehaviorObserved,
            new Observation(
                true, 2, 1, 0, 0, 0, 0, 2, 2, true, 0,
                true, true, true, CredentialState.ExposedAtTarget, CleanupState.Succeeded),
            new List<FindingCode> { FindingCode.CredentialExposedAtTarget });
        rows[5] = new ScenarioResult(
            ScenarioId.DelayedResponse,
            Assessment.Inconclusive,
            UnrunObservation(CleanupState.Failed),
            new List<FindingCode>
            {
                FindingCode.AttemptNotObserved,
                FindingCode.CaptureIncomplete,
                FindingCode.CleanupUnverified,
            });

        var result = new SuiteResult(Assessment.UnsafeBehaviorObserved, rows);
        ScenarioSuite.Validate(result);

        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment);
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Scenarios[3].Assessment);
        Assert.AreEqual(Assessment.Inconclusive, result.Scenarios[5].Assessment);
    }

    [TestMethod]
    public void ConstructorsRejectInvalidValuesOnlyWithInvalidResult()
    {
        AssertInvalid(() => new Observation(
            true, 4, 1, 0, 0, 0, 0, 0, 0, false, 0,
            true, true, true, CredentialState.SourceOnly, CleanupState.Succeeded));
        AssertInvalid(() => new Observation(
            false, 0, 0, 0, 0, 0, 0, 0, 0, false, 0,
            true, true, true, (CredentialState)0, CleanupState.Succeeded));
        AssertInvalid(() => new Observation(
            false, 0, 0, 0, 0, 0, 0, 0, 0, false, 0,
            false, true, true, CredentialState.NotObserved, CleanupState.Succeeded));

        var retryLimit = PositiveObservation(2, 0, 2, 2, true, 0, CredentialState.SourceOnly);
        AssertInvalid(() => new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.NoUnsafeBehaviorObserved,
            retryLimit,
            new List<FindingCode>()));
        AssertInvalid(() => new ScenarioResult(
            (ScenarioId)0,
            Assessment.NoUnsafeBehaviorObserved,
            PositiveRows()[0].Observation,
            new List<FindingCode>()));
        AssertInvalid(() => new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.Inconclusive,
            PositiveRows()[0].Observation,
            new List<FindingCode>()));
        AssertInvalid(() => new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.NoUnsafeBehaviorObserved,
            PositiveRows()[0].Observation,
            new List<FindingCode> { FindingCode.ScenarioIncomplete }));
        AssertInvalid(() => new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.NoUnsafeBehaviorObserved,
            null!,
            new List<FindingCode>()));
        AssertInvalid(() => new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.NoUnsafeBehaviorObserved,
            PositiveRows()[0].Observation,
            null!));

        var rows = PositiveRows();
        AssertInvalid(() => new SuiteResult(Assessment.Inconclusive, rows));
        AssertInvalid(() => new SuiteResult(Assessment.NoUnsafeBehaviorObserved, rows.GetRange(0, 5)));
        var reordered = PositiveRows();
        (reordered[0], reordered[1]) = (reordered[1], reordered[0]);
        AssertInvalid(() => new SuiteResult(Assessment.NoUnsafeBehaviorObserved, reordered));
        AssertInvalid(() => new SuiteResult(Assessment.NoUnsafeBehaviorObserved, null!));
        AssertInvalid(() => ScenarioSuite.Validate(null!));
    }

    [TestMethod]
    public void ConstructorCollectionsCannotBeMutatedByTheCaller()
    {
        var suppliedFindings = new List<FindingCode>();
        var first = new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.NoUnsafeBehaviorObserved,
            PositiveRows()[0].Observation,
            suppliedFindings);
        var second = new ScenarioResult(
            ScenarioId.DisconnectBeforeAcceptance,
            Assessment.NoUnsafeBehaviorObserved,
            PositiveRows()[1].Observation,
            new List<FindingCode>());
        suppliedFindings.Add(FindingCode.BodyChanged);

        Assert.AreEqual(0, first.Findings.Count);
        Assert.IsFalse(ReferenceEquals(suppliedFindings, first.Findings));
        Assert.IsFalse(first.Findings is FindingCode[]);
        Assert.IsFalse(first.Findings is IList<FindingCode>);
        Assert.IsFalse(ReferenceEquals(first.Findings, second.Findings));

        var suppliedRows = PositiveRows();
        var result = new SuiteResult(Assessment.NoUnsafeBehaviorObserved, suppliedRows);
        suppliedRows.Clear();

        Assert.AreEqual(6, result.Scenarios.Count);
        Assert.IsFalse(ReferenceEquals(suppliedRows, result.Scenarios));
        Assert.IsFalse(result.Scenarios is ScenarioResult[]);
        Assert.IsFalse(result.Scenarios is IList<ScenarioResult>);
        ScenarioSuite.Validate(result);
    }

    [TestMethod]
    public void CallerCollectionFailuresAreSanitized()
    {
        var positive = PositiveRows();
        AssertInvalid(() => new ScenarioResult(
            ScenarioId.AcceptThenDisconnect,
            Assessment.NoUnsafeBehaviorObserved,
            positive[0].Observation,
            new ThrowingReadOnlyList<FindingCode>()));
        AssertInvalid(() => new SuiteResult(
            Assessment.NoUnsafeBehaviorObserved,
            new ThrowingReadOnlyList<ScenarioResult>()));
    }

    private static void AssertInvalid(Action action)
    {
        SuiteException? failure = null;
        try
        {
            action();
        }
        catch (SuiteException exception)
        {
            failure = exception;
        }

        Assert.IsNotNull(failure);
        var retainedFailure = failure!;
        Assert.AreEqual(SuiteFailureCode.InvalidResult, retainedFailure.Code);
        Assert.AreEqual("HTTP scenario suite result is invalid", retainedFailure.Message);
        Assert.IsNull(retainedFailure.InnerException);
        Assert.IsNull(retainedFailure.StackTrace);
        Assert.AreEqual(retainedFailure.Message, retainedFailure.ToString());
        Assert.IsFalse(retainedFailure.Message.Contains("SEMANTIC-COLLECTION-MARKER", StringComparison.Ordinal));
    }

    private static List<ScenarioResult> PositiveRows()
    {
        return new List<ScenarioResult>
        {
            PositiveRow(ScenarioId.AcceptThenDisconnect, PositiveObservation(1, 1, 0, 0, false, 0, CredentialState.SourceOnly)),
            PositiveRow(ScenarioId.DisconnectBeforeAcceptance, PositiveObservation(1, 0, 0, 0, false, 0, CredentialState.SourceOnly)),
            PositiveRow(ScenarioId.ChangedBodyRetry, PositiveObservation(1, 1, 0, 0, false, 0, CredentialState.SourceOnly)),
            PositiveRow(ScenarioId.CrossOriginRedirectCredentials, PositiveObservation(2, 1, 2, 2, true, 0, CredentialState.AbsentAtTarget)),
            PositiveRow(ScenarioId.RetryLimit, PositiveObservation(2, 0, 2, 2, true, 0, CredentialState.SourceOnly)),
            PositiveRow(ScenarioId.DelayedResponse, PositiveObservation(1, 1, 1, 1, true, 1, CredentialState.SourceOnly)),
        };
    }

    private static ScenarioResult PositiveRow(ScenarioId scenario, Observation observation)
    {
        return new ScenarioResult(
            scenario,
            Assessment.NoUnsafeBehaviorObserved,
            observation,
            new List<FindingCode>());
    }

    private static Observation PositiveObservation(
        uint attempts,
        ulong effects,
        uint responseAttempts,
        uint responseCompletions,
        bool firstResponseComplete,
        uint delays,
        CredentialState credential)
    {
        return new Observation(
            true,
            attempts,
            effects,
            0,
            0,
            0,
            0,
            responseAttempts,
            responseCompletions,
            firstResponseComplete,
            delays,
            true,
            true,
            true,
            credential,
            CleanupState.Succeeded);
    }

    private static Observation UnrunObservation(CleanupState cleanup)
    {
        return new Observation(
            false,
            0,
            0,
            0,
            0,
            0,
            0,
            0,
            0,
            false,
            0,
            true,
            true,
            true,
            CredentialState.NotObserved,
            cleanup);
    }

    private static List<FindingCode> Copy(IReadOnlyList<FindingCode> source)
    {
        var copy = new List<FindingCode>(source.Count);
        for (var index = 0; index < source.Count; index++)
        {
            copy.Add(source[index]);
        }

        return copy;
    }

    private sealed class ThrowingReadOnlyList<T> : IReadOnlyList<T>
    {
        public int Count => throw new InvalidOperationException("SEMANTIC-COLLECTION-MARKER");

        public T this[int index] => throw new InvalidOperationException("SEMANTIC-COLLECTION-MARKER");

        public IEnumerator<T> GetEnumerator()
        {
            throw new InvalidOperationException("SEMANTIC-COLLECTION-MARKER");
        }

        IEnumerator IEnumerable.GetEnumerator() => GetEnumerator();
    }
}
