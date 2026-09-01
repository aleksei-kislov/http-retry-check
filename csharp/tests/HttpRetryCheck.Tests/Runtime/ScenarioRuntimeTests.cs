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
            row => row.Assessment == Assessment.NoUnsafeBehaviorObserved && row.Findings.Count == 0));
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
    public async Task RedirectTaskFailureIsInconclusiveAndStopsLaterInvocations()
    {
        using var handler = new ThrowAfterScenarioResponseHandler(targetCall: 4);
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment, Describe(result));
        var redirect = result.Scenarios[3];
        Assert.AreEqual(Assessment.Inconclusive, redirect.Assessment, Describe(result));
        Assert.AreEqual((uint)1, redirect.Observation.AttemptCount);
        Assert.AreEqual((uint)1, redirect.Observation.ResponseCompleteCount);
        Assert.AreEqual(CredentialState.SourceOnly, redirect.Observation.Credential);
        Assert.IsFalse(redirect.Observation.CaptureComplete);
        CollectionAssert.Contains(
            new List<FindingCode>(redirect.Findings),
            FindingCode.CaptureIncomplete);
        Assert.AreEqual(4, handler.Calls);
        Assert.AreEqual((uint)0, result.Scenarios[4].Observation.AttemptCount);
        Assert.AreEqual((uint)0, result.Scenarios[5].Observation.AttemptCount);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
    }

    [TestMethod]
    public async Task RetryLimitTaskFailureIsInconclusiveAndStopsDelayedInvocation()
    {
        using var handler = new ThrowAfterScenarioResponseHandler(targetCall: 5, attempts: 2);
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment, Describe(result));
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.Inconclusive, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)2, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)2, retry.Observation.ResponseCompleteCount);
        Assert.IsFalse(retry.Observation.CaptureComplete);
        CollectionAssert.Contains(
            new List<FindingCode>(retry.Findings),
            FindingCode.CaptureIncomplete);
        Assert.AreEqual(5, handler.Calls);
        Assert.AreEqual((uint)0, result.Scenarios[5].Observation.AttemptCount);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
    }

    [TestMethod]
    public async Task RetryLimitTaskFailurePreservesUnsafeEvidenceAndStopsDelayedInvocation()
    {
        using var handler = new ThrowAfterScenarioResponseHandler(targetCall: 5, attempts: 3);
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);

        var result = await ScenarioSuite.RunAsync(client);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment, Describe(result));
        var retry = result.Scenarios[4];
        Assert.AreEqual(Assessment.UnsafeBehaviorObserved, retry.Assessment, Describe(result));
        Assert.AreEqual((uint)3, retry.Observation.AttemptCount);
        Assert.AreEqual((uint)3, retry.Observation.ResponseCompleteCount);
        Assert.IsFalse(retry.Observation.CaptureComplete);
        CollectionAssert.Contains(
            new List<FindingCode>(retry.Findings),
            FindingCode.CaptureIncomplete);
        CollectionAssert.Contains(
            new List<FindingCode>(retry.Findings),
            FindingCode.AttemptLimitExceeded);
        Assert.AreEqual(5, handler.Calls);
        Assert.AreEqual((uint)0, result.Scenarios[5].Observation.AttemptCount);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
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
        Assert.IsFalse(exactRetry.Observation.BodyConsistent);
        Assert.AreEqual((uint)1, exactRetry.Observation.ResponseAttemptCount);
        CollectionAssert.Contains(new List<FindingCode>(exactRetry.Findings), FindingCode.BodyChanged);
        var exceededRetry = exceeded.Scenarios[4];
        Assert.AreEqual((uint)1, exceededRetry.Observation.AttemptCount);
        Assert.IsFalse(exceededRetry.Observation.CaptureComplete);
        Assert.AreEqual((uint)0, exceededRetry.Observation.ResponseAttemptCount);
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
    public async Task CancellationAtEntryReturnsAnInvalidCallError()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();
        cancellation.Cancel();

        var task = ScenarioSuite.RunAsync(invoker, cancellation.Token);

        var exception = await CaptureSuiteExceptionAsync(task);
        Assert.AreEqual(SuiteFailureCode.InvalidCall, exception.Code);
        Assert.IsFalse(task.IsCanceled);
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

        var exception = await CaptureSuiteExceptionAsync(task);
        Assert.AreEqual(SuiteFailureCode.SuiteUnavailable, exception.Code);
        Assert.AreEqual(0, handler.Calls);
        Assert.IsFalse(task.IsCanceled);
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
        var exception = await CaptureSuiteExceptionAsync(task);

        Assert.AreEqual(SuiteFailureCode.SuiteUnavailable, exception.Code);
        CollectionAssert.AreEqual(new[] { 1 }, ordinals);
        Assert.HasCount(1, ports);
        Assert.AreEqual(0, handler.Calls);
        Assert.IsFalse(task.IsCanceled);
        await AssertListenerClosedAsync(ports[0]);
    }

    [TestMethod]
    public async Task CancellationBeforeFirstSendReturnsSuiteUnavailable()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();
        using var observer = new AcceptStartCancellationObserver(cancellation);

        var task = ScenarioSuite.RunAsync(invoker, cancellation.Token);
        await observer.Observed.WaitAsync(TimeSpan.FromSeconds(2));
        var exception = await CaptureSuiteExceptionAsync(task);

        Assert.AreEqual(SuiteFailureCode.SuiteUnavailable, exception.Code);
        Assert.AreEqual(0, handler.Calls);
        Assert.IsFalse(task.IsCanceled);
    }

    [TestMethod]
    public async Task CancellationAfterAdmissionMaterializesSixRows()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.WaitForCancellation);
        using var invoker = new HttpMessageInvoker(handler, false);
        using var cancellation = new CancellationTokenSource();

        var task = ScenarioSuite.RunAsync(invoker, cancellation.Token);
        await handler.Entered.Task.WaitAsync(TimeSpan.FromSeconds(2));
        cancellation.Cancel();
        var result = await task;

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment);
        Assert.AreEqual(6, result.Scenarios.Count);
        Assert.AreEqual(1, handler.Calls);
        Assert.IsFalse(task.IsCanceled);
    }

    [TestMethod]
    public async Task SynchronousSenderExceptionReturnsAnInconclusiveResult()
    {
        await AssertInvocationFailureAsync(ControlledHandlerMode.SynchronousException);
    }

    [TestMethod]
    public async Task AsynchronousSenderExceptionReturnsAnInconclusiveResult()
    {
        await AssertInvocationFailureAsync(ControlledHandlerMode.AsynchronousException);
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
    public async Task ResponseDisposalFaultMakesCaptureIncompleteAndCleanupFailed()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.DisposalFailure);
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment);
        Assert.IsFalse(result.Scenarios[0].Observation.CaptureComplete);
        Assert.AreEqual(CleanupState.Failed, result.Scenarios[0].Observation.Cleanup);
        CollectionAssert.Contains(
            new List<FindingCode>(result.Scenarios[0].Findings),
            FindingCode.CleanupUnverified);
    }

    [TestMethod]
    public async Task RequestContentDisposalFaultMakesCleanupFailedWithoutLeakingCause()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ReplaceWithThrowingContent);
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(CleanupState.Failed, result.Scenarios[0].Observation.Cleanup);
        Assert.IsFalse(Describe(result).Contains("marker", StringComparison.Ordinal));
    }

    [TestMethod]
    public async Task DisposedReplayContentDoesNotLeakItsRawException()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.DisposeThenReplayContent);
        using var invoker = new HttpMessageInvoker(handler, false);

        var result = await ScenarioSuite.RunAsync(invoker);

        ScenarioSuite.Validate(result);
        Assert.AreEqual(Assessment.Inconclusive, result.Assessment);
        Assert.IsFalse(result.Scenarios[0].Observation.CaptureComplete);
        Assert.IsFalse(Describe(result).Contains("ObjectDisposed", StringComparison.Ordinal));
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
