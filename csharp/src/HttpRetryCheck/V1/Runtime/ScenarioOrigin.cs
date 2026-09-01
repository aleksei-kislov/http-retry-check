using System;
using System.Collections.Generic;
using System.Globalization;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.V1.Runtime;

internal sealed class ScenarioOrigin
{
    private readonly object sync = new();
    private readonly ScenarioId scenario;
    private readonly IReadOnlyList<AdmittedEndpoint> endpoints;
    private readonly CancellationToken scenarioToken;
    private readonly Action stopScenario;
    private readonly Dictionary<Socket, bool> connections = new();
    private readonly List<CapturedAttempt> attempts = new(RuntimeConstants.MaximumAttempts);
    private readonly List<Task> acceptTasks = new(2);
    private readonly List<Task> handlerTasks = new(RuntimeConstants.MaximumAttempts);
    private bool started;
    private bool closing;
    private bool closeFailed;
    private bool serveFailed;
    private bool captureMissing;
    private uint active;
    private uint overlapCount;
    private ulong effectCount;
    private uint retryAfterEffectCount;
    private uint retryAfterUnconfirmedCount;
    private uint retryBeforeResponseCount;
    private uint pendingResponseCount;
    private uint responseAttemptCount;
    private uint responseCompleteCount;
    private bool firstResponseComplete;
    private uint delayCompleteCount;

    internal ScenarioOrigin(
        AdmittedScenario admitted,
        CancellationToken scenarioToken,
        Action stopScenario)
    {
        scenario = admitted.Scenario;
        endpoints = admitted.Endpoints;
        this.scenarioToken = scenarioToken;
        this.stopScenario = stopScenario;
    }

    internal string SourceTarget => Target(endpoints[0]);

    internal void Start()
    {
        lock (sync)
        {
            if (started)
            {
                throw new InvalidOperationException("HTTP scenario origin already started");
            }

            started = true;
            foreach (var endpoint in endpoints)
            {
                acceptTasks.Add(ServeAsync(endpoint));
            }
        }
    }

    internal async Task<bool> CloseAsync(CancellationToken cleanupToken)
    {
        CloseAdmissions();
        Socket[] readingConnections;
        Task[] accepts;
        lock (sync)
        {
            var reading = new List<Socket>(connections.Count);
            foreach (var item in connections)
            {
                if (item.Value)
                {
                    reading.Add(item.Key);
                }
            }

            readingConnections = reading.ToArray();
            accepts = acceptTasks.ToArray();
        }

        foreach (var connection in readingConnections)
        {
            CloseSocket(connection);
        }

        var settled = await WaitAsync(accepts, cleanupToken).ConfigureAwait(false);
        Task[] handlers;
        lock (sync)
        {
            handlers = handlerTasks.ToArray();
        }

        settled = await WaitAsync(handlers, cleanupToken).ConfigureAwait(false) && settled;
        if (!settled)
        {
            RequestStop();
            Socket[] remaining;
            lock (sync)
            {
                captureMissing = true;
                closeFailed = true;
                remaining = new Socket[connections.Count];
                connections.Keys.CopyTo(remaining, 0);
            }

            foreach (var connection in remaining)
            {
                CloseSocket(connection);
            }

            await AwaitAfterForcedCloseAsync(accepts).ConfigureAwait(false);
            lock (sync)
            {
                handlers = handlerTasks.ToArray();
            }

            await AwaitAfterForcedCloseAsync(handlers).ConfigureAwait(false);
        }

        lock (sync)
        {
            return settled && !closeFailed && active == 0 && connections.Count == 0;
        }
    }

    internal Observation Snapshot()
    {
        lock (sync)
        {
            var methodConsistent = true;
            var destinationConsistent = true;
            var bodyConsistent = true;
            var sourceSeen = false;
            var sourceMissing = false;
            var targetSeen = false;
            var targetExposed = false;

            for (var index = 0; index < attempts.Count; index++)
            {
                var attempt = attempts[index];
                if (attempt.HeadersObserved)
                {
                    methodConsistent = methodConsistent && attempt.MethodConsistent;
                    var expectedRole = EndpointRole.Source;
                    var sequenceAllowed = true;
                    if (scenario == ScenarioId.CrossOriginRedirectCredentials && index == 1)
                    {
                        expectedRole = EndpointRole.RedirectTarget;
                    }
                    else if (scenario == ScenarioId.CrossOriginRedirectCredentials && index > 1)
                    {
                        sequenceAllowed = false;
                    }

                    destinationConsistent = destinationConsistent &&
                        attempt.DestinationConsistent &&
                        attempt.Role == expectedRole &&
                        sequenceAllowed;
                    if (attempt.Role == EndpointRole.RedirectTarget && attempt.CredentialExposed)
                    {
                        targetExposed = true;
                    }
                }

                if (!attempt.Complete)
                {
                    continue;
                }

                bodyConsistent = bodyConsistent && attempt.BodyConsistent;
                if (attempt.Role == EndpointRole.Source)
                {
                    sourceSeen = true;
                    if (!attempt.CredentialExact)
                    {
                        sourceMissing = true;
                    }
                }
                else
                {
                    targetSeen = true;
                }
            }

            var credential = targetExposed
                ? CredentialState.ExposedAtTarget
                : !sourceSeen
                    ? CredentialState.NotObserved
                    : sourceMissing
                        ? CredentialState.Missing
                        : scenario != ScenarioId.CrossOriginRedirectCredentials || !targetSeen
                            ? CredentialState.SourceOnly
                            : CredentialState.AbsentAtTarget;
            var cleanup = closeFailed || active != 0 || connections.Count != 0
                ? CleanupState.Failed
                : CleanupState.Succeeded;
            return new Observation(
                !captureMissing && !serveFailed,
                (uint)attempts.Count,
                effectCount,
                overlapCount,
                retryAfterEffectCount,
                retryAfterUnconfirmedCount,
                retryBeforeResponseCount,
                responseAttemptCount,
                responseCompleteCount,
                firstResponseComplete,
                delayCompleteCount,
                methodConsistent,
                destinationConsistent,
                bodyConsistent,
                credential,
                cleanup);
        }
    }

    private async Task ServeAsync(AdmittedEndpoint endpoint)
    {
        try
        {
            while (true)
            {
                Socket connection;
                try
                {
                    connection = await endpoint.Listener.AcceptSocketAsync(scenarioToken).ConfigureAwait(false);
                }
                catch (OperationCanceledException) when (scenarioToken.IsCancellationRequested)
                {
                    return;
                }
                catch (ObjectDisposedException)
                {
                    lock (sync)
                    {
                        if (!closing && !scenarioToken.IsCancellationRequested)
                        {
                            serveFailed = true;
                            captureMissing = true;
                        }
                    }

                    return;
                }
                catch (SocketException)
                {
                    lock (sync)
                    {
                        if (!closing && !scenarioToken.IsCancellationRequested)
                        {
                            serveFailed = true;
                            captureMissing = true;
                        }
                    }

                    return;
                }

                var index = Admit(connection, endpoint.Role);
                if (index < 0)
                {
                    CloseSocket(connection);
                    RequestStop();
                    CloseAdmissions();
                    return;
                }

                var handler = ObserveAsync(connection, endpoint, index);
                lock (sync)
                {
                    handlerTasks.Add(handler);
                }
            }
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            lock (sync)
            {
                if (!closing && !scenarioToken.IsCancellationRequested)
                {
                    serveFailed = true;
                    captureMissing = true;
                }
            }
        }
    }

    private int Admit(Socket connection, EndpointRole role)
    {
        lock (sync)
        {
            if (closing || attempts.Count >= RuntimeConstants.MaximumAttempts)
            {
                if (attempts.Count >= RuntimeConstants.MaximumAttempts)
                {
                    captureMissing = true;
                }

                return -1;
            }

            if (active != 0)
            {
                overlapCount++;
                captureMissing = true;
            }

            if (attempts.Count != 0)
            {
                if ((scenario is ScenarioId.AcceptThenDisconnect or
                        ScenarioId.ChangedBodyRetry or
                        ScenarioId.DelayedResponse) &&
                    effectCount != 0)
                {
                    retryAfterEffectCount++;
                }

                if (scenario == ScenarioId.DisconnectBeforeAcceptance &&
                    effectCount == 0 &&
                    HasCompleteAttempt())
                {
                    retryAfterUnconfirmedCount++;
                }

                if (scenario == ScenarioId.DelayedResponse && pendingResponseCount != 0)
                {
                    retryBeforeResponseCount++;
                }
            }

            attempts.Add(new CapturedAttempt(role));
            connections.Add(connection, true);
            active++;
            return attempts.Count - 1;
        }
    }

    private async Task ObserveAsync(Socket connection, AdmittedEndpoint endpoint, int index)
    {
        var activeReleased = false;
        var pendingResponse = false;
        using var connectionCancellation = CancellationTokenSource.CreateLinkedTokenSource(scenarioToken);
        connectionCancellation.CancelAfter(RuntimeConstants.ConnectionTimeout);
        try
        {
            try
            {
                connection.NoDelay = true;
            }
            catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
            {
                MarkCaptureIncomplete();
                return;
            }

            var observed = await Http1Capture.ReadAsync(
                connection,
                endpoint.Address,
                connectionCancellation.Token).ConfigureAwait(false);
            lock (sync)
            {
                attempts[index].Apply(observed);
                if (!observed.CaptureComplete)
                {
                    captureMissing = true;
                }

                if (observed.Complete)
                {
                    connections[connection] = false;
                    if (CommitsEffect(endpoint.Role))
                    {
                        effectCount++;
                        if (scenario == ScenarioId.DelayedResponse)
                        {
                            pendingResponseCount++;
                            pendingResponse = true;
                        }
                    }
                }
            }

            if (!observed.Complete || observed.Reader is null)
            {
                return;
            }

            if (scenario != ScenarioId.DelayedResponse)
            {
                ReleaseActive();
                activeReleased = true;
            }

            switch (scenario)
            {
                case ScenarioId.AcceptThenDisconnect:
                case ScenarioId.DisconnectBeforeAcceptance:
                case ScenarioId.ChangedBodyRetry:
                    return;
                case ScenarioId.CrossOriginRedirectCredentials:
                    if (endpoint.Role == EndpointRole.Source)
                    {
                        var response = RedirectResponse();
                        try
                        {
                            await WriteResponseAsync(
                                connection,
                                index,
                                endpoint.Role,
                                response,
                                connectionCancellation.Token).ConfigureAwait(false);
                        }
                        finally
                        {
                            Array.Clear(response);
                        }
                    }
                    else
                    {
                        await WriteResponseAsync(
                            connection,
                            index,
                            endpoint.Role,
                            RuntimeConstants.NoContentResponse.ToArray(),
                            connectionCancellation.Token).ConfigureAwait(false);
                    }

                    await RecordPostResponseProbeAsync(
                        observed.Reader,
                        connectionCancellation.Token).ConfigureAwait(false);
                    return;
                case ScenarioId.RetryLimit:
                    await WriteResponseAsync(
                        connection,
                        index,
                        endpoint.Role,
                        RuntimeConstants.UnavailableResponse.ToArray(),
                        connectionCancellation.Token).ConfigureAwait(false);
                    await RecordPostResponseProbeAsync(
                        observed.Reader,
                        connectionCancellation.Token).ConfigureAwait(false);
                    return;
                case ScenarioId.DelayedResponse:
                    var monitor = new TrailingMonitor(observed.Reader, connectionCancellation.Token);
                    try
                    {
                        await Task.Delay(
                            RuntimeConstants.DelayedResponseDuration,
                            connectionCancellation.Token).ConfigureAwait(false);
                    }
                    catch (OperationCanceledException)
                    {
                        var canceledMonitor = await monitor.FinishAsync(
                            TimeSpan.Zero,
                            connectionCancellation.Token).ConfigureAwait(false);
                        if (!canceledMonitor.Complete)
                        {
                            MarkCaptureIncomplete();
                        }

                        return;
                    }

                    lock (sync)
                    {
                        delayCompleteCount++;
                    }

                    await WriteDelayedResponseAsync(
                        connection,
                        index,
                        RuntimeConstants.NoContentResponse.ToArray(),
                        connectionCancellation.Token).ConfigureAwait(false);
                    pendingResponse = false;
                    activeReleased = true;
                    var monitorResult = await monitor.FinishAsync(
                        RuntimeConstants.TrailingWindow,
                        connectionCancellation.Token).ConfigureAwait(false);
                    if (monitorResult.Trailing || !monitorResult.Complete)
                    {
                        MarkCaptureIncomplete();
                    }

                    return;
            }
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            MarkCaptureIncomplete();
        }
        finally
        {
            lock (sync)
            {
                connections.Remove(connection);
                if (pendingResponse && pendingResponseCount != 0)
                {
                    pendingResponseCount--;
                }

                if (!activeReleased && active != 0)
                {
                    active--;
                }
            }

            CloseSocket(connection);
        }
    }

    private bool HasCompleteAttempt()
    {
        foreach (var attempt in attempts)
        {
            if (attempt.Complete)
            {
                return true;
            }
        }

        return false;
    }

    private bool CommitsEffect(EndpointRole role)
    {
        return scenario is ScenarioId.AcceptThenDisconnect or
            ScenarioId.ChangedBodyRetry or
            ScenarioId.DelayedResponse ||
            scenario == ScenarioId.CrossOriginRedirectCredentials && role == EndpointRole.RedirectTarget;
    }

    private async Task RecordPostResponseProbeAsync(
        SocketWireReader reader,
        CancellationToken cancellationToken)
    {
        var probe = await Http1Capture.ProbeAsync(reader, cancellationToken).ConfigureAwait(false);
        if (probe.Trailing || !probe.Complete)
        {
            MarkCaptureIncomplete();
        }
    }

    private async Task WriteResponseAsync(
        Socket connection,
        int index,
        EndpointRole role,
        byte[] response,
        CancellationToken cancellationToken)
    {
        lock (sync)
        {
            responseAttemptCount++;
        }

        var complete = false;
        try
        {
            complete = await WriteAllAsync(connection, response, cancellationToken).ConfigureAwait(false);
        }
        finally
        {
            Array.Clear(response);
        }

        if (complete)
        {
            lock (sync)
            {
                responseCompleteCount++;
                if (index == 0 &&
                    (scenario != ScenarioId.CrossOriginRedirectCredentials || role == EndpointRole.Source))
                {
                    firstResponseComplete = true;
                }
            }
        }
    }

    private async Task WriteDelayedResponseAsync(
        Socket connection,
        int index,
        byte[] response,
        CancellationToken cancellationToken)
    {
        lock (sync)
        {
            responseAttemptCount++;
        }

        var complete = false;
        try
        {
            complete = await WriteAllAsync(connection, response, cancellationToken).ConfigureAwait(false);
        }
        finally
        {
            Array.Clear(response);
        }

        lock (sync)
        {
            if (complete)
            {
                responseCompleteCount++;
                if (index == 0)
                {
                    firstResponseComplete = true;
                }
            }

            if (pendingResponseCount != 0)
            {
                pendingResponseCount--;
            }

            if (active != 0)
            {
                active--;
            }
        }
    }

    private static async Task<bool> WriteAllAsync(
        Socket connection,
        byte[] response,
        CancellationToken cancellationToken)
    {
        var offset = 0;
        try
        {
            while (offset < response.Length)
            {
                var count = await connection.SendAsync(
                    response.AsMemory(offset),
                    SocketFlags.None,
                    cancellationToken).ConfigureAwait(false);
                if (count == 0)
                {
                    return false;
                }

                offset += count;
            }

            return true;
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return false;
        }
    }

    private byte[] RedirectResponse()
    {
        var target = Target(endpoints[1]);
        return Encoding.ASCII.GetBytes(
            string.Create(
                CultureInfo.InvariantCulture,
                $"HTTP/1.1 307 Temporary Redirect\r\nLocation: {target}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"));
    }

    private static string Target(AdmittedEndpoint endpoint)
    {
        return string.Create(
            CultureInfo.InvariantCulture,
            $"http://127.0.0.1:{endpoint.Address.Port}{RuntimeConstants.ControlledPath}");
    }

    private void ReleaseActive()
    {
        lock (sync)
        {
            if (active != 0)
            {
                active--;
            }
        }
    }

    private void MarkCaptureIncomplete()
    {
        lock (sync)
        {
            captureMissing = true;
        }
    }

    private void CloseAdmissions()
    {
        lock (sync)
        {
            if (closing)
            {
                return;
            }

            closing = true;
        }

        foreach (var endpoint in endpoints)
        {
            if (!endpoint.Close())
            {
                lock (sync)
                {
                    closeFailed = true;
                }
            }
        }
    }

    private void RequestStop()
    {
        try
        {
            stopScenario();
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            lock (sync)
            {
                closeFailed = true;
            }
        }
    }

    private void CloseSocket(Socket connection)
    {
        try
        {
            connection.Dispose();
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            lock (sync)
            {
                closeFailed = true;
            }
        }
    }

    private static async Task<bool> WaitAsync(Task[] tasks, CancellationToken cancellationToken)
    {
        if (tasks.Length == 0)
        {
            return true;
        }

        try
        {
            await Task.WhenAll(tasks).WaitAsync(cancellationToken).ConfigureAwait(false);
            return true;
        }
        catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
        {
            return false;
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return false;
        }
    }

    private static async Task AwaitAfterForcedCloseAsync(Task[] tasks)
    {
        if (tasks.Length == 0)
        {
            return;
        }

        try
        {
            await Task.WhenAll(tasks).ConfigureAwait(false);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            // Record the cleanup failure without exposing its cause.
        }
    }

    private sealed class CapturedAttempt
    {
        internal CapturedAttempt(EndpointRole role)
        {
            Role = role;
            MethodConsistent = true;
            DestinationConsistent = true;
            BodyConsistent = true;
        }

        internal EndpointRole Role { get; }

        internal bool HeadersObserved { get; private set; }

        internal bool Complete { get; private set; }

        internal bool MethodConsistent { get; private set; }

        internal bool DestinationConsistent { get; private set; }

        internal bool BodyConsistent { get; private set; }

        internal bool CredentialExact { get; private set; }

        internal bool CredentialExposed { get; private set; }

        internal void Apply(CaptureResult result)
        {
            HeadersObserved = result.HeadersObserved;
            Complete = result.Complete;
            MethodConsistent = result.MethodConsistent;
            DestinationConsistent = result.DestinationConsistent;
            BodyConsistent = result.BodyConsistent;
            CredentialExact = result.CredentialExact;
            CredentialExposed = result.CredentialExposed;
        }
    }
}

internal sealed class TrailingMonitor
{
    private readonly CancellationToken outerToken;
    private readonly CancellationTokenSource stop = new();
    private readonly Task<ProbeResult> task;

    internal TrailingMonitor(SocketWireReader reader, CancellationToken outerToken)
    {
        this.outerToken = outerToken;
        task = MonitorAsync(reader);
    }

    internal async Task<ProbeResult> FinishAsync(TimeSpan window, CancellationToken cancellationToken)
    {
        try
        {
            if (window > TimeSpan.Zero)
            {
                using var timerCancellation = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
                var timer = Task.Delay(window, timerCancellation.Token);
                var completed = await Task.WhenAny(task, timer).ConfigureAwait(false);
                timerCancellation.Cancel();
                try
                {
                    await timer.ConfigureAwait(false);
                }
                catch (OperationCanceledException)
                {
                    // The timer is product-owned and joined before return.
                }

                if (completed == task)
                {
                    return await task.ConfigureAwait(false);
                }
            }

            stop.Cancel();
            var result = await task.ConfigureAwait(false);
            return new ProbeResult(
                result.Trailing,
                result.Complete && !cancellationToken.IsCancellationRequested);
        }
        finally
        {
            stop.Dispose();
        }
    }

    private async Task<ProbeResult> MonitorAsync(SocketWireReader reader)
    {
        if (reader.BufferedCount != 0)
        {
            return new ProbeResult(true, true);
        }

        if (reader.RemainingBudget == 0)
        {
            return new ProbeResult(false, false);
        }

        using var linked = CancellationTokenSource.CreateLinkedTokenSource(outerToken, stop.Token);
        try
        {
            var value = await reader.ReadByteAsync(linked.Token).ConfigureAwait(false);
            return value switch
            {
                SocketWireReader.EndOfStream => new ProbeResult(false, true),
                SocketWireReader.BudgetExhausted => new ProbeResult(false, false),
                _ => new ProbeResult(true, true),
            };
        }
        catch (OperationCanceledException) when (stop.IsCancellationRequested && !outerToken.IsCancellationRequested)
        {
            return new ProbeResult(false, true);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return new ProbeResult(false, false);
        }
    }
}
