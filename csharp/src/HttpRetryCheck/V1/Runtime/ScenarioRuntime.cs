using System;
using System.Collections.Generic;
using System.Net;
using System.Net.Http;
using System.Runtime.ExceptionServices;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.V1.Runtime;

internal enum InvocationStatus
{
    Response = 1,
    HttpRequestFailure = 2,
    TaskFailure = 3,
    SynchronousFailure = 4,
    InvalidCompletion = 5,
}

internal readonly record struct InvocationResult(
    InvocationStatus Status,
    bool DisposalFailed,
    ExceptionDispatchInfo? CallerFailure);

internal readonly record struct ScenarioExecution(
    Observation Observation,
    bool Settled,
    bool InvocationStarted,
    bool CancellationBeforeInvocation,
    bool CleanupVerified,
    ExceptionDispatchInfo? CallerFailure);

internal sealed class RuntimeDependencies
{
    private readonly Func<TimeSpan, CancellationToken, Task> waitForQuietObservation;

    internal RuntimeDependencies(
        ScenarioOptions? options = null,
        Func<TimeSpan, CancellationToken, Task>? waitForQuietObservation = null,
        Func<TimeSpan, CancellationToken, Task>? waitForDelayedResponse = null)
    {
        var selected = options ?? new ScenarioOptions();
        if (!selected.IsValid())
        {
            throw new ArgumentOutOfRangeException(nameof(options));
        }

        ScenarioTimeout = selected.ScenarioTimeout;
        ConnectionTimeout = selected.ConnectionTimeout;
        QuietWindow = selected.QuietWindow;
        AttemptLimit = selected.AttemptLimit;
        this.waitForQuietObservation = waitForQuietObservation ?? DelayAsync;
        WaitForDelayedResponse = waitForDelayedResponse ?? DelayAsync;
    }

    internal TimeSpan ScenarioTimeout { get; }

    internal TimeSpan ConnectionTimeout { get; }

    internal TimeSpan QuietWindow { get; }

    internal uint AttemptLimit { get; }

    internal int MaximumObservedAttempts => checked((int)AttemptLimit + 1);

    internal Func<TimeSpan, CancellationToken, Task> WaitForDelayedResponse { get; }

    internal Task WaitForQuietObservationAsync(CancellationToken cancellationToken)
    {
        return waitForQuietObservation(QuietWindow, cancellationToken);
    }

    private static Task DelayAsync(TimeSpan duration, CancellationToken cancellationToken)
    {
        return Task.Delay(duration, cancellationToken);
    }
}

internal static class ScenarioRuntime
{
    internal static async Task<SuiteResult> RunAsync(
        HttpMessageInvoker? client,
        CancellationToken cancellationToken,
        AdmissionObserver? admissionObserver = null)
    {
        return await RunAsync(
            client,
            cancellationToken,
            admissionObserver,
            new RuntimeDependencies()).ConfigureAwait(false);
    }

    internal static async Task<SuiteResult> RunAsync(
        HttpMessageInvoker? client,
        ScenarioOptions? options,
        CancellationToken cancellationToken)
    {
        if (options is null || !options.IsValid())
        {
            throw new SuiteException(SuiteFailureCode.InvalidCall);
        }

        return await RunAsync(
            client,
            cancellationToken,
            admissionObserver: null,
            dependencies: new RuntimeDependencies(options)).ConfigureAwait(false);
    }

    internal static async Task<SuiteResult> RunAsync(
        HttpMessageInvoker? client,
        CancellationToken cancellationToken,
        AdmissionObserver? admissionObserver,
        Func<TimeSpan, CancellationToken, Task>? waitForDelayedResponse)
    {
        return await RunAsync(
            client,
            cancellationToken,
            admissionObserver,
            new RuntimeDependencies(waitForDelayedResponse: waitForDelayedResponse)).ConfigureAwait(false);
    }

    internal static async Task<SuiteResult> RunAsync(
        HttpMessageInvoker? client,
        CancellationToken cancellationToken,
        AdmissionObserver? admissionObserver,
        RuntimeDependencies dependencies)
    {
        if (client is null)
        {
            throw new SuiteException(SuiteFailureCode.InvalidCall);
        }

        ArgumentNullException.ThrowIfNull(dependencies);

        cancellationToken.ThrowIfCancellationRequested();

        await Task.Yield();

        var admission = SuiteAdmission.Open(
            cancellationToken,
            admissionObserver,
            dependencies.MaximumObservedAttempts);
        if (admission.Suite is null)
        {
            cancellationToken.ThrowIfCancellationRequested();
            throw new SuiteException(
                admission.Failure == 0 ? SuiteFailureCode.InternalFailure : admission.Failure);
        }

        var suite = admission.Suite;
        ThrowCallerCancellationAfterClosing(cancellationToken, suite, 0);

        var rows = new List<ScenarioResult>(SemanticEvaluator.ScenarioCount);
        var executionAdmitted = false;
        var stop = false;
        for (var index = 0; index < suite.Scenarios.Count; index++)
        {
            ThrowCallerCancellationAfterClosing(cancellationToken, suite, index);
            var admitted = suite.Scenarios[index];
            if (stop)
            {
                rows.Add(CreateUnavailableRow(
                    admitted.Scenario,
                    dependencies.AttemptLimit,
                    admitted.Close()));
                continue;
            }

            ScenarioExecution execution;
            try
            {
                execution = await ExecuteAsync(
                    cancellationToken,
                    client,
                    admitted,
                    () => executionAdmitted = true,
                    dependencies).ConfigureAwait(false);
            }
            catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
            {
                var currentClosed = admitted.Close();
                ThrowCallerCancellationAfterClosing(cancellationToken, suite, index + 1);
                if (!executionAdmitted)
                {
                    _ = currentClosed;
                    _ = suite.CloseFrom(index + 1);
                    throw new SuiteException(SuiteFailureCode.InternalFailure);
                }

                rows.Add(CreateUnavailableRow(
                    admitted.Scenario,
                    dependencies.AttemptLimit,
                    currentClosed));
                stop = true;
                continue;
            }

            ThrowCallerCancellationAfterClosing(cancellationToken, suite, index + 1);
            if (execution.CallerFailure is not null)
            {
                _ = suite.CloseFrom(index + 1);
                cancellationToken.ThrowIfCancellationRequested();
                execution.CallerFailure.Throw();
                throw new SuiteException(SuiteFailureCode.InternalFailure);
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
                    dependencies.AttemptLimit,
                    execution.Observation.Cleanup == CleanupState.Succeeded));
                stop = true;
            }

            if (!execution.Settled)
            {
                stop = true;
            }
        }

        var result = SemanticEvaluator.CreateSuiteResult(rows.AsReadOnly());
        cancellationToken.ThrowIfCancellationRequested();
        return result;
    }

    private static void ThrowCallerCancellationAfterClosing(
        CancellationToken cancellationToken,
        AdmittedSuite suite,
        int firstScenario)
    {
        if (!cancellationToken.IsCancellationRequested)
        {
            return;
        }

        _ = suite.CloseFrom(firstScenario);
        cancellationToken.ThrowIfCancellationRequested();
    }

    private static async Task<ScenarioExecution> ExecuteAsync(
        CancellationToken parentToken,
        HttpMessageInvoker client,
        AdmittedScenario admitted,
        Action onInvocationStarted,
        RuntimeDependencies dependencies)
    {
        using var scenarioCancellation = CancellationTokenSource.CreateLinkedTokenSource(parentToken);
        scenarioCancellation.CancelAfter(dependencies.ScenarioTimeout);
        var origin = new ScenarioOrigin(
            admitted,
            scenarioCancellation.Token,
            scenarioCancellation.Cancel,
            dependencies.ConnectionTimeout,
            dependencies.AttemptLimit,
            dependencies.MaximumObservedAttempts,
            dependencies.WaitForDelayedResponse);
        HttpRequestMessage? request = null;
        TrackedContent? content = null;
        var invocation = new InvocationResult(InvocationStatus.InvalidCompletion, false, null);
        var invocationStarted = false;
        var cancellationBeforeInvocation = false;
        var requestDisposalFailed = false;
        var contentQuiesced = true;
        var quietObservationCompleted = true;
        var originClosed = false;
        var setupFailed = false;
        ExceptionDispatchInfo? callerFailure = null;

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
            callerFailure = invocation.CallerFailure;
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
                    if (!contentOwnedByRequest)
                    {
                        callerFailure ??= ExceptionDispatchInfo.Capture(exception);
                    }
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

            using var quiescenceCancellation = new CancellationTokenSource(RuntimeConstants.CleanupTimeout);
            if (content is not null)
            {
                try
                {
                    await content.WaitForQuiescenceAsync(quiescenceCancellation.Token).ConfigureAwait(false);
                }
                catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                {
                    contentQuiesced = false;
                }
            }

            if (invocationStarted && contentQuiesced && callerFailure is null)
            {
                try
                {
                    await dependencies.WaitForQuietObservationAsync(
                        scenarioCancellation.Token).ConfigureAwait(false);
                }
                catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                {
                    quietObservationCompleted = false;
                }
            }

            using var cleanupCancellation = new CancellationTokenSource(RuntimeConstants.CleanupTimeout);
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
            quietObservationCompleted &&
            !scenarioCancellation.IsCancellationRequested && !setupFailed;
        observation = CopyObservation(
            observation,
            captureComplete,
            cleanupVerified ? observation.Cleanup : CleanupState.Failed);
        var settled = invocationStarted && invocationAccepted && cleanupVerified &&
            quietObservationCompleted &&
            !scenarioCancellation.IsCancellationRequested && !setupFailed;
        return new ScenarioExecution(
            observation,
            settled,
            invocationStarted,
            cancellationBeforeInvocation,
            cleanupVerified,
            callerFailure);
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
        catch (HttpRequestException)
        {
            return new InvocationResult(InvocationStatus.HttpRequestFailure, false, null);
        }
        catch (OperationCanceledException)
        {
            return new InvocationResult(InvocationStatus.SynchronousFailure, false, null);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return new InvocationResult(
                InvocationStatus.SynchronousFailure,
                false,
                ExceptionDispatchInfo.Capture(exception));
        }

        if (task is null)
        {
            return new InvocationResult(InvocationStatus.InvalidCompletion, false, null);
        }

        HttpResponseMessage? response;
        try
        {
            response = await task.ConfigureAwait(false);
        }
        catch (HttpRequestException)
        {
            return new InvocationResult(InvocationStatus.HttpRequestFailure, false, null);
        }
        catch (OperationCanceledException)
        {
            return new InvocationResult(InvocationStatus.TaskFailure, false, null);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return new InvocationResult(
                InvocationStatus.TaskFailure,
                false,
                ExceptionDispatchInfo.Capture(exception));
        }

        if (response is null)
        {
            return new InvocationResult(InvocationStatus.InvalidCompletion, false, null);
        }

        var valid = true;
        ExceptionDispatchInfo? callerFailure = null;
        try
        {
            valid = response.Content is not null;
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            valid = false;
            callerFailure = ExceptionDispatchInfo.Capture(exception);
        }

        var disposalFailed = false;
        try
        {
            response.Dispose();
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            disposalFailed = true;
            callerFailure ??= ExceptionDispatchInfo.Capture(exception);
        }

        return new InvocationResult(
            valid && !disposalFailed
                ? InvocationStatus.Response
                : InvocationStatus.InvalidCompletion,
            disposalFailed,
            callerFailure);
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

        if (status is not InvocationStatus.HttpRequestFailure and not InvocationStatus.TaskFailure)
        {
            return false;
        }

        if (scenario is ScenarioId.AcceptThenDisconnect or
            ScenarioId.DisconnectBeforeAcceptance or
            ScenarioId.ChangedBodyRetry)
        {
            return observation.AttemptCount != 0;
        }

        if (scenario == ScenarioId.DelayedResponse)
        {
            return IsExactDelayedTaskFailureObservation(observation);
        }

        return status == InvocationStatus.HttpRequestFailure &&
            scenario is ScenarioId.CrossOriginRedirectCredentials or ScenarioId.RetryLimit &&
            IsCompleteResponseErrorObservation(observation);
    }

    private static bool IsCompleteResponseErrorObservation(Observation observation)
    {
        return observation.CaptureComplete &&
            observation.AttemptCount >= 1 &&
            observation.ResponseAttemptCount == observation.AttemptCount &&
            observation.ResponseCompleteCount == observation.AttemptCount &&
            observation.FirstResponseComplete;
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

    private static ScenarioResult CreateUnavailableRow(
        ScenarioId scenario,
        uint attemptLimit,
        bool cleanupSucceeded)
    {
        return SemanticEvaluator.CreateScenarioResult(
            scenario,
            new Observation(
                false,
                0,
                attemptLimit,
                string.Empty,
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
            source.AttemptLimit,
            source.Protocol,
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
