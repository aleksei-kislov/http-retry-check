using System;
using System.Collections.Generic;
using System.Linq;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Runtime;

[TestClass]
public sealed class ScenarioRuntimeTests
{
    [TestMethod]
    public async Task OrdinaryExplicitHttpClientProducesSixPositiveRows()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient();

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(
            Assessment.NoUnsafeBehaviorObserved,
            result.Assessment,
            Describe(result));
        Assert.AreEqual(6, result.Scenarios.Count);
        Assert.IsTrue(result.Scenarios.All(
            row => row.Assessment == Assessment.NoUnsafeBehaviorObserved &&
                row.Findings.Count == 0 &&
                row.Observation.AttemptLimit == 2 &&
                row.Observation.Protocol == "HTTP/1.1"));
    }

    [TestMethod]
    public void ScenarioOptionsExposeTheDocumentedDefaults()
    {
        var options = new ScenarioOptions();

        Assert.AreEqual(TimeSpan.FromSeconds(5), options.ScenarioTimeout);
        Assert.AreEqual(TimeSpan.FromSeconds(2), options.ConnectionTimeout);
        Assert.AreEqual(TimeSpan.FromMilliseconds(300), options.QuietWindow);
        Assert.AreEqual((uint)2, options.AttemptLimit);
    }

    [TestMethod]
    public async Task InvalidScenarioOptionsReturnOnlyInvalidCall()
    {
        ScenarioOptions?[] cases =
        [
            null,
            new ScenarioOptions(scenarioTimeout: TimeSpan.FromMilliseconds(-1)),
            new ScenarioOptions(scenarioTimeout: TimeSpan.Zero),
            new ScenarioOptions(scenarioTimeout: TimeSpan.FromMinutes(1) + TimeSpan.FromTicks(1)),
            new ScenarioOptions(connectionTimeout: TimeSpan.FromMilliseconds(-1)),
            new ScenarioOptions(connectionTimeout: TimeSpan.Zero),
            new ScenarioOptions(connectionTimeout: TimeSpan.FromMinutes(1) + TimeSpan.FromTicks(1)),
            new ScenarioOptions(quietWindow: TimeSpan.FromMilliseconds(-1)),
            new ScenarioOptions(quietWindow: TimeSpan.Zero),
            new ScenarioOptions(quietWindow: TimeSpan.FromMinutes(1) + TimeSpan.FromTicks(1)),
            new ScenarioOptions(attemptLimit: 0),
            new ScenarioOptions(attemptLimit: 4),
        ];
        using var client = RuntimeTestClients.CreateOrdinaryClient();

        foreach (var options in cases)
        {
            var task = ScenarioSuite.RunAsync(client, options!, CancellationToken.None);
            Assert.IsNotNull(task);
            var exception = await CaptureSuiteExceptionAsync(task);
            Assert.AreEqual(SuiteFailureCode.InvalidCall, exception.Code);
            Assert.AreEqual("HTTP scenario suite call is invalid", exception.Message);
            Assert.IsNull(exception.InnerException);
            Assert.IsNull(exception.StackTrace);
            Assert.AreEqual(exception.Message, exception.ToString());
            Assert.IsFalse(task.IsCanceled);
        }
    }

    [TestMethod]
    public async Task CustomOptionsReachAllSixScenarios()
    {
        var quietDurations = new List<TimeSpan>();
        Task ObserveAsync(TimeSpan duration, CancellationToken _)
        {
            quietDurations.Add(duration);
            return Task.CompletedTask;
        }

        var options = new ScenarioOptions(
            scenarioTimeout: TimeSpan.FromMinutes(1),
            connectionTimeout: TimeSpan.FromMinutes(1),
            quietWindow: TimeSpan.FromMinutes(1),
            attemptLimit: 3);
        using var client = RuntimeTestClients.CreateOrdinaryClient();
        var result = await RunWithRuntimeDependenciesAsync(
            client,
            CancellationToken.None,
            admissionCallback: null,
            ObserveAsync,
            CompleteImmediatelyAsync,
            options);

        ScenarioSuite.Validate(result);
        Assert.HasCount(6, quietDurations);
        Assert.IsTrue(quietDurations.All(duration => duration == TimeSpan.FromMinutes(1)));
        Assert.IsTrue(result.Scenarios.All(row => row.Observation.AttemptLimit == 3));
    }

    [TestMethod]
    public async Task AttemptAboveConfiguredLimitRemainsValidUnsafeEvidence()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new FixedAttemptHandler(4));
        var result = await ScenarioSuite.RunAsync(
            client,
            new ScenarioOptions(attemptLimit: 3));

        ScenarioSuite.Validate(result);
        var first = result.Scenarios[0];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, first.Assessment, Describe(result));
        Assert.AreEqual((uint)4, first.Observation.AttemptCount, Describe(result));
        Assert.AreEqual((uint)3, first.Observation.AttemptLimit, Describe(result));
        Assert.AreEqual("HTTP/1.1", first.Observation.Protocol, Describe(result));
        CollectionAssert.Contains(
            new List<FindingCode>(first.Findings),
            FindingCode.AttemptLimitExceeded);
    }

    [TestMethod]
    public async Task FiniteHttpClientTimeoutProducesAnAllowedDelayedOutcome()
    {
        using var handler = new SixthCallTimeoutHandler();
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);
        client.Timeout = TimeSpan.FromMilliseconds(200);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var delayed = result.Scenarios[5];
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, delayed.Assessment, Describe(result));
        Assert.AreEqual((uint)1, delayed.Observation.AttemptCount);
        Assert.AreEqual((ulong)1, delayed.Observation.EffectCount);
        Assert.AreEqual((uint)1, delayed.Observation.DelayCompleteCount);
        Assert.AreEqual((uint)1, delayed.Observation.ResponseAttemptCount);
        Assert.IsTrue(delayed.Observation.ResponseCompleteCount is 0 or 1);
        Assert.AreEqual(
            delayed.Observation.ResponseCompleteCount == 1,
            delayed.Observation.FirstResponseComplete);
        Assert.IsTrue(delayed.Observation.CaptureComplete);
        Assert.AreEqual(CleanupState.Succeeded, delayed.Observation.Cleanup);
        Assert.AreEqual(0, delayed.Findings.Count);
        Assert.AreEqual(6, handler.Calls);
        Assert.IsTrue(handler.TimeoutObserved);
    }

    [TestMethod]
    public async Task DelayedResponseWaitUsesTheScenarioClock()
    {
        var waits = 0;
        TimeSpan observedDuration = default;
        CancellationToken observedToken = default;
        Task WaitAsync(TimeSpan duration, CancellationToken cancellationToken)
        {
            waits++;
            observedDuration = duration;
            observedToken = cancellationToken;
            return Task.CompletedTask;
        }

        using var handler = new ScenarioTokenRecordingHandler();
        using var invoker = new HttpMessageInvoker(handler, disposeHandler: false);
        var result = await RunWithDelayedResponseWaiterAsync(invoker, WaitAsync);

        ScenarioSuite.Validate(result);
        var delayed = result.Scenarios[5];
        Assert.AreEqual(1, waits);
        Assert.AreEqual(TimeSpan.FromMilliseconds(250), observedDuration);
        Assert.IsTrue(observedToken.CanBeCanceled);
        Assert.AreEqual(handler.DelayedScenarioToken, observedToken);
        Assert.AreEqual((uint)1, delayed.Observation.DelayCompleteCount, Describe(result));
        Assert.AreEqual((uint)1, delayed.Observation.ResponseAttemptCount, Describe(result));
    }

    [TestMethod]
    public async Task PostInvocationQuietObservationUsesTheDefaultDuration()
    {
        var durations = new List<TimeSpan>();
        var tokens = new List<CancellationToken>();
        Task ObserveAsync(TimeSpan duration, CancellationToken cancellationToken)
        {
            durations.Add(duration);
            tokens.Add(cancellationToken);
            return Task.CompletedTask;
        }

        using var client = RuntimeTestClients.CreateOrdinaryClient();
        var result = await RunWithRuntimeDependenciesAsync(
            client,
            CancellationToken.None,
            admissionCallback: null,
            ObserveAsync,
            CompleteImmediatelyAsync);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, result.Assessment, Describe(result));
        Assert.HasCount(6, durations);
        Assert.IsTrue(durations.All(duration => duration == TimeSpan.FromMilliseconds(300)));
        Assert.AreEqual(
            TimeSpan.FromMilliseconds(1800),
            durations.Aggregate(TimeSpan.Zero, (sum, duration) => sum + duration));
        Assert.IsTrue(tokens.All(token => token.CanBeCanceled));
    }

    [TestMethod]
    public async Task DetachedRetryAfterInvocationReturnsIsObserved()
    {
        using var handler = new DetachedBackgroundRetryHandler();
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);

        var result = await ScenarioSuite.RunAsync(client);
        await handler.BackgroundRetry.WaitAsync(TimeSpan.FromSeconds(1));

        ScenarioSuite.Validate(result);
        Assert.IsTrue(handler.RetryScheduled);
        Assert.IsTrue(handler.BackgroundRetry.IsCompletedSuccessfully);
        var first = result.Scenarios[0];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, first.Assessment, Describe(result));
        Assert.AreEqual((uint)2, first.Observation.AttemptCount, Describe(result));
        Assert.AreEqual((ulong)2, first.Observation.EffectCount, Describe(result));
        Assert.AreEqual((uint)1, first.Observation.RetryAfterEffectCount, Describe(result));
        Assert.IsTrue(first.Observation.CaptureComplete, Describe(result));
        CollectionAssert.Contains(
            new List<FindingCode>(first.Findings),
            FindingCode.RetryAfterAcceptedRequest);
    }

    [TestMethod]
    public async Task CallerRetryAfterAcceptedDisconnectPreservesUnsafeEvidence()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryingHandler());

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment);
        var first = result.Scenarios[0];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, first.Assessment);
        CollectionAssert.Contains(
            new List<FindingCode>(first.Findings),
            FindingCode.RetryAfterAcceptedRequest);
        Assert.AreEqual((uint)2, first.Observation.AttemptCount);
        Assert.AreEqual((ulong)2, first.Observation.EffectCount);
        var changed = result.Scenarios[2];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, changed.Assessment);
        Assert.IsFalse(changed.Observation.BodyConsistent);
        CollectionAssert.Contains(
            new List<FindingCode>(changed.Findings),
            FindingCode.BodyChanged);
    }

    [TestMethod]
    public async Task BareConnectionAfterTransportFailureDoesNotCreateRetryCausality()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new BareConnectionProbeHandler());

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        foreach (var index in new[] { 0, 1, 2 })
        {
            var row = result.Scenarios[index];
            Assert.AreEqual(Assessment.Inconclusive, row.Assessment, Describe(result));
            Assert.AreEqual((uint)2, row.Observation.AttemptCount, Describe(result));
            Assert.AreEqual(string.Empty, row.Observation.Protocol, Describe(result));
            Assert.AreEqual((uint)0, row.Observation.RetryAfterEffectCount, Describe(result));
            Assert.AreEqual((uint)0, row.Observation.RetryAfterUnconfirmedCount, Describe(result));
            Assert.AreEqual((uint)0, row.Observation.RetryBeforeResponseCount, Describe(result));
            CollectionAssert.Contains(
                new List<FindingCode>(row.Findings),
                FindingCode.CaptureIncomplete);
            Assert.IsFalse(row.Findings.Contains(FindingCode.RetryAfterAcceptedRequest));
            Assert.IsFalse(row.Findings.Contains(FindingCode.RetryAfterUnconfirmedAcceptance));
        }
    }

    [TestMethod]
    public async Task CompleteReplayAfterTransportFailureStillRecordsRetryCausality()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryingHandler());

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var accepted = result.Scenarios[0];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, accepted.Assessment, Describe(result));
        Assert.AreEqual((uint)2, accepted.Observation.AttemptCount, Describe(result));
        Assert.AreEqual((uint)1, accepted.Observation.RetryAfterEffectCount, Describe(result));
        CollectionAssert.Contains(
            new List<FindingCode>(accepted.Findings),
            FindingCode.RetryAfterAcceptedRequest);

        var uncertain = result.Scenarios[1];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, uncertain.Assessment, Describe(result));
        Assert.AreEqual((uint)2, uncertain.Observation.AttemptCount, Describe(result));
        Assert.AreEqual((uint)1, uncertain.Observation.RetryAfterUnconfirmedCount, Describe(result));
        CollectionAssert.Contains(
            new List<FindingCode>(uncertain.Findings),
            FindingCode.RetryAfterUnconfirmedAcceptance);

        var changed = result.Scenarios[2];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, changed.Assessment, Describe(result));
        Assert.AreEqual((uint)2, changed.Observation.AttemptCount, Describe(result));
        Assert.AreEqual((uint)1, changed.Observation.RetryAfterEffectCount, Describe(result));
        CollectionAssert.Contains(
            new List<FindingCode>(changed.Findings),
            FindingCode.RetryAfterAcceptedRequest);
    }

    [TestMethod]
    public async Task RedirectRefusalProducesAPassingResult()
    {
        using var handler = RuntimeTestClients.CreateSocketsHandler();
        handler.AllowAutoRedirect = false;
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, redirect.Assessment, Describe(result));
        Assert.AreEqual((uint)1, redirect.Observation.AttemptCount);
        Assert.AreEqual(CredentialState.SourceOnly, redirect.Observation.Credential);
    }

    [TestMethod]
    public async Task CredentialPreservingRedirectIsUnsafe()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RedirectExposureHandler());

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment, Describe(result));
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, redirect.Assessment, Describe(result));
        Assert.AreEqual(CredentialState.ExposedAtTarget, redirect.Observation.Credential);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CredentialExposedAtTarget);
    }

    [TestMethod]
    [DataRow((int)RedirectCredentialMode.Prefix)]
    [DataRow((int)RedirectCredentialMode.Suffix)]
    [DataRow((int)RedirectCredentialMode.MultipleWithSuffix)]
    [DataRow((int)RedirectCredentialMode.Cookie)]
    [DataRow((int)RedirectCredentialMode.CustomHeader)]
    public async Task RedirectContainingSyntheticCredentialIsUnsafe(int modeValue)
    {
        var mode = (RedirectCredentialMode)modeValue;
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RedirectExposureHandler(mode));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment, Describe(result));
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, redirect.Assessment, Describe(result));
        Assert.AreEqual(CredentialState.ExposedAtTarget, redirect.Observation.Credential);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CredentialExposedAtTarget);
    }

    [TestMethod]
    public async Task MalformedRedirectTargetStillPreservesCredentialExposure()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(
            new RedirectExposureHandler(RedirectCredentialMode.MalformedTarget));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment, Describe(result));
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, redirect.Assessment, Describe(result));
        Assert.AreEqual(CredentialState.ExposedAtTarget, redirect.Observation.Credential);
        Assert.IsFalse(redirect.Observation.CaptureComplete);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CredentialExposedAtTarget);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CaptureIncomplete);
    }

    [TestMethod]
    public async Task UnrelatedRedirectCredentialIsNotExposure()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(
            new RedirectExposureHandler(RedirectCredentialMode.Unrelated));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, result.Assessment, Describe(result));
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, redirect.Assessment, Describe(result));
        Assert.AreEqual(CredentialState.AbsentAtTarget, redirect.Observation.Credential);
        CollectionAssert.DoesNotContain(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CredentialExposedAtTarget);
    }

    [TestMethod]
    public async Task SourceCredentialValidationStillRequiresOneExactValue()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(
            new RedirectExposureHandler(RedirectCredentialMode.DuplicateSource));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment, Describe(result));
        Assert.AreEqual(Assessment.Inconclusive, redirect.Assessment, Describe(result));
        Assert.AreEqual(CredentialState.Missing, redirect.Observation.Credential);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CredentialMissing);
        CollectionAssert.DoesNotContain(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CredentialExposedAtTarget);
    }

    [TestMethod]
    public async Task TwoRetryableResponsesProduceAPassingResult()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryStatusHandler(2));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)2, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)2, retry.Observation.ResponseCompleteCount);
    }

    [TestMethod]
    public async Task ThreeRetryableResponsesExceedTheSuiteLimit()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryStatusHandler(3));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)3, retry.Observation.AttemptCount);
        CollectionAssert.Contains(
            new List<FindingCode>(retry.Findings),
            FindingCode.AttemptLimitExceeded);
    }

    [TestMethod]
    public async Task RedirectRefusalAfterACompleteSourceResponsePassesAndContinues()
    {
        using var handler = new ScenarioHttpRequestFailureHandler(targetCall: 4, completedAttempts: 1);
        using var client = new HttpMessageInvoker(handler, disposeHandler: false);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, result.Assessment, Describe(result));
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, redirect.Assessment, Describe(result));
        Assert.AreEqual((uint)1, redirect.Observation.AttemptCount);
        Assert.AreEqual((uint)1, redirect.Observation.ResponseCompleteCount);
        Assert.AreEqual(CredentialState.SourceOnly, redirect.Observation.Credential);
        Assert.IsTrue(redirect.Observation.CaptureComplete);
        Assert.AreEqual(6, handler.Calls);
        Assert.AreEqual((uint)1, result.Scenarios[4].Observation.AttemptCount);
        Assert.AreEqual((uint)1, result.Scenarios[5].Observation.AttemptCount);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
    }

    [TestMethod]
    public async Task RetryLimitErrorAfterTwoCompleteAttemptsPassesAndContinues()
    {
        using var handler = new ScenarioHttpRequestFailureHandler(targetCall: 5, completedAttempts: 2);
        using var client = new HttpMessageInvoker(handler, disposeHandler: false);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, result.Assessment, Describe(result));
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)2, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)2, retry.Observation.ResponseCompleteCount);
        Assert.IsTrue(retry.Observation.CaptureComplete);
        Assert.AreEqual(6, handler.Calls);
        Assert.AreEqual((uint)1, result.Scenarios[5].Observation.AttemptCount);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
    }

    [TestMethod]
    public async Task RetryLimitErrorAfterThreeCompleteAttemptsPreservesUnsafeEvidenceAndContinues()
    {
        using var handler = new ScenarioHttpRequestFailureHandler(targetCall: 5, completedAttempts: 3);
        using var client = new HttpMessageInvoker(handler, disposeHandler: false);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment, Describe(result));
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)3, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)3, retry.Observation.ResponseCompleteCount);
        Assert.IsTrue(retry.Observation.CaptureComplete);
        CollectionAssert.Contains(
            new List<FindingCode>(retry.Findings),
            FindingCode.AttemptLimitExceeded);
        Assert.AreEqual(6, handler.Calls);
        Assert.AreEqual((uint)1, result.Scenarios[5].Observation.AttemptCount);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
    }

    [TestMethod]
    public async Task HttpRequestErrorWithoutAnAttemptIsInconclusiveAndStops()
    {
        using var handler = new ScenarioHttpRequestFailureHandler(targetCall: 4, completedAttempts: 0);
        using var client = new HttpMessageInvoker(handler, disposeHandler: false);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment, Describe(result));
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.Inconclusive, redirect.Assessment, Describe(result));
        Assert.AreEqual((uint)0, redirect.Observation.AttemptCount);
        Assert.IsFalse(redirect.Observation.CaptureComplete);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.AttemptNotObserved);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CaptureIncomplete);
        Assert.AreEqual(4, handler.Calls);
        Assert.AreEqual((uint)0, result.Scenarios[4].Observation.AttemptCount);
        Assert.AreEqual((uint)0, result.Scenarios[5].Observation.AttemptCount);
    }

    [TestMethod]
    public async Task SynchronousAndAsynchronousHttpRequestErrorsProduceTheSameEvidence()
    {
        var asynchronous = await RunRedirectHttpRequestFailureAsync(
            HttpRequestFailureDelivery.Asynchronous);
        var synchronous = await RunRedirectHttpRequestFailureAsync(
            HttpRequestFailureDelivery.Synchronous);

        CollectionAssert.AreEqual(
            ScenarioReports.Encode(ScenarioReports.Create(asynchronous)),
            ScenarioReports.Encode(ScenarioReports.Create(synchronous)));
    }

    [TestMethod]
    public async Task FourthRetryIsRejectedAndMakesCaptureIncomplete()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryStatusHandler(4));

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)3, retry.Observation.AttemptCount);
        Assert.IsFalse(retry.Observation.CaptureComplete);
        CollectionAssert.Contains(new List<FindingCode>(retry.Findings), FindingCode.AttemptLimitExceeded);
        CollectionAssert.Contains(new List<FindingCode>(retry.Findings), FindingCode.CaptureIncomplete);
    }

    [TestMethod]
    public async Task AcceptDisconnectPreEffectOverlapDoesNotFabricateRetryCausality()
    {
        await AssertPreEffectOverlapAsync(
            RawOriginControlMode.AcceptPreEffectOverlap,
            scenarioIndex: 0);
    }

    [TestMethod]
    public async Task ChangedBodyPreEffectOverlapDoesNotFabricateRetryCausality()
    {
        await AssertPreEffectOverlapAsync(
            RawOriginControlMode.ChangedBodyPreEffectOverlap,
            scenarioIndex: 2);
    }

    [TestMethod]
    public async Task TruncatedChangedReplayPreservesUnsafeBodyEvidence()
    {
        var result = await RunRawControlAsync(RawOriginControlMode.ChangedBodyTruncatedReplay);

        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment, Describe(result));
        var changed = result.Scenarios[2];
        Assert.AreEqual(ScenarioId.ChangedBodyRetry, changed.Scenario);
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, changed.Assessment, Describe(result));
        Assert.AreEqual((uint)2, changed.Observation.AttemptCount);
        Assert.AreEqual((ulong)1, changed.Observation.EffectCount);
        Assert.AreEqual((uint)1, changed.Observation.RetryAfterEffectCount);
        Assert.IsFalse(changed.Observation.CaptureComplete);
        Assert.IsFalse(changed.Observation.BodyConsistent);
        CollectionAssert.Contains(
            new List<FindingCode>(changed.Findings),
            FindingCode.RetryAfterAcceptedRequest);
        CollectionAssert.Contains(
            new List<FindingCode>(changed.Findings),
            FindingCode.BodyChanged);
        CollectionAssert.Contains(
            new List<FindingCode>(changed.Findings),
            FindingCode.CaptureIncomplete);
    }

    [TestMethod]
    public async Task DelayedConcurrentRetryRecordsOverlapAndRetryBeforeResponse()
    {
        var result = await RunRawControlAsync(RawOriginControlMode.DelayedOverlap);

        var delayed = result.Scenarios[5];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, delayed.Assessment, Describe(result));
        Assert.AreEqual((uint)2, delayed.Observation.AttemptCount);
        Assert.AreEqual((uint)1, delayed.Observation.OverlapCount);
        Assert.AreEqual((uint)1, delayed.Observation.RetryBeforeResponseCount);
        Assert.AreEqual((uint)1, delayed.Observation.RetryAfterEffectCount);
        CollectionAssert.Contains(new List<FindingCode>(delayed.Findings), FindingCode.RetryBeforeResponse);
        CollectionAssert.Contains(new List<FindingCode>(delayed.Findings), FindingCode.RetryAfterAcceptedRequest);
    }

    [TestMethod]
    public async Task SameConnectionTrailingInputMakesCaptureIncomplete()
    {
        var result = await RunRawControlAsync(RawOriginControlMode.SameConnectionTrailing);

        var delayed = result.Scenarios[5];
        Assert.AreEqual(Assessment.Inconclusive, delayed.Assessment, Describe(result));
        Assert.AreEqual((uint)1, delayed.Observation.AttemptCount);
        Assert.AreEqual((ulong)1, delayed.Observation.EffectCount);
        Assert.AreEqual((uint)1, delayed.Observation.DelayCompleteCount);
        Assert.AreEqual((uint)1, delayed.Observation.ResponseAttemptCount);
        Assert.AreEqual((uint)1, delayed.Observation.ResponseCompleteCount);
        Assert.IsTrue(delayed.Observation.FirstResponseComplete);
        Assert.AreEqual((uint)0, delayed.Observation.OverlapCount);
        Assert.AreEqual((uint)0, delayed.Observation.RetryAfterEffectCount);
        Assert.AreEqual((uint)0, delayed.Observation.RetryAfterUnconfirmedCount);
        Assert.AreEqual((uint)0, delayed.Observation.RetryBeforeResponseCount);
        Assert.IsFalse(delayed.Observation.CaptureComplete);
        Assert.AreEqual(CleanupState.Succeeded, delayed.Observation.Cleanup);
        CollectionAssert.Contains(new List<FindingCode>(delayed.Findings), FindingCode.CaptureIncomplete);
    }

    [TestMethod]
    public async Task MalformedHttpOneHeaderIsInconclusive()
    {
        var result = await RunRawControlAsync(RawOriginControlMode.MalformedHeader);

        var retry = result.Scenarios[4];
        Assert.AreEqual((uint)1, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)0, retry.Observation.ResponseAttemptCount);
        Assert.AreEqual(string.Empty, retry.Observation.Protocol);
        Assert.IsFalse(retry.Observation.CaptureComplete);
    }

    [TestMethod]
    public async Task HeaderLimitIsAcceptedAndTheNextByteIsInconclusive()
    {
        var exact = await RunRawControlAsync(RawOriginControlMode.ExactHeaderLimit);
        var exceeded = await RunRawControlAsync(RawOriginControlMode.HeaderLimitExceeded);

        var exactRetry = exact.Scenarios[4];
        Assert.IsTrue(exactRetry.Observation.CaptureComplete, Describe(exact));
        Assert.AreEqual((uint)1, exactRetry.Observation.ResponseAttemptCount);
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, exactRetry.Assessment, Describe(exact));
        var exceededRetry = exceeded.Scenarios[4];
        Assert.AreEqual((uint)1, exceededRetry.Observation.AttemptCount);
        Assert.IsFalse(exceededRetry.Observation.CaptureComplete);
        Assert.AreEqual((uint)0, exceededRetry.Observation.ResponseAttemptCount);
    }

    [TestMethod]
    public async Task BodyLimitIsCapturedAndTheNextByteIsInconclusive()
    {
        var exact = await RunRawControlAsync(RawOriginControlMode.ExactBodyLimit);
        var exceeded = await RunRawControlAsync(RawOriginControlMode.BodyLimitExceeded);

        var exactRetry = exact.Scenarios[4];
        Assert.IsTrue(exactRetry.Observation.CaptureComplete, Describe(exact));
        Assert.IsFalse(exactRetry.Observation.BodyConsistent, Describe(exact));
        Assert.AreEqual((uint)1, exactRetry.Observation.ResponseAttemptCount, Describe(exact));
        CollectionAssert.Contains(new List<FindingCode>(exactRetry.Findings), FindingCode.BodyChanged);
        var exceededRetry = exceeded.Scenarios[4];
        Assert.AreEqual((uint)1, exceededRetry.Observation.AttemptCount, Describe(exceeded));
        Assert.IsFalse(exceededRetry.Observation.CaptureComplete, Describe(exceeded));
        Assert.AreEqual((uint)0, exceededRetry.Observation.ResponseAttemptCount, Describe(exceeded));
    }

    [TestMethod]
    public async Task ChunkFramingConsumesTheSharedPostHeaderWireBudget()
    {
        var result = await RunRawControlAsync(RawOriginControlMode.SharedWireLimitExceeded);

        var retry = result.Scenarios[4];
        Assert.AreEqual((uint)1, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)0, retry.Observation.ResponseAttemptCount);
        Assert.IsFalse(retry.Observation.CaptureComplete);
    }

    [TestMethod]
    public async Task NoDispatchResponsesProduceAnOrderedInconclusiveSuite()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment);
        Assert.AreEqual(6, handler.Calls);
        CollectionAssert.AreEqual(
            new[]
            {
                ScenarioId.AcceptThenDisconnect,
                ScenarioId.DisconnectBeforeAcceptance,
                ScenarioId.ChangedBodyRetry,
                ScenarioId.CrossOriginRedirectCredentials,
                ScenarioId.RetryLimit,
                ScenarioId.DelayedResponse,
            },
            result.Scenarios.Select(row => row.Scenario).ToArray());
        Assert.AreEqual(6, handler.Targets.Count);
        Assert.AreEqual(
            6,
            handler.Targets.Select(target => target.Port).Distinct().Count());
        Assert.AreEqual(0, handler.DisposeCalls);
        Assert.IsTrue(result.Scenarios.All(row =>
            row.Observation.AttemptCount == 0 &&
            row.Observation.AttemptLimit == 2 &&
            row.Observation.Protocol == string.Empty &&
            !row.Observation.CaptureComplete));
    }

    [TestMethod]
    public async Task NullClientReturnsAnInvalidCallError()
    {
        var task = ScenarioSuite.RunAsync(null!);

        Assert.IsNotNull(task);
        var exception = await CaptureSuiteExceptionAsync(task);
        Assert.AreEqual(SuiteFailureCode.InvalidCall, exception.Code);
        Assert.AreEqual("HTTP scenario suite call is invalid", exception.Message);
        Assert.IsNull(exception.StackTrace);
        Assert.AreEqual(exception.Message, exception.ToString());
        Assert.IsFalse(task.IsCanceled);
    }

    [TestMethod]
    public async Task CancellationAtEntryPropagatesTheCallerToken()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();
        cancellation.Cancel();

        var task = ScenarioSuite.RunAsync(invoker, cancellation.Token);

        var exception = await CaptureCancellationAsync(task);
        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        Assert.IsTrue(task.IsCanceled);
        Assert.AreEqual(0, handler.Calls);
    }

    [TestMethod]
    public async Task CancellationBeforeFirstInvocationClosesListeners()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();
        var context = new QueuedSynchronizationContext();
        var prior = SynchronizationContext.Current;
        SynchronizationContext.SetSynchronizationContext(context);
        Task<SuiteResult> task;
        try
        {
            task = ScenarioSuite.RunAsync(invoker, cancellation.Token);
        }
        finally
        {
            SynchronizationContext.SetSynchronizationContext(prior);
        }

        cancellation.Cancel();
        context.RunOne();

        var exception = await CaptureCancellationAsync(task);
        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        Assert.AreEqual(0, handler.Calls);
        Assert.IsTrue(task.IsCanceled);
    }

    [TestMethod]
    public async Task CancellationDuringListenerAdmissionClosesOpenedListeners()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();
        var ordinals = new List<int>();
        var ports = new List<int>();
        Action<int, int> observer = (ordinal, port) =>
        {
            ordinals.Add(ordinal);
            ports.Add(port);
            cancellation.Cancel();
        };

        var task = RunWithAdmissionObserverAsync(invoker, cancellation.Token, observer);
        var exception = await CaptureCancellationAsync(task);

        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        CollectionAssert.AreEqual(new[] { 1 }, ordinals);
        Assert.HasCount(1, ports);
        Assert.AreEqual(0, handler.Calls);
        Assert.IsTrue(task.IsCanceled);
        await AssertListenerClosedAsync(ports[0]);
    }

    [TestMethod]
    public async Task CancellationBeforeFirstSendPropagatesAfterCleanup()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();
        using var observer = new AcceptStartCancellationObserver(cancellation);

        var task = ScenarioSuite.RunAsync(invoker, cancellation.Token);
        await observer.Observed.WaitAsync(TimeSpan.FromSeconds(2));
        var exception = await CaptureCancellationAsync(task);

        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        Assert.AreEqual(0, handler.Calls);
        Assert.IsTrue(task.IsCanceled);
    }

    [TestMethod]
    public async Task CancellationAfterAdmissionPropagatesAfterCleanup()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.WaitForCancellation);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();

        var task = ScenarioSuite.RunAsync(invoker, cancellation.Token);
        await handler.Entered.Task.WaitAsync(TimeSpan.FromSeconds(2));
        cancellation.Cancel();
        var exception = await CaptureCancellationAsync(task);

        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        Assert.AreEqual(1, handler.Calls);
        Assert.IsTrue(task.IsCanceled);
    }

    [TestMethod]
    public async Task CancellationDuringQuietObservationPropagatesAfterCleanup()
    {
        using var handler = new ScenarioTokenRecordingHandler();
        using var invoker = new HttpMessageInvoker(handler, disposeHandler: false);
        using var cancellation = new CancellationTokenSource();
        var quietEntered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var ports = new List<int>();

        async Task ObserveAsync(TimeSpan _, CancellationToken cancellationToken)
        {
            quietEntered.TrySetResult();
            await Task.Delay(Timeout.InfiniteTimeSpan, cancellationToken).ConfigureAwait(false);
        }

        var task = RunWithRuntimeDependenciesAsync(
            invoker,
            cancellation.Token,
            (_, port) => ports.Add(port),
            ObserveAsync,
            CompleteImmediatelyAsync);
        await quietEntered.Task.WaitAsync(TimeSpan.FromSeconds(2));
        cancellation.Cancel();

        var exception = await CaptureCancellationAsync(task);

        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        Assert.IsTrue(task.IsCanceled);
        Assert.HasCount(7, ports);
        Assert.AreEqual(7, ports.Distinct().Count());
        foreach (var port in ports)
        {
            await AssertListenerClosedAsync(port);
        }
    }

    [TestMethod]
    public async Task CancellationDuringSecondScenarioPropagatesAfterFirstPositiveRowAndClosesListeners()
    {
        using var handler = new PauseSecondSendHandler(RuntimeTestClients.CreateSocketsHandler());
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);
        using var cancellation = new CancellationTokenSource();
        var ports = new List<int>();

        var task = RunWithAdmissionObserverAsync(
            client,
            cancellation.Token,
            (_, port) => ports.Add(port));
        await handler.SecondSendStarted.Task.WaitAsync(TimeSpan.FromSeconds(2));
        cancellation.Cancel();

        var exception = await CaptureCancellationAsync(task);

        Assert.AreEqual(cancellation.Token, exception.CancellationToken);
        Assert.IsTrue(task.IsCanceled);
        Assert.AreEqual(2, handler.Calls);
        Assert.IsTrue(handler.FirstSendCompleted);
        Assert.HasCount(7, ports);
        Assert.AreEqual(7, ports.Distinct().Count());
        foreach (var port in ports)
        {
            await AssertListenerClosedAsync(port);
        }
    }

    [TestMethod]
    public async Task SynchronousSenderExceptionPropagatesAfterClosingListeners()
    {
        await AssertCallerFailureAfterCleanupAsync(ControlledHandlerMode.SynchronousException);
    }

    [TestMethod]
    public async Task AsynchronousSenderExceptionPropagatesAfterClosingListeners()
    {
        await AssertCallerFailureAfterCleanupAsync(ControlledHandlerMode.AsynchronousException);
    }

    [TestMethod]
    public async Task NullTaskReturnsAnInconclusiveResult()
    {
        await AssertInvocationFailureAsync(ControlledHandlerMode.NullTask);
    }

    [TestMethod]
    public async Task CanceledTaskReturnsAnInconclusiveResult()
    {
        await AssertInvocationFailureAsync(ControlledHandlerMode.CanceledTask);
    }

    [TestMethod]
    public async Task NullResponseReturnsAnInconclusiveResult()
    {
        await AssertInvocationFailureAsync(ControlledHandlerMode.NullResponse);
    }

    [TestMethod]
    public async Task ResponseDisposalExceptionPropagatesAfterClosingListeners()
    {
        await AssertCallerFailureAfterCleanupAsync(ControlledHandlerMode.DisposalFailure);
    }

    [TestMethod]
    public async Task CallerOwnedRequestContentDisposalExceptionPropagatesAfterClosingListeners()
    {
        await AssertCallerFailureAfterCleanupAsync(ControlledHandlerMode.ReplaceWithThrowingContent);
    }

    [TestMethod]
    public async Task DisposedReplayContentExceptionPropagatesAfterClosingListeners()
    {
        await AssertCallerFailureAfterCleanupAsync(ControlledHandlerMode.DisposeThenReplayContent);
    }

    [TestMethod]
    public async Task ResponseAndHandlerMarkersAreNotRetainedInResults()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.MarkerResponse);
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        var projection = Describe(result) + result + handler.GetType().Namespace;
        Assert.IsFalse(projection.Contains("marker", StringComparison.OrdinalIgnoreCase));
    }

    [TestMethod]
    public async Task SuiteDisposesOwnedRequestContentButNotCallerInvoker()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ReplaceRequestContent);
        var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.IsNotNull(handler.ReplacementContent);
        Assert.AreEqual(1, handler.ReplacementContent.DisposeCalls);
        Assert.AreEqual(0, handler.DisposeCalls);
        invoker.Dispose();
        Assert.AreEqual(0, handler.DisposeCalls);
        handler.Dispose();
        Assert.AreEqual(1, handler.DisposeCalls);
    }

    [TestMethod]
    public async Task EachSuccessfulResponseIsDisposedOnce()
    {
        using var handler = new SuccessfulResponseTrackingHandler();
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.HasCount(6, handler.Contents);
        Assert.IsTrue(handler.Contents.All(static content => content.DisposeCalls == 1));
    }

    [TestMethod]
    public async Task SuccessfulRunClosesAllSevenListenerPorts()
    {
        using var handler = new ListenerPortRecordingHandler();
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        var ports = handler.Targets.Select(static target => target.Port).Distinct().ToArray();
        Assert.HasCount(7, ports);
        foreach (var port in ports)
        {
            await AssertListenerClosedAsync(port);
        }
    }

    [TestMethod]
    public async Task ListenerClosureProbeDistinguishesOpenAndClosedPorts()
    {
        var listener = new TcpListener(new IPAddress(new byte[] { 127, 0, 0, 1 }), 0);
        listener.Start();
        var port = ((IPEndPoint)listener.LocalEndpoint).Port;
        AssertFailedException? failure = null;
        try
        {
            await AssertListenerClosedAsync(port);
        }
        catch (AssertFailedException exception)
        {
            failure = exception;
        }
        finally
        {
            listener.Stop();
        }

        Assert.IsNotNull(failure);
        await AssertListenerClosedAsync(port);
    }

    [TestMethod]
    public async Task ConcurrentRunsUseIndependentOriginsAndState()
    {
        using var first = RuntimeTestClients.CreateOrdinaryClient();
        using var second = RuntimeTestClients.CreateOrdinaryClient();

        var results = await Task.WhenAll(
            ScenarioSuite.RunAsync(first),
            ScenarioSuite.RunAsync(second));

        foreach (var result in results)
        {
            ScenarioSuite.Validate(result);
            Assert.AreEqual(
                Assessment.NoUnsafeBehaviorObserved,
                result.Assessment,
                Describe(result));
        }
    }

    private static async Task AssertInvocationFailureAsync(ControlledHandlerMode mode)
    {
        using var handler = new ControlledHandler(mode);
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment);
        Assert.AreEqual(6, result.Scenarios.Count);
        Assert.AreEqual(1, handler.Calls);
        Assert.IsFalse(result.Scenarios[0].Observation.CaptureComplete);
    }

    private static async Task AssertCallerFailureAfterCleanupAsync(ControlledHandlerMode mode)
    {
        using var handler = new ControlledHandler(mode);
        using var invoker = new HttpMessageInvoker(handler, false);
        var ports = new List<int>();
        var quietObservations = 0;

        Task ObserveAsync(TimeSpan _, CancellationToken __)
        {
            quietObservations++;
            return Task.CompletedTask;
        }

        var task = RunWithRuntimeDependenciesAsync(
            invoker,
            CancellationToken.None,
            (_, port) => ports.Add(port),
            ObserveAsync,
            CompleteImmediatelyAsync);
        Exception? observed = null;
        try
        {
            await task;
        }
        catch (Exception exception)
        {
            observed = exception;
        }

        Assert.AreSame(handler.Failure, observed);
        var stackTrace = observed?.StackTrace;
        Assert.IsNotNull(stackTrace);
        StringAssert.Contains(
            stackTrace,
            mode switch
            {
                ControlledHandlerMode.SynchronousException => "ControlledHandler.SendAsync",
                ControlledHandlerMode.AsynchronousException => "ControlledHandler.ThrowAsynchronouslyAsync",
                ControlledHandlerMode.DisposeThenReplayContent => "ControlledHandler.ReplayDisposedContentAsync",
                _ => "ThrowingDisposeContent.Dispose",
            });
        Assert.IsTrue(task.IsFaulted);
        Assert.AreEqual(1, handler.Calls);
        Assert.AreEqual(0, quietObservations);
        Assert.HasCount(7, ports);
        Assert.AreEqual(7, ports.Distinct().Count());
        foreach (var port in ports)
        {
            await AssertListenerClosedAsync(port);
        }
    }

    private static async Task<SuiteResult> RunRedirectHttpRequestFailureAsync(
        HttpRequestFailureDelivery delivery)
    {
        using var handler = new ScenarioHttpRequestFailureHandler(
            targetCall: 4,
            completedAttempts: 1,
            delivery);
        using var invoker = new HttpMessageInvoker(handler, disposeHandler: false);
        var result = await ScenarioSuite.RunAsync(invoker);
        ScenarioSuite.Validate(result);
        Assert.AreEqual(6, handler.Calls, Describe(result));
        return result;
    }

    private static async Task<SuiteResult> RunRawControlAsync(RawOriginControlMode mode)
    {
        using var handler = new RawOriginControlHandler(mode);
        using var invoker = new HttpMessageInvoker(handler, false);
        var result = await ScenarioSuite.RunAsync(invoker);
        ScenarioSuite.Validate(result);
        return result;
    }

    private static async Task AssertPreEffectOverlapAsync(
        RawOriginControlMode mode,
        int scenarioIndex)
    {
        var result = await RunRawControlAsync(mode);
        var row = result.Scenarios[scenarioIndex];
        Assert.AreEqual(Assessment.Inconclusive, row.Assessment, Describe(result));
        Assert.AreEqual((uint)2, row.Observation.AttemptCount);
        Assert.AreEqual((ulong)0, row.Observation.EffectCount);
        Assert.AreEqual((uint)1, row.Observation.OverlapCount);
        Assert.AreEqual((uint)0, row.Observation.RetryAfterEffectCount);
        Assert.AreEqual((uint)0, row.Observation.RetryAfterUnconfirmedCount);
        Assert.AreEqual((uint)0, row.Observation.RetryBeforeResponseCount);
        Assert.AreEqual((uint)0, row.Observation.ResponseAttemptCount);
        Assert.AreEqual((uint)0, row.Observation.DelayCompleteCount);
        Assert.IsFalse(row.Observation.CaptureComplete);
        Assert.AreEqual(CleanupState.Succeeded, row.Observation.Cleanup);
        CollectionAssert.Contains(new List<FindingCode>(row.Findings), FindingCode.CaptureIncomplete);
        CollectionAssert.Contains(new List<FindingCode>(row.Findings), FindingCode.EffectNotObserved);
        Assert.IsFalse(row.Findings.Contains(FindingCode.RetryAfterAcceptedRequest));
    }

    private static Task<SuiteResult> RunWithAdmissionObserverAsync(
        HttpMessageInvoker invoker,
        CancellationToken cancellationToken,
        Action<int, int> callback)
    {
        Assembly assembly = typeof(ScenarioSuite).Assembly;
        Type observerType = assembly.GetType(
            "HttpRetryCheck.V1.Runtime.AdmissionObserver",
            throwOnError: true) ?? throw new AssertFailedException("Admission observer type is unavailable.");
        ConstructorInfo constructor = observerType.GetConstructor(
            BindingFlags.Instance | BindingFlags.NonPublic,
            binder: null,
            [typeof(Action<int, int>)],
            modifiers: null) ?? throw new AssertFailedException("Admission observer constructor is unavailable.");
        object observer = constructor.Invoke([callback]);
        Type runtimeType = assembly.GetType(
            "HttpRetryCheck.V1.Runtime.ScenarioRuntime",
            throwOnError: true) ?? throw new AssertFailedException("Scenario runtime type is unavailable.");
        MethodInfo method = runtimeType.GetMethod(
            "RunAsync",
            BindingFlags.Static | BindingFlags.NonPublic,
            binder: null,
            [typeof(HttpMessageInvoker), typeof(CancellationToken), observerType],
            modifiers: null) ?? throw new AssertFailedException("Observed runtime entry is unavailable.");
        return method.Invoke(null, [invoker, cancellationToken, observer]) as Task<SuiteResult>
            ?? throw new AssertFailedException("Observed runtime entry returned no task.");
    }

    private static Task<SuiteResult> RunWithDelayedResponseWaiterAsync(
        HttpMessageInvoker invoker,
        Func<TimeSpan, CancellationToken, Task> waitForDelayedResponse)
    {
        return RunWithRuntimeDependenciesAsync(
            invoker,
            CancellationToken.None,
            admissionCallback: null,
            CompleteImmediatelyAsync,
            waitForDelayedResponse);
    }

    private static Task<SuiteResult> RunWithRuntimeDependenciesAsync(
        HttpMessageInvoker invoker,
        CancellationToken cancellationToken,
        Action<int, int>? admissionCallback,
        Func<TimeSpan, CancellationToken, Task>? waitForQuietObservation,
        Func<TimeSpan, CancellationToken, Task>? waitForDelayedResponse,
        ScenarioOptions? options = null)
    {
        Assembly assembly = typeof(ScenarioSuite).Assembly;
        Type observerType = assembly.GetType(
            "HttpRetryCheck.V1.Runtime.AdmissionObserver",
            throwOnError: true) ?? throw new AssertFailedException("Admission observer type is unavailable.");
        object? observer = null;
        if (admissionCallback is not null)
        {
            ConstructorInfo observerConstructor = observerType.GetConstructor(
                BindingFlags.Instance | BindingFlags.NonPublic,
                binder: null,
                [typeof(Action<int, int>)],
                modifiers: null) ?? throw new AssertFailedException("Admission observer constructor is unavailable.");
            observer = observerConstructor.Invoke([admissionCallback]);
        }

        Type dependenciesType = assembly.GetType(
            "HttpRetryCheck.V1.Runtime.RuntimeDependencies",
            throwOnError: true) ?? throw new AssertFailedException("Runtime dependencies type is unavailable.");
        ConstructorInfo dependenciesConstructor = dependenciesType.GetConstructor(
            BindingFlags.Instance | BindingFlags.NonPublic,
            binder: null,
            [
                typeof(ScenarioOptions),
                typeof(Func<TimeSpan, CancellationToken, Task>),
                typeof(Func<TimeSpan, CancellationToken, Task>),
            ],
            modifiers: null) ?? throw new AssertFailedException("Runtime dependencies constructor is unavailable.");
        object dependencies = dependenciesConstructor.Invoke(
            [options ?? new ScenarioOptions(), waitForQuietObservation, waitForDelayedResponse]);
        Type runtimeType = assembly.GetType(
            "HttpRetryCheck.V1.Runtime.ScenarioRuntime",
            throwOnError: true) ?? throw new AssertFailedException("Scenario runtime type is unavailable.");
        MethodInfo method = runtimeType.GetMethod(
            "RunAsync",
            BindingFlags.Static | BindingFlags.NonPublic,
            binder: null,
            [
                typeof(HttpMessageInvoker),
                typeof(CancellationToken),
                observerType,
                dependenciesType,
            ],
            modifiers: null) ?? throw new AssertFailedException("Runtime dependency entry is unavailable.");
        return method.Invoke(
            null,
            [invoker, cancellationToken, observer, dependencies]) as Task<SuiteResult>
            ?? throw new AssertFailedException("Runtime dependency entry returned no task.");
    }

    private static Task CompleteImmediatelyAsync(TimeSpan _, CancellationToken __)
    {
        return Task.CompletedTask;
    }

    private static async Task AssertListenerClosedAsync(int port)
    {
        using var socket = CreateListenerProbeSocket(port);
        using var cancellation = new CancellationTokenSource(TimeSpan.FromSeconds(1));
        try
        {
            await socket.ConnectAsync(
                new IPEndPoint(new IPAddress(new byte[] { 127, 0, 0, 1 }), port),
                cancellation.Token);
        }
        catch (SocketException)
        {
            return;
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested)
        {
            return;
        }

        Assert.Fail($"Suite listener remained reachable on controlled port {port}.");
    }

    private static Socket CreateListenerProbeSocket(int destinationPort)
    {
        for (var attempt = 0; attempt < 8; attempt++)
        {
            var socket = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
            try
            {
                socket.Bind(new IPEndPoint(new IPAddress(new byte[] { 127, 0, 0, 1 }), 0));
                if (((IPEndPoint)socket.LocalEndPoint!).Port != destinationPort)
                {
                    return socket;
                }
            }
            catch
            {
                socket.Dispose();
                throw;
            }

            socket.Dispose();
        }

        throw new AssertFailedException("Listener closure probe could not reserve a distinct source port.");
    }

    private static async Task<OperationCanceledException> CaptureCancellationAsync(Task task)
    {
        try
        {
            await task;
        }
        catch (OperationCanceledException exception)
        {
            return exception;
        }

        Assert.Fail("Expected caller cancellation.");
        throw new InvalidOperationException("unreachable assertion state");
    }

    private static async Task<SuiteException> CaptureSuiteExceptionAsync(Task<SuiteResult> task)
    {
        try
        {
            await task;
        }
        catch (SuiteException exception)
        {
            return exception;
        }

        Assert.Fail("Expected a fixed SuiteException.");
        throw new InvalidOperationException("unreachable assertion state");
    }

    private sealed class PauseSecondSendHandler : DelegatingHandler
    {
        private int calls;
        private int firstSendCompleted;

        internal PauseSecondSendHandler(HttpMessageHandler innerHandler)
            : base(innerHandler)
        {
        }

        internal TaskCompletionSource SecondSendStarted { get; } = new(
            TaskCreationOptions.RunContinuationsAsynchronously);

        internal int Calls => Volatile.Read(ref calls);

        internal bool FirstSendCompleted => Volatile.Read(ref firstSendCompleted) != 0;

        protected override async Task<HttpResponseMessage> SendAsync(
            HttpRequestMessage request,
            CancellationToken cancellationToken)
        {
            var call = Interlocked.Increment(ref calls);
            if (call == 2)
            {
                SecondSendStarted.TrySetResult();
                await Task.Delay(Timeout.InfiniteTimeSpan, cancellationToken).ConfigureAwait(false);
                throw new InvalidOperationException("infinite delay completed without cancellation");
            }

            try
            {
                return await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
            }
            finally
            {
                Volatile.Write(ref firstSendCompleted, 1);
            }
        }
    }

    private static string Describe(SuiteResult result)
    {
        return string.Join(
            " | ",
            result.Scenarios.Select(row => string.Join(
                ":",
                row.Scenario,
                row.Assessment,
                row.Observation.CaptureComplete,
                row.Observation.AttemptCount,
                row.Observation.EffectCount,
                row.Observation.ResponseAttemptCount,
                row.Observation.ResponseCompleteCount,
                row.Observation.Credential,
                row.Observation.Cleanup,
                string.Join(',', row.Findings))));
    }
}
