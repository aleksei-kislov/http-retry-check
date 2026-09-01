using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Runtime;

[TestClass]
public sealed class Http1ChunkedCaptureTests
{
    [TestMethod]
    public async Task ValidMultiChunkBodyPassesTheRetryLimitScenario()
    {
        var result = await RunAsync(ChunkedControlMode.ValidMultiChunk);

        AssertPassingRetryLimitRow(result);
    }

    [TestMethod]
    public async Task ChunkExtensionsAndTrailersPassTheRetryLimitScenario()
    {
        var result = await RunAsync(ChunkedControlMode.ExtensionsAndTrailers);

        AssertPassingRetryLimitRow(result);
    }

    [TestMethod]
    public async Task QuotedAndEscapedChunkExtensionsPassTheRetryLimitScenario()
    {
        var result = await RunAsync(ChunkedControlMode.QuotedEscapedExtension);

        AssertPassingRetryLimitRow(result);
    }

    [TestMethod]
    public async Task MalformedChunkTerminatorIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.MalformedTerminator);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task MalformedChunkSizeIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.MalformedSize);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task OversizedChunkFramingIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.OversizedFraming);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task EarlyCloseDuringChunkDataIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.EarlyClose);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task DeleteControlInTrailerValueIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.TrailerDeleteControl);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task BareChunkExtensionIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.BareExtension);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task MalformedChunkExtensionNameIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.MalformedExtensionName);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task DeleteControlInChunkExtensionIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.ExtensionDeleteControl);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task UnclosedQuotedChunkExtensionIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.UnclosedQuotedExtension);

        AssertIncompleteRetryLimitRow(result);
    }

    [TestMethod]
    public async Task EmptyTokenChunkExtensionValueIsInconclusive()
    {
        var result = await RunAsync(ChunkedControlMode.EmptyTokenExtensionValue);

        AssertIncompleteRetryLimitRow(result);
    }

    private static async Task<SuiteResult> RunAsync(ChunkedControlMode mode)
    {
        using var handler = new ChunkedControlHandler(mode);
        using var invoker = new HttpMessageInvoker(handler, disposeHandler: false);
        var result = await ScenarioSuite.RunAsync(invoker).ConfigureAwait(false);
        ScenarioSuite.Validate(result);
        return result;
    }

    private static void AssertPassingRetryLimitRow(SuiteResult result)
    {
        var row = result.Scenarios[4];
        Assert.AreEqual(ScenarioId.RetryLimit, row.Scenario);
        Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, row.Assessment, Describe(row));
        Assert.IsTrue(row.Observation.CaptureComplete, Describe(row));
        Assert.IsTrue(row.Observation.BodyConsistent, Describe(row));
        Assert.AreEqual((uint)1, row.Observation.AttemptCount);
        Assert.AreEqual((uint)1, row.Observation.ResponseAttemptCount);
        Assert.AreEqual((uint)1, row.Observation.ResponseCompleteCount);
        Assert.HasCount(0, row.Findings);
    }

    private static void AssertIncompleteRetryLimitRow(SuiteResult result)
    {
        var row = result.Scenarios[4];
        Assert.AreEqual(ScenarioId.RetryLimit, row.Scenario);
        Assert.AreEqual(Assessment.Inconclusive, row.Assessment, Describe(row));
        Assert.IsFalse(row.Observation.CaptureComplete, Describe(row));
        Assert.AreEqual((uint)1, row.Observation.AttemptCount);
        Assert.AreEqual((uint)0, row.Observation.ResponseAttemptCount);
        CollectionAssert.Contains(new List<FindingCode>(row.Findings), FindingCode.CaptureIncomplete);
        CollectionAssert.Contains(new List<FindingCode>(row.Findings), FindingCode.ResponseIncomplete);
    }

    private static string Describe(ScenarioResult row)
    {
        return string.Join(
            ":",
            row.Scenario,
            row.Assessment,
            row.Observation.CaptureComplete,
            row.Observation.AttemptCount,
            row.Observation.ResponseAttemptCount,
            string.Join(",", row.Findings));
    }
}

internal enum ChunkedControlMode
{
    ValidMultiChunk,
    ExtensionsAndTrailers,
    QuotedEscapedExtension,
    MalformedTerminator,
    MalformedSize,
    OversizedFraming,
    EarlyClose,
    TrailerDeleteControl,
    BareExtension,
    MalformedExtensionName,
    ExtensionDeleteControl,
    UnclosedQuotedExtension,
    EmptyTokenExtensionValue,
}

internal sealed class ChunkedControlHandler : HttpMessageHandler
{
    private const int RetryLimitCall = 5;
    private const int WireLimit = (1 << 20) + 1;
    private readonly ChunkedControlMode mode;
    private int calls;

    internal ChunkedControlHandler(ChunkedControlMode mode)
    {
        this.mode = mode;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        calls++;
        if (calls != RetryLimitCall)
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
        var body = await request.Content!.ReadAsByteArrayAsync(cancellationToken).ConfigureAwait(false);
        byte[] wire = mode switch
        {
            ChunkedControlMode.ValidMultiChunk => BuildValidMultiChunk(target, credential, body),
            ChunkedControlMode.ExtensionsAndTrailers => BuildExtensionsAndTrailers(target, credential, body),
            ChunkedControlMode.QuotedEscapedExtension => BuildChunkExtensionRequest(
                target,
                credential,
                body,
                " \t; \tlabel \t= \t\"one\\\"two\\\\three\";empty=\"\";flag"),
            ChunkedControlMode.MalformedTerminator => BuildMalformedTerminator(target, credential, body),
            ChunkedControlMode.MalformedSize => BuildMalformedSize(target, credential, body),
            ChunkedControlMode.OversizedFraming => BuildOversizedFraming(target, credential),
            ChunkedControlMode.EarlyClose => BuildEarlyClose(target, credential, body),
            ChunkedControlMode.TrailerDeleteControl => BuildTrailerDeleteControl(
                target,
                credential,
                body),
            ChunkedControlMode.BareExtension => BuildChunkExtensionRequest(
                target,
                credential,
                body,
                ";"),
            ChunkedControlMode.MalformedExtensionName => BuildChunkExtensionRequest(
                target,
                credential,
                body,
                ";bad/name=value"),
            ChunkedControlMode.ExtensionDeleteControl => BuildChunkExtensionRequest(
                target,
                credential,
                body,
                ";name=value\u007f"),
            ChunkedControlMode.UnclosedQuotedExtension => BuildChunkExtensionRequest(
                target,
                credential,
                body,
                ";name=\"unterminated"),
            ChunkedControlMode.EmptyTokenExtensionValue => BuildChunkExtensionRequest(
                target,
                credential,
                body,
                ";name="),
            _ => throw new InvalidOperationException("unknown chunked control mode"),
        };

        try
        {
            await SendRawAsync(
                target,
                wire,
                allowEarlyClose: mode is not ChunkedControlMode.ValidMultiChunk and
                    not ChunkedControlMode.ExtensionsAndTrailers and
                    not ChunkedControlMode.QuotedEscapedExtension,
                cancellationToken).ConfigureAwait(false);
        }
        finally
        {
            Array.Clear(body);
            Array.Clear(wire);
        }

        return new HttpResponseMessage(HttpStatusCode.NoContent);
    }

    private static byte[] BuildValidMultiChunk(Uri target, string credential, byte[] body)
    {
        using var wire = NewChunkedRequest(target, credential);
        var first = body.Length / 3;
        var second = (body.Length - first) / 2;
        WriteChunk(wire, body.AsSpan(0, first), extension: null);
        WriteChunk(wire, body.AsSpan(first, second), extension: null);
        WriteChunk(wire, body.AsSpan(first + second), extension: null);
        WriteAscii(wire, "0\r\n\r\n");
        return wire.ToArray();
    }

    private static byte[] BuildExtensionsAndTrailers(Uri target, string credential, byte[] body)
    {
        using var wire = NewChunkedRequest(target, credential);
        var split = body.Length / 2;
        WriteChunk(wire, body.AsSpan(0, split), ";part=first");
        WriteChunk(wire, body.AsSpan(split), ";part=second;flag");
        WriteAscii(wire, "0;done=true\r\nX-HTTP-Retry-Check: complete\r\nX-Second: value\r\n\r\n");
        return wire.ToArray();
    }

    private static byte[] BuildMalformedTerminator(Uri target, string credential, byte[] body)
    {
        using var wire = NewChunkedRequest(target, credential);
        WriteAscii(wire, body.Length.ToString("x", CultureInfo.InvariantCulture));
        WriteAscii(wire, "\r\n");
        wire.Write(body);
        WriteAscii(wire, "\n0\r\n\r\n");
        return wire.ToArray();
    }

    private static byte[] BuildMalformedSize(Uri target, string credential, byte[] body)
    {
        using var wire = NewChunkedRequest(target, credential);
        WriteAscii(wire, "z\r\n");
        wire.Write(body);
        WriteAscii(wire, "\r\n0\r\n\r\n");
        return wire.ToArray();
    }

    private static byte[] BuildOversizedFraming(Uri target, string credential)
    {
        using var wire = NewChunkedRequest(target, credential);
        WriteAscii(wire, "1;");
        var extension = new byte[WireLimit + 32];
        try
        {
            Array.Fill(extension, (byte)'a');
            wire.Write(extension);
        }
        finally
        {
            Array.Clear(extension);
        }

        return wire.ToArray();
    }

    private static byte[] BuildEarlyClose(Uri target, string credential, byte[] body)
    {
        using var wire = NewChunkedRequest(target, credential);
        WriteAscii(wire, body.Length.ToString("x", CultureInfo.InvariantCulture));
        WriteAscii(wire, "\r\n");
        wire.Write(body.AsSpan(0, body.Length / 2));
        return wire.ToArray();
    }

    private static byte[] BuildTrailerDeleteControl(Uri target, string credential, byte[] body)
    {
        using var wire = NewChunkedRequest(target, credential);
        WriteChunk(wire, body, extension: null);
        WriteAscii(wire, "0\r\nX-Trailer: value\u007f\r\n\r\n");
        return wire.ToArray();
    }

    private static byte[] BuildChunkExtensionRequest(
        Uri target,
        string credential,
        byte[] body,
        string extension)
    {
        using var wire = NewChunkedRequest(target, credential);
        WriteChunk(wire, body, extension);
        WriteAscii(wire, "0\r\n\r\n");
        return wire.ToArray();
    }

    private static MemoryStream NewChunkedRequest(Uri target, string credential)
    {
        var wire = new MemoryStream();
        WriteAscii(
            wire,
            $"POST {target.PathAndQuery} HTTP/1.1\r\n" +
            $"Host: {target.Host}:{target.Port}\r\n" +
            $"Authorization: {credential}\r\n" +
            "Transfer-Encoding: chunked\r\n" +
            "Connection: close\r\n\r\n");
        return wire;
    }

    private static void WriteChunk(MemoryStream wire, ReadOnlySpan<byte> body, string? extension)
    {
        WriteAscii(wire, body.Length.ToString("x", CultureInfo.InvariantCulture));
        if (extension is not null)
        {
            WriteAscii(wire, extension);
        }

        WriteAscii(wire, "\r\n");
        wire.Write(body);
        WriteAscii(wire, "\r\n");
    }

    private static void WriteAscii(MemoryStream wire, string value)
    {
        var encoded = Encoding.ASCII.GetBytes(value);
        try
        {
            wire.Write(encoded);
        }
        finally
        {
            Array.Clear(encoded);
        }
    }

    private static async Task SendRawAsync(
        Uri target,
        byte[] wire,
        bool allowEarlyClose,
        CancellationToken cancellationToken)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(TimeSpan.FromSeconds(4));
        using var socket = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
        await socket.ConnectAsync(
            new IPEndPoint(IPAddress.Loopback, target.Port),
            timeout.Token).ConfigureAwait(false);

        try
        {
            var offset = 0;
            while (offset < wire.Length)
            {
                var sent = await socket.SendAsync(
                    wire.AsMemory(offset),
                    SocketFlags.None,
                    timeout.Token).ConfigureAwait(false);
                if (sent == 0)
                {
                    break;
                }

                offset += sent;
            }

            socket.Shutdown(SocketShutdown.Send);
        }
        catch (SocketException) when (allowEarlyClose)
        {
            // Invalid requests may be rejected before the sender finishes writing.
        }

        var buffer = new byte[1024];
        try
        {
            while (await socket.ReceiveAsync(
                       buffer.AsMemory(),
                       SocketFlags.None,
                       timeout.Token).ConfigureAwait(false) != 0)
            {
            }
        }
        catch (SocketException) when (allowEarlyClose)
        {
            // Invalid requests may be closed without a response.
        }
        finally
        {
            Array.Clear(buffer);
        }
    }
}
