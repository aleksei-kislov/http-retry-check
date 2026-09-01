using System;
using System.Collections.Generic;
using System.Diagnostics.Tracing;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.Tests.Runtime;

internal enum RawOriginControlMode
{
    AcceptPreEffectOverlap,
    ChangedBodyPreEffectOverlap,
    DelayedOverlap,
    SameConnectionTrailing,
    MalformedHeader,
    ExactHeaderLimit,
    HeaderLimitExceeded,
    ExactBodyLimit,
    BodyLimitExceeded,
    SharedWireLimitExceeded,
}

internal sealed class RawOriginControlHandler : HttpMessageHandler
{
    private const int HeaderLimit = 64 << 10;
    private const int BodyLimit = 1 << 20;
    private const int WireLimit = BodyLimit + 1;
    private readonly RawOriginControlMode mode;
    private int calls;

    internal RawOriginControlHandler(RawOriginControlMode mode)
    {
        this.mode = mode;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        calls++;
        var targetCall = mode switch
        {
            RawOriginControlMode.AcceptPreEffectOverlap => 1,
            RawOriginControlMode.ChangedBodyPreEffectOverlap => 3,
            RawOriginControlMode.DelayedOverlap or RawOriginControlMode.SameConnectionTrailing => 6,
            _ => 5,
        };
        if (calls != targetCall)
        {
            return new HttpResponseMessage(HttpStatusCode.NoContent);
        }

        var target = request.RequestUri ?? throw new InvalidOperationException("controlled target is absent");
        if (!target.Host.Equals("127.0.0.1", StringComparison.Ordinal))
        {
            throw new InvalidOperationException("controlled target is not literal loopback");
        }

        var credential = request.Headers.TryGetValues("Authorization", out var values)
            ? values.Single()
            : throw new InvalidOperationException("controlled credential is absent");
        var ordinaryBody = await request.Content!.ReadAsByteArrayAsync(cancellationToken).ConfigureAwait(false);
        switch (mode)
        {
            case RawOriginControlMode.AcceptPreEffectOverlap:
            case RawOriginControlMode.ChangedBodyPreEffectOverlap:
                await SendPreEffectOverlapAsync(
                    target,
                    WithoutLastByte(BuildContentLengthRequest(target, credential, ordinaryBody)),
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.DelayedOverlap:
                await SendDelayedOverlapAsync(
                    target,
                    BuildContentLengthRequest(target, credential, ordinaryBody),
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.SameConnectionTrailing:
                await SendDelayedTrailingAsync(
                    target,
                    BuildContentLengthRequest(target, credential, ordinaryBody),
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.MalformedHeader:
                await SendSingleAsync(
                    target,
                    BuildMalformedRequest(target, credential, ordinaryBody),
                    true,
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.ExactHeaderLimit:
                await SendSingleAsync(
                    target,
                    BuildHeaderSizedRequest(target, credential, ordinaryBody, HeaderLimit),
                    false,
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.HeaderLimitExceeded:
                await SendSingleAsync(
                    target,
                    BuildHeaderSizedRequest(target, credential, ordinaryBody, HeaderLimit + 1),
                    true,
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.ExactBodyLimit:
                await SendSingleAsync(
                    target,
                    BuildContentLengthRequest(target, credential, FilledBody(BodyLimit)),
                    false,
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.BodyLimitExceeded:
                await SendSingleAsync(
                    target,
                    BuildContentLengthRequest(target, credential, FilledBody(BodyLimit + 1)),
                    true,
                    cancellationToken).ConfigureAwait(false);
                break;
            case RawOriginControlMode.SharedWireLimitExceeded:
                await SendSingleAsync(
                    target,
                    BuildSharedWireExhaustionRequest(target, credential),
                    true,
                    cancellationToken).ConfigureAwait(false);
                break;
            default:
                throw new InvalidOperationException("unknown raw origin control");
        }

        return new HttpResponseMessage(HttpStatusCode.NoContent);
    }

    private static async Task SendDelayedOverlapAsync(
        Uri target,
        byte[] request,
        CancellationToken cancellationToken)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(4));
        using var accepted = new SocketAcceptPhaseObserver();
        using var first = await ConnectAsync(target, timeout.Token).ConfigureAwait(false);
        await SendAllAsync(first, request, false, timeout.Token).ConfigureAwait(false);
        await accepted.Observed.WaitAsync(TimeSpan.FromSeconds(1), timeout.Token).ConfigureAwait(false);
        ShutdownSend(first);
        var firstDrain = DrainAsync(first, timeout.Token);

        await Task.Delay(TimeSpan.FromMilliseconds(100), timeout.Token).ConfigureAwait(false);
        using var second = await ConnectAsync(target, timeout.Token).ConfigureAwait(false);
        await SendAllAsync(second, request, false, timeout.Token).ConfigureAwait(false);
        ShutdownSend(second);
        var secondDrain = DrainAsync(second, timeout.Token);
        await Task.WhenAll(firstDrain, secondDrain).ConfigureAwait(false);
    }

    private static async Task SendPreEffectOverlapAsync(
        Uri target,
        byte[] partialRequest,
        CancellationToken cancellationToken)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(4));
        using var firstAccepted = new SocketAcceptPhaseObserver();
        using var first = await ConnectAsync(target, timeout.Token).ConfigureAwait(false);
        await firstAccepted.Observed.WaitAsync(TimeSpan.FromSeconds(1), timeout.Token).ConfigureAwait(false);
        await SendAllAsync(first, partialRequest, false, timeout.Token).ConfigureAwait(false);

        using var secondAccepted = new SocketAcceptPhaseObserver();
        using var second = await ConnectAsync(target, timeout.Token).ConfigureAwait(false);
        await secondAccepted.Observed.WaitAsync(TimeSpan.FromSeconds(1), timeout.Token).ConfigureAwait(false);
        await SendAllAsync(second, partialRequest, false, timeout.Token).ConfigureAwait(false);

        ShutdownSend(first);
        ShutdownSend(second);
        await Task.WhenAll(
            DrainAsync(first, timeout.Token),
            DrainAsync(second, timeout.Token)).ConfigureAwait(false);
    }

    private static async Task SendSingleAsync(
        Uri target,
        byte[] request,
        bool allowEarlyClose,
        CancellationToken cancellationToken)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(4));
        using var socket = await ConnectAsync(target, timeout.Token).ConfigureAwait(false);
        await SendAllAsync(socket, request, allowEarlyClose, timeout.Token).ConfigureAwait(false);
        ShutdownSend(socket);
        await DrainAsync(socket, timeout.Token).ConfigureAwait(false);
    }

    private static async Task SendDelayedTrailingAsync(
        Uri target,
        byte[] request,
        CancellationToken cancellationToken)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(4));
        using var accepted = new SocketAcceptPhaseObserver();
        using var socket = await ConnectAsync(target, timeout.Token).ConfigureAwait(false);
        await SendAllAsync(socket, request, false, timeout.Token).ConfigureAwait(false);
        await accepted.Observed.WaitAsync(TimeSpan.FromSeconds(1), timeout.Token).ConfigureAwait(false);
        await Task.Delay(TimeSpan.FromMilliseconds(100), timeout.Token).ConfigureAwait(false);
        await SendAllAsync(socket, [(byte)'!'], false, timeout.Token).ConfigureAwait(false);
        ShutdownSend(socket);
        await DrainAsync(socket, timeout.Token).ConfigureAwait(false);
    }

    private static async Task<Socket> ConnectAsync(Uri target, CancellationToken cancellationToken)
    {
        var socket = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp)
        {
            NoDelay = true,
        };
        try
        {
            await socket.ConnectAsync(
                new IPEndPoint(IPAddress.Parse(target.Host), target.Port),
                cancellationToken).ConfigureAwait(false);
            return socket;
        }
        catch
        {
            socket.Dispose();
            throw;
        }
    }

    private static async Task SendAllAsync(
        Socket socket,
        byte[] request,
        bool allowEarlyClose,
        CancellationToken cancellationToken)
    {
        var offset = 0;
        try
        {
            while (offset < request.Length)
            {
                var count = await socket.SendAsync(
                    request.AsMemory(offset),
                    SocketFlags.None,
                    cancellationToken).ConfigureAwait(false);
                if (count == 0)
                {
                    if (allowEarlyClose)
                    {
                        return;
                    }

                    throw new IOException("controlled socket closed before the request was sent");
                }

                offset += count;
            }
        }
        catch (SocketException) when (allowEarlyClose)
        {
            // Limit rejection may close the controlled connection before the final buffered send.
        }
    }

    private static async Task DrainAsync(Socket socket, CancellationToken cancellationToken)
    {
        var buffer = new byte[4096];
        try
        {
            while (await socket.ReceiveAsync(
                       buffer.AsMemory(),
                       SocketFlags.None,
                       cancellationToken).ConfigureAwait(false) != 0)
            {
            }
        }
        catch (SocketException)
        {
            // A malformed controlled request may be rejected with a reset rather than EOF.
        }
        finally
        {
            Array.Clear(buffer);
        }
    }

    private static void ShutdownSend(Socket socket)
    {
        try
        {
            socket.Shutdown(SocketShutdown.Send);
        }
        catch (SocketException)
        {
            // The controlled origin may already have rejected and closed the connection.
        }
    }

    private static byte[] BuildContentLengthRequest(Uri target, string credential, byte[] body)
    {
        var header = Encoding.ASCII.GetBytes(
            $"POST {target.PathAndQuery} HTTP/1.1\r\n" +
            $"Host: {target.Host}:{target.Port}\r\n" +
            $"Authorization: {credential}\r\n" +
            $"Content-Length: {body.Length}\r\n" +
            "Connection: close\r\n\r\n");
        return Combine(header, body);
    }

    private static byte[] BuildMalformedRequest(Uri target, string credential, byte[] body)
    {
        var header = Encoding.ASCII.GetBytes(
            $"POST {target.PathAndQuery} HTTP/1.1\r\n" +
            $"Host: {target.Host}:{target.Port}\r\n" +
            $"Authorization: {credential}\r\n" +
            "Malformed Header\r\n" +
            $"Content-Length: {body.Length}\r\n\r\n");
        return Combine(header, body);
    }

    private static byte[] BuildHeaderSizedRequest(
        Uri target,
        string credential,
        byte[] body,
        int headerLength)
    {
        var prefix = Encoding.ASCII.GetBytes(
            $"POST {target.PathAndQuery} HTTP/1.1\r\n" +
            $"Host: {target.Host}:{target.Port}\r\n" +
            $"Authorization: {credential}\r\n" +
            $"Content-Length: {body.Length}\r\n" +
            "Connection: close\r\n" +
            "X-Fill: ");
        var suffix = "\r\n\r\n"u8.ToArray();
        var fillLength = headerLength - prefix.Length - suffix.Length;
        if (fillLength < 0)
        {
            throw new InvalidOperationException("controlled header size is too small");
        }

        var header = new byte[headerLength];
        prefix.CopyTo(header, 0);
        Array.Fill(header, (byte)'a', prefix.Length, fillLength);
        suffix.CopyTo(header, prefix.Length + fillLength);
        return Combine(header, body);
    }

    private static byte[] BuildSharedWireExhaustionRequest(Uri target, string credential)
    {
        var header = Encoding.ASCII.GetBytes(
            $"POST {target.PathAndQuery} HTTP/1.1\r\n" +
            $"Host: {target.Host}:{target.Port}\r\n" +
            $"Authorization: {credential}\r\n" +
            "Transfer-Encoding: chunked\r\n" +
            "Connection: close\r\n\r\n");
        var framing = new byte[WireLimit];
        framing[0] = (byte)'1';
        framing[1] = (byte)';';
        Array.Fill(framing, (byte)'a', 2, framing.Length - 3);
        framing[^1] = (byte)'\r';
        return Combine(header, framing);
    }

    private static byte[] FilledBody(int length)
    {
        var body = new byte[length];
        Array.Fill(body, (byte)'x');
        return body;
    }

    private static byte[] WithoutLastByte(byte[] source)
    {
        if (source.Length == 0)
        {
            throw new InvalidOperationException("controlled request cannot be empty");
        }

        var result = new byte[source.Length - 1];
        Buffer.BlockCopy(source, 0, result, 0, result.Length);
        Array.Clear(source);
        return result;
    }

    private static byte[] Combine(byte[] first, byte[] second)
    {
        var result = new byte[first.Length + second.Length];
        first.CopyTo(result, 0);
        second.CopyTo(result, first.Length);
        return result;
    }
}

internal sealed class SuccessfulResponseTrackingHandler : HttpMessageHandler
{
    private readonly List<TrackingContent> contents = new();

    internal IReadOnlyList<TrackingContent> Contents => contents.AsReadOnly();

    protected override Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        var content = new TrackingContent();
        contents.Add(content);
        return Task.FromResult(new HttpResponseMessage(HttpStatusCode.NoContent)
        {
            Content = content,
        });
    }
}

internal sealed class SixthCallTimeoutHandler : DelegatingHandler
{
    private int calls;

    internal SixthCallTimeoutHandler()
        : base(RuntimeTestClients.CreateSocketsHandler())
    {
    }

    internal int Calls => calls;

    internal bool TimeoutObserved { get; private set; }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        var call = Interlocked.Increment(ref calls);
        if (call != 6)
        {
            return new HttpResponseMessage(HttpStatusCode.NoContent);
        }

        using var response = await base.SendAsync(request, CancellationToken.None).ConfigureAwait(false);
        TimeoutObserved = cancellationToken.IsCancellationRequested;
        cancellationToken.ThrowIfCancellationRequested();
        throw new InvalidOperationException("controlled finite timeout did not expire");
    }
}

internal sealed class ListenerPortRecordingHandler : DelegatingHandler
{
    private readonly List<Uri> targets = new();

    internal ListenerPortRecordingHandler()
        : base(new SocketsHttpHandler
        {
            AllowAutoRedirect = false,
            UseCookies = false,
            UseProxy = false,
        })
    {
    }

    internal IReadOnlyList<Uri> Targets => targets.AsReadOnly();

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        var target = request.RequestUri ?? throw new InvalidOperationException("controlled target is absent");
        targets.Add(target);
        var response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        if (response.StatusCode == HttpStatusCode.TemporaryRedirect && response.Headers.Location is not null)
        {
            targets.Add(response.Headers.Location.IsAbsoluteUri
                ? response.Headers.Location
                : new Uri(target, response.Headers.Location));
        }

        return response;
    }
}

internal sealed class SocketAcceptPhaseObserver : EventListener
{
    private EventSource? socketSource;
    private TaskCompletionSource? observed;
    private bool initialized;

    internal SocketAcceptPhaseObserver()
    {
        observed = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        Volatile.Write(ref initialized, true);
        var source = Volatile.Read(ref socketSource);
        if (source is not null)
        {
            EnableEvents(source, EventLevel.LogAlways, EventKeywords.All);
        }
    }

    internal Task Observed => observed?.Task
        ?? throw new InvalidOperationException("socket phase observer is not initialized");

    protected override void OnEventSourceCreated(EventSource eventSource)
    {
        if (eventSource.Name.Equals("System.Net.Sockets", StringComparison.Ordinal))
        {
            Volatile.Write(ref socketSource, eventSource);
            if (Volatile.Read(ref initialized))
            {
                EnableEvents(eventSource, EventLevel.LogAlways, EventKeywords.All);
            }
        }
    }

    protected override void OnEventWritten(EventWrittenEventArgs eventData)
    {
        if (eventData.EventName?.Equals("AcceptStop", StringComparison.Ordinal) == true)
        {
            Volatile.Read(ref observed)?.TrySetResult();
        }
    }
}

internal sealed class AcceptStartCancellationObserver : EventListener
{
    private EventSource? socketSource;
    private CancellationTokenSource? cancellation;
    private TaskCompletionSource? observed;
    private bool initialized;

    internal AcceptStartCancellationObserver(CancellationTokenSource cancellation)
    {
        this.cancellation = cancellation;
        observed = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        Volatile.Write(ref initialized, true);
        var source = Volatile.Read(ref socketSource);
        if (source is not null)
        {
            EnableEvents(source, EventLevel.LogAlways, EventKeywords.All);
        }
    }

    internal Task Observed => observed?.Task
        ?? throw new InvalidOperationException("accept-start observer is not initialized");

    protected override void OnEventSourceCreated(EventSource eventSource)
    {
        if (eventSource.Name.Equals("System.Net.Sockets", StringComparison.Ordinal))
        {
            Volatile.Write(ref socketSource, eventSource);
            if (Volatile.Read(ref initialized))
            {
                EnableEvents(eventSource, EventLevel.LogAlways, EventKeywords.All);
            }
        }
    }

    protected override void OnEventWritten(EventWrittenEventArgs eventData)
    {
        if (eventData.EventName?.Equals("AcceptStart", StringComparison.Ordinal) == true)
        {
            Volatile.Read(ref observed)?.TrySetResult();
            Volatile.Read(ref cancellation)?.Cancel();
        }
    }
}
