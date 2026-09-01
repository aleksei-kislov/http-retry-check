using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Sockets;
using System.Reflection;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Runtime;

[TestClass]
public sealed class Http1CaptureCorpusTests
{
    private const string CorpusRelativePath = "capture/request-wires.json";

    [TestMethod]
    public async Task EverySharedRequestWireMatchesTheNativeCapture()
    {
        byte[] corpusBytes = ReadCorpusFile(CorpusRelativePath);
        using JsonDocument document = JsonDocument.Parse(corpusBytes);
        JsonElement root = document.RootElement;
        CollectionAssert.AreEqual(
            new[] { "schema_version", "endpoint", "cases" },
            root.EnumerateObject().Select(static property => property.Name).ToArray());
        Assert.AreEqual(
            "http_retry_check.capture_corpus.v1",
            root.GetProperty("schema_version").GetString());
        Assert.AreEqual("127.0.0.1:41001", root.GetProperty("endpoint").GetString());
        var expectedEndpoint = IPEndPoint.Parse(root.GetProperty("endpoint").GetString()!);
        JsonElement[] cases = root.GetProperty("cases").EnumerateArray().ToArray();
        Assert.HasCount(53, cases);
        var ids = new HashSet<string>(StringComparer.Ordinal);

        foreach (JsonElement item in cases)
        {
            CollectionAssert.AreEqual(
                new[] { "id", "wire_base64", "expected" },
                item.EnumerateObject().Select(static property => property.Name).ToArray());
            string id = item.GetProperty("id").GetString()
                ?? throw new AssertFailedException("Capture case id is missing.");
            Assert.IsTrue(id.Length != 0 && ids.Add(id), $"Duplicate or empty capture case id: {id}");
            string encoded = item.GetProperty("wire_base64").GetString()
                ?? throw new AssertFailedException($"Capture wire is missing: {id}");
            byte[] wire = Convert.FromBase64String(encoded);
            try
            {
                Assert.AreEqual(encoded, Convert.ToBase64String(wire), $"Non-canonical base64: {id}");
                CaptureView actual = await CaptureAsync(wire, expectedEndpoint).ConfigureAwait(false);
                AssertCapture(id, item.GetProperty("expected"), actual);
            }
            finally
            {
                Array.Clear(wire);
            }
        }
    }

    [TestMethod]
    public void SharedRequestWireCorpusIsBoundByTheRootManifest()
    {
        byte[] corpusBytes = ReadCorpusFile(CorpusRelativePath);
        byte[] manifestBytes = ReadCorpusFile("manifest.json");
        try
        {
            using JsonDocument manifest = JsonDocument.Parse(manifestBytes);
            JsonElement[] entries = manifest.RootElement
                .GetProperty("files")
                .EnumerateArray()
                .Where(item => item.GetProperty("path").GetString() == CorpusRelativePath)
                .ToArray();
            Assert.HasCount(1, entries);
            JsonElement entry = entries[0];
            Assert.AreEqual(corpusBytes.Length, entry.GetProperty("size").GetInt32());
            string actualHash = Convert.ToHexString(SHA256.HashData(corpusBytes)).ToLowerInvariant();
            Assert.AreEqual(entry.GetProperty("sha256").GetString(), actualHash);
        }
        finally
        {
            Array.Clear(corpusBytes);
            Array.Clear(manifestBytes);
        }
    }

    [TestMethod]
    public async Task RecoverableReadCancellationPreservesTheAccumulatedMarker()
    {
        byte[] partialHead = Encoding.ASCII.GetBytes(
            "http-retry-check-synthetic-scenario-suite-v1");
        try
        {
            CaptureView result = await CaptureQueuedBytesThenCancelAsync(partialHead).ConfigureAwait(false);
            Assert.IsFalse(result.HeadersObserved);
            Assert.IsFalse(result.Complete);
            Assert.IsFalse(result.CaptureComplete);
            Assert.IsTrue(result.CredentialExposed);
        }
        finally
        {
            Array.Clear(partialHead);
        }
    }

    [TestMethod]
    public async Task RecoverableBodyCancellationPreservesAChangedByte()
    {
        byte[] partialRequest = Encoding.ASCII.GetBytes(
            "POST /case HTTP/1.1\r\n" +
            "Host: 127.0.0.1:41001\r\n" +
            "Authorization: Bearer http-retry-check-synthetic-scenario-suite-v1\r\n" +
            "Content-Length: 47\r\n\r\n" +
            "X");
        try
        {
            CaptureView result = await CaptureQueuedBytesThenCancelAsync(partialRequest).ConfigureAwait(false);
            Assert.IsTrue(result.HeadersObserved);
            Assert.IsFalse(result.Complete);
            Assert.IsFalse(result.CaptureComplete);
            Assert.IsTrue(result.MethodConsistent);
            Assert.IsTrue(result.DestinationConsistent);
            Assert.IsFalse(result.BodyConsistent);
            Assert.IsTrue(result.CredentialExact);
            Assert.IsTrue(result.CredentialExposed);
        }
        finally
        {
            Array.Clear(partialRequest);
        }
    }

    private static async Task<CaptureView> CaptureQueuedBytesThenCancelAsync(byte[] partialWire)
    {
        using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(3));
        using var captureCancellation = new CancellationTokenSource();
        var listener = new TcpListener(IPAddress.Loopback, 0);
        listener.Start(1);
        try
        {
            Task<Socket> accepted = listener.AcceptSocketAsync(timeout.Token).AsTask();
            using var client = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
            await client.ConnectAsync(
                (IPEndPoint)listener.LocalEndpoint,
                timeout.Token).ConfigureAwait(false);
            using Socket server = await accepted.ConfigureAwait(false);
            await SendAllAsync(client, partialWire, timeout.Token).ConfigureAwait(false);
            await WaitForAvailableAsync(
                server,
                count => count >= partialWire.Length,
                timeout.Token).ConfigureAwait(false);
            Task capture = InvokeCapture(
                server,
                IPEndPoint.Parse("127.0.0.1:41001"),
                captureCancellation.Token);
            await WaitForAvailableAsync(
                server,
                count => count == 0,
                timeout.Token).ConfigureAwait(false);
            captureCancellation.Cancel();
            await capture.WaitAsync(timeout.Token).ConfigureAwait(false);
            return ReadCapture(capture);
        }
        finally
        {
            listener.Stop();
        }
    }

    private static async Task WaitForAvailableAsync(
        Socket socket,
        Func<int, bool> condition,
        CancellationToken cancellationToken)
    {
        while (!condition(socket.Available))
        {
            cancellationToken.ThrowIfCancellationRequested();
            await Task.Yield();
        }
    }

    private static async Task<CaptureView> CaptureAsync(byte[] wire, IPEndPoint expectedEndpoint)
    {
        using var cancellation = new CancellationTokenSource(TimeSpan.FromSeconds(3));
        var listener = new TcpListener(IPAddress.Loopback, 0);
        listener.Start(1);
        try
        {
            Task<Socket> accepted = listener.AcceptSocketAsync(cancellation.Token).AsTask();
            using var client = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
            await client.ConnectAsync(
                (IPEndPoint)listener.LocalEndpoint,
                cancellation.Token).ConfigureAwait(false);
            using Socket server = await accepted.ConfigureAwait(false);
            Task capture = InvokeCapture(server, expectedEndpoint, cancellation.Token);
            await SendAllAsync(client, wire, cancellation.Token).ConfigureAwait(false);

            client.Shutdown(SocketShutdown.Send);
            await capture.WaitAsync(cancellation.Token).ConfigureAwait(false);
            return ReadCapture(capture);
        }
        finally
        {
            listener.Stop();
        }
    }

    private static async Task SendAllAsync(
        Socket socket,
        byte[] wire,
        CancellationToken cancellationToken)
    {
        var offset = 0;
        while (offset < wire.Length)
        {
            var sent = await socket.SendAsync(
                wire.AsMemory(offset),
                SocketFlags.None,
                cancellationToken).ConfigureAwait(false);
            Assert.IsTrue(sent != 0, "Capture fixture socket stopped accepting bytes.");
            offset += sent;
        }
    }

    private static CaptureView ReadCapture(Task capture)
    {
        object boxed = capture.GetType().GetProperty("Result")?.GetValue(capture)
            ?? throw new AssertFailedException("Native capture returned no result.");
        return new CaptureView(
            ReadBoolean(boxed, "HeadersObserved"),
            ReadBoolean(boxed, "Complete"),
            ReadBoolean(boxed, "CaptureComplete"),
            ReadBoolean(boxed, "MethodConsistent"),
            ReadBoolean(boxed, "DestinationConsistent"),
            ReadBoolean(boxed, "BodyConsistent"),
            ReadBoolean(boxed, "CredentialExact"),
            ReadBoolean(boxed, "CredentialExposed"));
    }

    private static Task InvokeCapture(
        Socket socket,
        IPEndPoint expectedEndpoint,
        CancellationToken cancellationToken)
    {
        Type type = typeof(ScenarioSuite).Assembly.GetType(
            "HttpRetryCheck.V1.Runtime.Http1Capture",
            throwOnError: true) ?? throw new AssertFailedException("Native capture type is unavailable.");
        MethodInfo method = type.GetMethod(
            "ReadAsync",
            BindingFlags.Static | BindingFlags.NonPublic,
            binder: null,
            [typeof(Socket), typeof(IPEndPoint), typeof(CancellationToken)],
            modifiers: null) ?? throw new AssertFailedException("Native capture entry is unavailable.");
        return method.Invoke(null, [socket, expectedEndpoint, cancellationToken]) as Task
            ?? throw new AssertFailedException("Native capture entry returned no task.");
    }

    private static bool ReadBoolean(object source, string propertyName)
    {
        return source.GetType().GetProperty(
                propertyName,
                BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic)?.GetValue(source) as bool?
            ?? throw new AssertFailedException($"Native capture property is unavailable: {propertyName}");
    }

    private static void AssertCapture(string id, JsonElement expected, CaptureView actual)
    {
        CollectionAssert.AreEqual(
            new[]
            {
                "headers_observed",
                "complete",
                "capture_complete",
                "method_consistent",
                "destination_consistent",
                "body_consistent",
                "credential_exact",
                "credential_exposed",
            },
            expected.EnumerateObject().Select(static property => property.Name).ToArray());
        Assert.AreEqual(expected.GetProperty("headers_observed").GetBoolean(), actual.HeadersObserved, id);
        Assert.AreEqual(expected.GetProperty("complete").GetBoolean(), actual.Complete, id);
        Assert.AreEqual(expected.GetProperty("capture_complete").GetBoolean(), actual.CaptureComplete, id);
        Assert.AreEqual(expected.GetProperty("method_consistent").GetBoolean(), actual.MethodConsistent, id);
        Assert.AreEqual(expected.GetProperty("destination_consistent").GetBoolean(), actual.DestinationConsistent, id);
        Assert.AreEqual(expected.GetProperty("body_consistent").GetBoolean(), actual.BodyConsistent, id);
        Assert.AreEqual(expected.GetProperty("credential_exact").GetBoolean(), actual.CredentialExact, id);
        Assert.AreEqual(expected.GetProperty("credential_exposed").GetBoolean(), actual.CredentialExposed, id);
    }

    private static byte[] ReadCorpusFile(string relativePath)
    {
        DirectoryInfo? directory = new(AppContext.BaseDirectory);
        while (directory is not null)
        {
            string corpusRoot = Path.Combine(
                directory.FullName,
                "conformance",
                "http-retry-check",
                "v1");
            if (File.Exists(Path.Combine(corpusRoot, "manifest.json")))
            {
                return File.ReadAllBytes(Path.Combine(
                    corpusRoot,
                    relativePath.Replace('/', Path.DirectorySeparatorChar)));
            }

            directory = directory.Parent;
        }

        throw new AssertFailedException("Capture corpus root is unavailable.");
    }

    private readonly record struct CaptureView(
        bool HeadersObserved,
        bool Complete,
        bool CaptureComplete,
        bool MethodConsistent,
        bool DestinationConsistent,
        bool BodyConsistent,
        bool CredentialExact,
        bool CredentialExposed);
}
