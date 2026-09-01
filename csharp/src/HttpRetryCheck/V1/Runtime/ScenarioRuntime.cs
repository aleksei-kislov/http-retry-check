using System;
using System.Collections.Generic;
using System.Net;
using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.V1.Runtime;

internal enum InvocationStatus
{
    Response = 1,
    TaskFailure = 2,
    SynchronousFailure = 3,
    InvalidCompletion = 4,
}

internal readonly record struct InvocationResult(
    InvocationStatus Status,
    bool DisposalFailed);

internal readonly record struct ScenarioExecution(
    Observation Observation,
    bool Settled,
    bool InvocationStarted,
    bool CancellationBeforeInvocation,
    bool CleanupVerified);

internal static class ScenarioRuntime
{
    internal static async Task<SuiteResult> RunAsync(
        HttpMessageInvoker? client,
        CancellationToken cancellationToken,
        AdmissionObserver? admissionObserver = null)
    {
        if (client is null || cancellationToken.IsCancellationRequested)
        {
            throw new SuiteException(SuiteFailureCode.InvalidCall);
        }

        await Task.Yield();

        var admission = SuiteAdmission.Open(cancellationToken, admissionObserver);
        if (admission.Suite is null)
        {
            throw new SuiteException(
                admission.Failure == 0 ? SuiteFailureCode.InternalFailure : admission.Failure);
        }

        var suite = admission.Suite;
        if (cancellationToken.IsCancellationRequested)
        {
            var closed = suite.CloseFrom(0);
            throw new SuiteException(
                closed ? SuiteFailureCode.SuiteUnavailable : SuiteFailureCode.InternalFailure);
        }

        var rows = new List<ScenarioResult>(SemanticEvaluator.ScenarioCount);
        var executionAdmitted = false;
        var stop = false;
        for (var index = 0; index < suite.Scenarios.Count; index++)
        {
            var admitted = suite.Scenarios[index];
            if (stop)
            {
                rows.Add(CreateUnavailableRow(admitted.Scenario, admitted.Close()));
                continue;
            }

            ScenarioExecution execution;
            try
            {
                execution = await ExecuteAsync(
                    cancellationToken,
                    client,
                    admitted,
                    () => executionAdmitted = true).ConfigureAwait(false);
            }
            catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
            {
                var currentClosed = admitted.Close();
                if (!executionAdmitted)
                {
                    _ = currentClosed;
                    _ = suite.CloseFrom(index + 1);
                    throw new SuiteException(SuiteFailureCode.InternalFailure);
                }

                rows.Add(CreateUnavailableRow(admitted.Scenario, currentClosed));
                stop = true;
                continue;
            }

            if (execution.InvocationStarted)
            {
                executionAdmitted = true;
            }
            else if (!executionAdmitted)
            {
                var remainingClosed = suite.CloseFrom(index + 1);
                var cleanupVerified = execution.CleanupVerified && remainingClosed;
                throw new SuiteException(
                    execution.CancellationBeforeInvocation && cleanupVerified
                        ? SuiteFailureCode.SuiteUnavailable
                        : SuiteFailureCode.InternalFailure);
            }

            try
            {
                rows.Add(SemanticEvaluator.CreateScenarioResult(admitted.Scenario, execution.Observation));
            }
            catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
            {
                rows.Add(CreateUnavailableRow(
                    admitted.Scenario,
                    execution.Observation.Cleanup == CleanupState.Succeeded));
                stop = true;
            }

            if (!execution.Settled)
            {
                stop = true;
            }
        }

        return SemanticEvaluator.CreateSuiteResult(rows.AsReadOnly());
    }

    private static async Task<ScenarioExecution> ExecuteAsync(
        CancellationToken parentToken,
        HttpMessageInvoker client,
        AdmittedScenario admitted,
        Action onInvocationStarted)
    {
        using var scenarioCancellation = CancellationTokenSource.CreateLinkedTokenSource(parentToken);
        scenarioCancellation.CancelAfter(RuntimeConstants.ScenarioTimeout);
        var origin = new ScenarioOrigin(admitted, scenarioCancellation.Token, scenarioCancellation.Cancel);
        HttpRequestMessage? request = null;
        TrackedContent? content = null;
        var invocation = new InvocationResult(InvocationStatus.InvalidCompletion, false);
        var invocationStarted = false;
        var cancellationBeforeInvocation = false;
        var requestDisposalFailed = false;
        var contentQuiesced = true;
        var originClosed = false;
        var setupFailed = false;

        try
        {
            origin.Start();
            content = new TrackedContent(admitted.Scenario == ScenarioId.ChangedBodyRetry);
            request = CreateRequest(origin.SourceTarget, content);
            if (scenarioCancellation.IsCancellationRequested)
            {
                cancellationBeforeInvocation = true;
            }
            else
            {
                invocationStarted = true;
                onInvocationStarted();
                invocation = await InvokeAsync(
                    client,
                    request,
                    scenarioCancellation.Token).ConfigureAwait(false);
            }
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            setupFailed = !invocationStarted;
        }
        finally
        {
            content?.Seal();
            if (request is not null)
            {
                var contentOwnedByRequest = content is not null && ReferenceEquals(request.Content, content);
                try
                {
                    request.Dispose();
                }
                catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                {
                    requestDisposalFailed = true;
                }

                if (!contentOwnedByRequest)
                {
                    try
                    {
                        content?.Dispose();
                    }
                    catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                    {
                        requestDisposalFailed = true;
                    }
                }
            }
            else
            {
                try
                {
                    content?.Dispose();
                }
                catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                {
                    requestDisposalFailed = true;
                }
            }

            using var cleanupCancellation = new CancellationTokenSource(RuntimeConstants.CleanupTimeout);
            if (content is not null)
            {
                try
                {
                    await content.WaitForQuiescenceAsync(cleanupCancellation.Token).ConfigureAwait(false);
                }
                catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                {
                    contentQuiesced = false;
                }
            }

            try
            {
                originClosed = await origin.CloseAsync(cleanupCancellation.Token).ConfigureAwait(false);
            }
            catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
            {
                originClosed = false;
            }
        }

        var observation = origin.Snapshot();
        var cleanupVerified = originClosed && contentQuiesced && !requestDisposalFailed &&
            !invocation.DisposalFailed;
        var invocationAccepted = InvocationAccepted(admitted.Scenario, observation, invocation.Status);
        var captureComplete = observation.CaptureComplete && invocationAccepted && cleanupVerified &&
            !scenarioCancellation.IsCancellationRequested && !setupFailed;
        observation = CopyObservation(
            observation,
            captureComplete,
            cleanupVerified ? observation.Cleanup : CleanupState.Failed);
        var settled = invocationStarted && invocationAccepted && cleanupVerified &&
            !scenarioCancellation.IsCancellationRequested && !setupFailed;
        return new ScenarioExecution(
            observation,
            settled,
            invocationStarted,
            cancellationBeforeInvocation,
            cleanupVerified);
    }

    private static HttpRequestMessage CreateRequest(string target, TrackedContent content)
    {
        var request = new HttpRequestMessage(HttpMethod.Post, new Uri(target, UriKind.Absolute))
        {
            Version = HttpVersion.Version11,
            VersionPolicy = HttpVersionPolicy.RequestVersionExact,
            Content = content,
        };
        if (!request.Headers.TryAddWithoutValidation(
                "Authorization",
                RuntimeConstants.SyntheticCredential))
        {
            request.Dispose();
            throw new InvalidOperationException("HTTP scenario request could not be constructed");
        }

        return request;
    }

    private static async Task<InvocationResult> InvokeAsync(
        HttpMessageInvoker client,
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        Task<HttpResponseMessage>? task;
        try
        {
            task = client.SendAsync(request, cancellationToken);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return new InvocationResult(InvocationStatus.SynchronousFailure, false);
        }

        if (task is null)
        {
            return new InvocationResult(InvocationStatus.InvalidCompletion, false);
        }

        HttpResponseMessage? response;
        try
        {
            response = await task.ConfigureAwait(false);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return new InvocationResult(InvocationStatus.TaskFailure, false);
        }

        if (response is null)
        {
            return new InvocationResult(InvocationStatus.InvalidCompletion, false);
        }

        var valid = true;
        try
        {
            valid = response.Content is not null;
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            valid = false;
        }

        var disposalFailed = false;
        try
        {
            response.Dispose();
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            disposalFailed = true;
        }

        return new InvocationResult(
            valid && !disposalFailed
                ? InvocationStatus.Response
                : InvocationStatus.InvalidCompletion,
            disposalFailed);
    }

    private static bool InvocationAccepted(
        ScenarioId scenario,
        Observation observation,
        InvocationStatus status)
    {
        if (status == InvocationStatus.Response)
        {
            return true;
        }

        if (status != InvocationStatus.TaskFailure)
        {
            return false;
        }

        if (scenario is ScenarioId.AcceptThenDisconnect or
            ScenarioId.DisconnectBeforeAcceptance or
            ScenarioId.ChangedBodyRetry)
        {
            return observation.AttemptCount != 0;
        }

        return scenario == ScenarioId.DelayedResponse &&
            IsExactDelayedTaskFailureObservation(observation);
    }

    private static bool IsExactDelayedTaskFailureObservation(Observation observation)
    {
        return observation.CaptureComplete &&
            observation.AttemptCount == 1 &&
            observation.EffectCount == 1 &&
            observation.OverlapCount == 0 &&
            observation.RetryAfterEffectCount == 0 &&
            observation.RetryAfterUnconfirmedCount == 0 &&
            observation.RetryBeforeResponseCount == 0 &&
            observation.ResponseAttemptCount == 1 &&
            observation.ResponseCompleteCount <= 1 &&
            observation.FirstResponseComplete == (observation.ResponseCompleteCount == 1) &&
            observation.DelayCompleteCount == 1 &&
            observation.MethodConsistent &&
            observation.DestinationConsistent &&
            observation.BodyConsistent &&
            observation.Credential == CredentialState.SourceOnly &&
            observation.Cleanup == CleanupState.Succeeded;
    }

    private static ScenarioResult CreateUnavailableRow(ScenarioId scenario, bool cleanupSucceeded)
    {
        return SemanticEvaluator.CreateScenarioResult(
            scenario,
            new Observation(
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
                cleanupSucceeded ? CleanupState.Succeeded : CleanupState.Failed));
    }

    private static Observation CopyObservation(
        Observation source,
        bool captureComplete,
        CleanupState cleanup)
    {
        return new Observation(
            captureComplete,
            source.AttemptCount,
            source.EffectCount,
            source.OverlapCount,
            source.RetryAfterEffectCount,
            source.RetryAfterUnconfirmedCount,
            source.RetryBeforeResponseCount,
            source.ResponseAttemptCount,
            source.ResponseCompleteCount,
            source.FirstResponseComplete,
            source.DelayCompleteCount,
            source.MethodConsistent,
            source.DestinationConsistent,
            source.BodyConsistent,
            source.Credential,
            cleanup);
    }
}
