using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.Tests.Runtime;

internal static class RuntimeTestClients
{
    internal static HttpClient CreateOrdinaryClient(HttpMessageHandler? outer = null)
    {
        var handler = outer ?? CreateSocketsHandler();
        return new HttpClient(handler, true)
        {
            DefaultRequestVersion = HttpVersion.Version11,
            DefaultVersionPolicy = HttpVersionPolicy.RequestVersionExact,
            Timeout = Timeout.InfiniteTimeSpan,
        };
    }

    internal static SocketsHttpHandler CreateSocketsHandler()
    {
        return new SocketsHttpHandler
        {
            AllowAutoRedirect = true,
            MaxAutomaticRedirections = 2,
            UseCookies = false,
            UseProxy = false,
        };
    }
}

internal enum ControlledHandlerMode
{
    ImmediateResponse,
    SynchronousException,
    AsynchronousException,
    NullTask,
    CanceledTask,
    NullResponse,
    DisposalFailure,
    WaitForCancellation,
    ReplaceRequestContent,
    ReplaceWithThrowingContent,
    DisposeThenReplayContent,
    MarkerResponse,
}

internal sealed class ControlledHandler : HttpMessageHandler
{
    private readonly ControlledHandlerMode mode;
    private readonly ConcurrentQueue<Uri> targets = new();

    internal ControlledHandler(ControlledHandlerMode mode)
    {
        this.mode = mode;
        Failure = new MarkerException();
    }

    internal TaskCompletionSource Entered { get; } = new(
        TaskCreationOptions.RunContinuationsAsynchronously);

    internal int Calls { get; private set; }

    internal int DisposeCalls { get; private set; }

    internal Exception Failure { get; private set; }

    internal TrackingContent? ReplacementContent { get; private set; }

    internal IReadOnlyCollection<Uri> Targets => targets.ToArray();

    protected override Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        Calls++;
        if (request.RequestUri is not null)
        {
            targets.Enqueue(request.RequestUri);
        }

        Entered.TrySetResult();
        switch (mode)
        {
            case ControlledHandlerMode.ImmediateResponse:
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent));
            case ControlledHandlerMode.SynchronousException:
                throw Failure;
            case ControlledHandlerMode.AsynchronousException:
                return ThrowAsynchronouslyAsync(Failure);
            case ControlledHandlerMode.NullTask:
                return null!;
            case ControlledHandlerMode.CanceledTask:
                return Task.FromCanceled<HttpResponseMessage>(new CancellationToken(true));
            case ControlledHandlerMode.NullResponse:
                return Task.FromResult<HttpResponseMessage>(null!);
            case ControlledHandlerMode.DisposalFailure:
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent)
                {
                    Content = new ThrowingDisposeContent(Failure),
                });
            case ControlledHandlerMode.WaitForCancellation:
                return WaitForCancellationAsync(cancellationToken);
            case ControlledHandlerMode.ReplaceRequestContent:
                ReplacementContent = new TrackingContent();
                request.Content = ReplacementContent;
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent));
            case ControlledHandlerMode.ReplaceWithThrowingContent:
                ReplacementContent = new ThrowingDisposeContent(Failure);
                request.Content = ReplacementContent;
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent));
            case ControlledHandlerMode.DisposeThenReplayContent:
                request.Content!.Dispose();
                return ReplayDisposedContentAsync(request.Content, cancellationToken);
            case ControlledHandlerMode.MarkerResponse:
                var response = new HttpResponseMessage(HttpStatusCode.NoContent)
                {
                    ReasonPhrase = "marker-response-reason-secret",
                    Content = new StringContent("marker-response-content-secret"),
                };
                response.Headers.TryAddWithoutValidation(
                    "X-Marker",
                    "marker-response-header-secret");
                return Task.FromResult(response);
            default:
                throw new InvalidOperationException("unknown controlled handler mode");
        }
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            DisposeCalls++;
        }

        base.Dispose(disposing);
    }

    public override string ToString()
    {
        return "marker-handler-formatter-secret";
    }

    private static async Task<HttpResponseMessage> WaitForCancellationAsync(
        CancellationToken cancellationToken)
    {
        await Task.Delay(Timeout.InfiniteTimeSpan, cancellationToken).ConfigureAwait(false);
        throw new InvalidOperationException("unreachable controlled handler state");
    }

    private static async Task<HttpResponseMessage> ThrowAsynchronouslyAsync(Exception failure)
    {
        await Task.Yield();
        throw failure;
    }

    private async Task<HttpResponseMessage> ReplayDisposedContentAsync(
        HttpContent content,
        CancellationToken cancellationToken)
    {
        try
        {
            using var destination = new MemoryStream();
            await content.CopyToAsync(destination, cancellationToken).ConfigureAwait(false);
            return new HttpResponseMessage(HttpStatusCode.NoContent);
        }
        catch (Exception exception)
        {
            Failure = exception;
            throw;
        }
    }

    private sealed class MarkerException : Exception
    {
        internal MarkerException()
            : base("marker-sender-exception-secret")
        {
        }
    }
}

internal sealed class RetryingHandler : DelegatingHandler
{
    internal RetryingHandler()
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        try
        {
            return await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        }
        catch (HttpRequestException)
        {
            using var replay = await RequestClone.CreateAsync(
                request,
                request.RequestUri,
                cancellationToken).ConfigureAwait(false);
            return await base.SendAsync(replay, cancellationToken).ConfigureAwait(false);
        }
    }
}

internal sealed class FixedAttemptHandler : DelegatingHandler
{
    private readonly int maximumAttempts;

    internal FixedAttemptHandler(int maximumAttempts)
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
        this.maximumAttempts = maximumAttempts;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        var replays = new List<HttpRequestMessage>(maximumAttempts - 1);
        try
        {
            for (var index = 1; index < maximumAttempts; index++)
            {
                replays.Add(await RequestClone.CreateAsync(
                    request,
                    request.RequestUri,
                    cancellationToken).ConfigureAwait(false));
            }

            for (var index = 0; index < maximumAttempts; index++)
            {
                var current = index == 0 ? request : replays[index - 1];
                try
                {
                    var response = await base.SendAsync(current, cancellationToken).ConfigureAwait(false);
                    if (index + 1 == maximumAttempts ||
                        response.StatusCode != HttpStatusCode.ServiceUnavailable)
                    {
                        return response;
                    }

                    response.Dispose();
                }
                catch (HttpRequestException) when (index + 1 < maximumAttempts)
                {
                    // Retry the copied request until the configured test budget is exhausted.
                }
            }

            throw new InvalidOperationException("fixed-attempt handler reached an impossible state");
        }
        finally
        {
            foreach (var replay in replays)
            {
                replay.Dispose();
            }
        }
    }
}

internal sealed class DetachedBackgroundRetryHandler : DelegatingHandler
{
    private readonly HttpMessageInvoker backgroundInvoker = new(
        RuntimeTestClients.CreateSocketsHandler(),
        disposeHandler: true);
    private Task backgroundRetry = Task.CompletedTask;
    private int calls;
    private int retryScheduled;

    internal DetachedBackgroundRetryHandler()
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
    }

    internal Task BackgroundRetry => Volatile.Read(ref backgroundRetry);

    internal bool RetryScheduled => Volatile.Read(ref retryScheduled) != 0;

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        if (Interlocked.Increment(ref calls) != 1)
        {
            return await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        }

        var replay = await RequestClone.CreateAsync(
            request,
            request.RequestUri,
            cancellationToken).ConfigureAwait(false);
        try
        {
            return await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        }
        finally
        {
            Volatile.Write(ref retryScheduled, 1);
            Volatile.Write(ref backgroundRetry, SendAfterReturnAsync(replay));
        }
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            backgroundInvoker.Dispose();
        }

        base.Dispose(disposing);
    }

    private async Task SendAfterReturnAsync(HttpRequestMessage replay)
    {
        try
        {
            await Task.Delay(TimeSpan.FromMilliseconds(50)).ConfigureAwait(false);
            try
            {
                using var response = await backgroundInvoker.SendAsync(
                    replay,
                    CancellationToken.None).ConfigureAwait(false);
            }
            catch (HttpRequestException)
            {
                // The controlled disconnect is the expected response to this replay.
            }
        }
        finally
        {
            replay.Dispose();
        }
    }
}

internal sealed class BareConnectionProbeHandler : DelegatingHandler
{
    internal BareConnectionProbeHandler()
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        try
        {
            return await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        }
        catch (HttpRequestException)
        {
            Uri target = request.RequestUri
                ?? throw new InvalidOperationException("controlled target is absent");
            using var accepted = new SocketAcceptPhaseObserver();
            using var probe = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp)
            {
                NoDelay = true,
            };
            await probe.ConnectAsync(
                new IPEndPoint(new IPAddress(new byte[] { 127, 0, 0, 1 }), target.Port),
                cancellationToken).ConfigureAwait(false);
            await accepted.Observed.WaitAsync(
                TimeSpan.FromSeconds(1),
                cancellationToken).ConfigureAwait(false);
            throw;
        }
    }
}

internal enum RedirectCredentialMode
{
    Exact,
    Prefix,
    Suffix,
    Unrelated,
    MultipleWithSuffix,
    DuplicateSource,
    Cookie,
    CustomHeader,
    MalformedTarget,
}

internal sealed class RedirectExposureHandler : DelegatingHandler
{
    private readonly RedirectCredentialMode mode;

    internal RedirectExposureHandler(RedirectCredentialMode mode = RedirectCredentialMode.Exact)
        : base(new SocketsHttpHandler
        {
            AllowAutoRedirect = false,
            UseCookies = false,
            UseProxy = false,
        })
    {
        this.mode = mode;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        var credential = request.Headers.TryGetValues("Authorization", out var values)
            ? string.Join(", ", values)
            : throw new InvalidOperationException("controlled credential is absent");
        if (mode == RedirectCredentialMode.DuplicateSource)
        {
            request.Headers.Remove("Authorization");
            request.Headers.TryAddWithoutValidation(
                "Authorization",
                new[] { credential, "Bearer unrelated-credential" });
        }

        var response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        if (response.StatusCode != HttpStatusCode.TemporaryRedirect || response.Headers.Location is null)
        {
            return response;
        }

        var target = response.Headers.Location;
        response.Dispose();
        if (mode == RedirectCredentialMode.MalformedTarget)
        {
            await SendMalformedTargetAsync(target, credential, cancellationToken).ConfigureAwait(false);
            return new HttpResponseMessage(HttpStatusCode.NoContent);
        }

        using var redirected = await RequestClone.CreateAsync(
            request,
            target,
            cancellationToken).ConfigureAwait(false);
        redirected.Headers.Remove("Authorization");
        switch (mode)
        {
            case RedirectCredentialMode.Exact:
                redirected.Headers.TryAddWithoutValidation("Authorization", credential);
                break;
            case RedirectCredentialMode.Prefix:
                redirected.Headers.TryAddWithoutValidation("Authorization", $"forwarded {credential}");
                break;
            case RedirectCredentialMode.Suffix:
                redirected.Headers.TryAddWithoutValidation("Authorization", $"{credential} transformed");
                break;
            case RedirectCredentialMode.Unrelated:
            case RedirectCredentialMode.DuplicateSource:
                redirected.Headers.TryAddWithoutValidation(
                    "Authorization",
                    "Bearer unrelated-credential");
                break;
            case RedirectCredentialMode.MultipleWithSuffix:
                redirected.Headers.TryAddWithoutValidation(
                    "Authorization",
                    new[]
                    {
                        "Bearer unrelated-credential",
                        $"{credential} transformed",
                    });
                break;
            case RedirectCredentialMode.Cookie:
                redirected.Headers.TryAddWithoutValidation(
                    "Cookie",
                    "retry-check=" + MarkerFrom(credential));
                break;
            case RedirectCredentialMode.CustomHeader:
                redirected.Headers.TryAddWithoutValidation(
                    "X-Forwarded-Retry-Check",
                    "copied-" + MarkerFrom(credential) + "-value");
                break;
            default:
                throw new InvalidOperationException("unknown redirect credential mode");
        }

        return await base.SendAsync(redirected, cancellationToken).ConfigureAwait(false);
    }

    private static string MarkerFrom(string credential)
    {
        const string prefix = "Bearer ";
        return credential.StartsWith(prefix, StringComparison.Ordinal)
            ? credential[prefix.Length..]
            : throw new InvalidOperationException("controlled credential shape is invalid");
    }

    private static async Task SendMalformedTargetAsync(
        Uri target,
        string credential,
        CancellationToken cancellationToken)
    {
        using var socket = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
        await socket.ConnectAsync(
            IPAddress.Loopback,
            target.Port,
            cancellationToken).ConfigureAwait(false);
        var wire = Encoding.ASCII.GetBytes(
            $"POST {target.PathAndQuery} HTTP/1.1\r\n" +
            $"Host: {target.Host}:{target.Port}\r\n" +
            $"Cookie: retry-check={MarkerFrom(credential)}\r\n" +
            "Malformed Header: value\r\n" +
            "Content-Length: 0\r\n\r\n");
        try
        {
            var offset = 0;
            while (offset < wire.Length)
            {
                var sent = await socket.SendAsync(
                    wire.AsMemory(offset),
                    SocketFlags.None,
                    cancellationToken).ConfigureAwait(false);
                if (sent == 0)
                {
                    throw new InvalidOperationException("controlled malformed request was not sent");
                }

                offset += sent;
            }

            socket.Shutdown(SocketShutdown.Send);
            var response = new byte[1];
            try
            {
                while (await socket.ReceiveAsync(
                        response.AsMemory(),
                        SocketFlags.None,
                        cancellationToken).ConfigureAwait(false) != 0)
                {
                }
            }
            finally
            {
                Array.Clear(response);
            }
        }
        finally
        {
            Array.Clear(wire);
        }
    }
}

internal sealed class RetryStatusHandler : DelegatingHandler
{
    private readonly int attempts;

    internal RetryStatusHandler(int attempts)
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
        this.attempts = attempts;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        var response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        for (var attempt = 1;
             attempt < attempts && response.StatusCode == HttpStatusCode.ServiceUnavailable;
             attempt++)
        {
            response.Dispose();
            using var replay = await RequestClone.CreateAsync(
                request,
                request.RequestUri,
                cancellationToken).ConfigureAwait(false);
            response = await base.SendAsync(replay, cancellationToken).ConfigureAwait(false);
        }

        return response;
    }
}

internal sealed class ScenarioTokenRecordingHandler : DelegatingHandler
{
    private int calls;

    internal ScenarioTokenRecordingHandler()
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
    }

    internal CancellationToken DelayedScenarioToken { get; private set; }

    protected override Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        calls++;
        if (calls == 6)
        {
            DelayedScenarioToken = cancellationToken;
        }

        return base.SendAsync(request, cancellationToken);
    }
}

internal enum HttpRequestFailureDelivery
{
    Asynchronous,
    Synchronous,
}

internal sealed class ScenarioHttpRequestFailureHandler : DelegatingHandler
{
    private readonly int targetCall;
    private readonly int completedAttempts;
    private readonly HttpRequestFailureDelivery delivery;

    internal ScenarioHttpRequestFailureHandler(
        int targetCall,
        int completedAttempts,
        HttpRequestFailureDelivery delivery = HttpRequestFailureDelivery.Asynchronous)
        : base(new SocketsHttpHandler
        {
            AllowAutoRedirect = false,
            UseCookies = false,
            UseProxy = false,
        })
    {
        this.targetCall = targetCall;
        this.completedAttempts = completedAttempts;
        this.delivery = delivery;
    }

    internal int Calls { get; private set; }

    protected override Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        Calls++;
        if (Calls != targetCall)
        {
            return base.SendAsync(request, cancellationToken);
        }

        var failure = CompleteAttemptsThenFailAsync(request, cancellationToken);
        if (delivery == HttpRequestFailureDelivery.Asynchronous)
        {
            return failure;
        }

        _ = failure.GetAwaiter().GetResult();
        throw new InvalidOperationException("controlled synchronous failure did not throw");
    }

    private async Task<HttpResponseMessage> CompleteAttemptsThenFailAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        HttpResponseMessage? response = null;
        if (completedAttempts != 0)
        {
            response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        }

        for (var attempt = 1;
             attempt < completedAttempts && response?.StatusCode == HttpStatusCode.ServiceUnavailable;
             attempt++)
        {
            response.Dispose();
            using var replay = await RequestClone.CreateAsync(
                request,
                request.RequestUri,
                cancellationToken).ConfigureAwait(false);
            response = await base.SendAsync(replay, cancellationToken).ConfigureAwait(false);
        }

        response?.Dispose();
        throw new HttpRequestException("marker-post-response-failure-secret");
    }
}

internal static class RequestClone
{
    internal static async Task<HttpRequestMessage> CreateAsync(
        HttpRequestMessage request,
        Uri? target,
        CancellationToken cancellationToken)
    {
        var body = new MemoryStream();
        await request.Content!.CopyToAsync(body, cancellationToken).ConfigureAwait(false);
        var clone = new HttpRequestMessage(request.Method, target)
        {
            Version = request.Version,
            VersionPolicy = request.VersionPolicy,
            Content = new ByteArrayContent(body.ToArray()),
        };
        foreach (var header in request.Headers)
        {
            clone.Headers.TryAddWithoutValidation(header.Key, header.Value);
        }

        body.Dispose();
        return clone;
    }
}

internal class TrackingContent : HttpContent
{
    internal int DisposeCalls { get; private set; }

    protected override Task SerializeToStreamAsync(Stream stream, TransportContext? context)
    {
        return Task.CompletedTask;
    }

    protected override bool TryComputeLength(out long length)
    {
        length = 0;
        return true;
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            DisposeCalls++;
        }

        base.Dispose(disposing);
    }
}

internal sealed class ThrowingDisposeContent : TrackingContent
{
    private readonly Exception failure;

    internal ThrowingDisposeContent(Exception failure)
    {
        this.failure = failure;
    }

    protected override void Dispose(bool disposing)
    {
        base.Dispose(disposing);
        if (disposing)
        {
            throw failure;
        }
    }
}

internal sealed class QueuedSynchronizationContext : SynchronizationContext
{
    private readonly Queue<(SendOrPostCallback Callback, object? State)> callbacks = new();

    public override void Post(SendOrPostCallback d, object? state)
    {
        callbacks.Enqueue((d, state));
    }

    internal void RunOne()
    {
        var work = callbacks.Dequeue();
        work.Callback(work.State);
    }
}
