using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Net.Http;
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
    }

    internal TaskCompletionSource Entered { get; } = new(
        TaskCreationOptions.RunContinuationsAsynchronously);

    internal int Calls { get; private set; }

    internal int DisposeCalls { get; private set; }

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
                throw new MarkerException();
            case ControlledHandlerMode.AsynchronousException:
                return Task.FromException<HttpResponseMessage>(new MarkerException());
            case ControlledHandlerMode.NullTask:
                return null!;
            case ControlledHandlerMode.CanceledTask:
                return Task.FromCanceled<HttpResponseMessage>(new CancellationToken(true));
            case ControlledHandlerMode.NullResponse:
                return Task.FromResult<HttpResponseMessage>(null!);
            case ControlledHandlerMode.DisposalFailure:
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent)
                {
                    Content = new ThrowingDisposeContent(),
                });
            case ControlledHandlerMode.WaitForCancellation:
                return WaitForCancellationAsync(cancellationToken);
            case ControlledHandlerMode.ReplaceRequestContent:
                ReplacementContent = new TrackingContent();
                request.Content = ReplacementContent;
                return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent));
            case ControlledHandlerMode.ReplaceWithThrowingContent:
                ReplacementContent = new ThrowingDisposeContent();
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

    private static async Task<HttpResponseMessage> ReplayDisposedContentAsync(
        HttpContent content,
        CancellationToken cancellationToken)
    {
        using var destination = new MemoryStream();
        await content.CopyToAsync(destination, cancellationToken).ConfigureAwait(false);
        return new HttpResponseMessage(HttpStatusCode.NoContent);
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

internal enum RedirectCredentialMode
{
    Exact,
    Prefix,
    Suffix,
    Unrelated,
    MultipleWithSuffix,
    DuplicateSource,
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
            default:
                throw new InvalidOperationException("unknown redirect credential mode");
        }

        return await base.SendAsync(redirected, cancellationToken).ConfigureAwait(false);
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

internal sealed class ThrowAfterScenarioResponseHandler : DelegatingHandler
{
    private readonly int targetCall;
    private readonly int attempts;

    internal ThrowAfterScenarioResponseHandler(int targetCall, int attempts = 1)
        : base(new SocketsHttpHandler
        {
            AllowAutoRedirect = false,
            UseCookies = false,
            UseProxy = false,
        })
    {
        this.targetCall = targetCall;
        this.attempts = attempts;
    }

    internal int Calls { get; private set; }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        Calls++;
        var call = Calls;
        var response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        if (call != targetCall)
        {
            return response;
        }

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

        response.Dispose();
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
    protected override void Dispose(bool disposing)
    {
        base.Dispose(disposing);
        if (disposing)
        {
            throw new InvalidOperationException("marker-response-disposal");
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
