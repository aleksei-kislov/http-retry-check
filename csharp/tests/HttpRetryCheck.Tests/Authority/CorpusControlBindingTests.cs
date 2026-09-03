using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Http;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.Tests.Runtime;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using HttpRetryCheck.V1.Testing;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Authority;

[TestClass]
public sealed class CorpusControlBindingTests
{
    private const string CorpusMarker = "HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9";

    [TestMethod]
    public async Task EachCSharpSanitationVectorRunsItsMarkerControl()
    {
        using JsonDocument document = LoadBundle("sanitation.json");
        JsonElement root = document.RootElement;
        Assert.AreEqual("http_retry_check.conformance_invalid.v1", root.GetProperty("schema_version").GetString());
        Assert.AreEqual("sanitation", root.GetProperty("category").GetString());
        Assert.AreEqual(CorpusMarker, root.GetProperty("marker").GetString());

        var expectedTargets = new Dictionary<string, string>(StringComparer.Ordinal)
        {
            ["sanitation_callback_error_value"] = "callback_error_value",
            ["sanitation_callback_error_type"] = "callback_error_type",
            ["sanitation_response_reason"] = "response_reason",
            ["sanitation_response_header"] = "response_header",
            ["sanitation_response_content"] = "response_content",
            ["sanitation_request_url"] = "request_url",
            ["sanitation_request_header"] = "request_header",
            ["sanitation_request_body"] = "request_body",
            ["sanitation_replay_behavior"] = "replay_behavior",
            ["sanitation_sender_type"] = "client_type",
            ["sanitation_sender_formatter"] = "client_formatter",
            ["sanitation_environment_proxy"] = "environment_proxy",
            ["sanitation_environment_credential"] = "environment_credential",
        };
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (JsonElement vector in root.GetProperty("cases").EnumerateArray())
        {
            if (!RepresentableIn(vector, "csharp"))
            {
                continue;
            }

            string id = RequiredString(vector, "id");
            Assert.IsTrue(seen.Add(id), $"Duplicate C# sanitation vector: {id}");
            Assert.IsTrue(expectedTargets.TryGetValue(id, out string? target), id);
            Assert.AreEqual("marker_absent", RequiredString(vector, "expected"), id);
            Assert.AreEqual("inject_marker", RequiredString(vector, "operation"), id);
            Assert.AreEqual(target, RequiredString(vector, "target"), id);
            Assert.AreEqual(CorpusMarker, RequiredString(vector, "operand"), id);

            await RunSanitationControlAsync(id).ConfigureAwait(false);
        }

        CollectionAssert.AreEquivalent(expectedTargets.Keys.ToArray(), seen.ToArray());
    }

    [TestMethod]
    public async Task EachCSharpBoundaryVectorRunsItsControl()
    {
        using JsonDocument document = LoadBundle("authority.json");
        JsonElement root = document.RootElement;
        Assert.AreEqual("http_retry_check.conformance_invalid.v1", root.GetProperty("schema_version").GetString());
        Assert.AreEqual("authority", root.GetProperty("category").GetString());

        var expected = new Dictionary<string, (string Operation, string Target, string Result)>(StringComparer.Ordinal)
        {
            ["authority_constructor_aliasing"] = ("native_alias_probe", "/scenarios", "detached"),
            ["authority_native_api_surface"] = ("source_surface", "HttpRetryCheck.V1", "exact_surface"),
            ["authority_no_cross_binding_import"] = ("source_import", "other_binding", "forbidden_absent"),
            ["authority_runtime_literal_loopback"] = ("native_runtime", "literal_127_0_0_1", "native_control"),
            ["authority_runtime_no_ambient_proxy"] = ("native_runtime", "proxy_free_http1", "native_control"),
            ["authority_runtime_same_instance_order"] = ("native_runtime", "same_instance_canonical_order", "native_control"),
            ["authority_runtime_concurrent_independence"] = ("native_runtime", "concurrent_independence", "native_control"),
        };
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (JsonElement vector in root.GetProperty("cases").EnumerateArray())
        {
            if (!RepresentableIn(vector, "csharp"))
            {
                continue;
            }

            string id = RequiredString(vector, "id");
            Assert.IsTrue(seen.Add(id), $"Duplicate C# authority vector: {id}");
            Assert.IsTrue(expected.TryGetValue(id, out var identity), id);
            Assert.AreEqual(identity.Result, RequiredString(vector, "expected"), id);
            Assert.AreEqual(identity.Operation, RequiredString(vector, "operation"), id);
            Assert.AreEqual(identity.Target, RequiredString(vector, "target"), id);

            await RunAuthorityControlAsync(id).ConfigureAwait(false);
        }

        CollectionAssert.AreEquivalent(expected.Keys.ToArray(), seen.ToArray());
    }

    private static async Task RunSanitationControlAsync(string id)
    {
        switch (id)
        {
            case "sanitation_callback_error_value":
            case "sanitation_callback_error_type":
                await AssertCallbackMarkerIsSanitizedAsync().ConfigureAwait(false);
                return;
            case "sanitation_environment_proxy":
                await AssertEnvironmentMarkerIsIgnoredAsync("HTTP_PROXY").ConfigureAwait(false);
                return;
            case "sanitation_environment_credential":
                await AssertEnvironmentMarkerIsIgnoredAsync("HTTP_RETRY_CHECK_HTTP_CREDENTIAL").ConfigureAwait(false);
                return;
            default:
                using (var handler = new HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9SenderHandler(id))
                using (var invoker = new HttpMessageInvoker(handler, disposeHandler: false))
                {
                    SuiteResult result = await ScenarioSuite.RunAsync(invoker).ConfigureAwait(false);
                    AssertMarkerAbsent(result);
                }
                return;
        }
    }

    private static async Task RunAuthorityControlAsync(string id)
    {
        switch (id)
        {
            case "authority_constructor_aliasing":
                using (var client = RuntimeTestClients.CreateOrdinaryClient())
                {
                    SuiteResult sourceResult = await ScenarioSuite.RunAsync(client).ConfigureAwait(false);
                    var supplied = new List<ScenarioResult>(sourceResult.Scenarios);
                    var detached = new SuiteResult(sourceResult.Assessment, supplied);
                    supplied.Clear();
                    Assert.HasCount(6, detached.Scenarios);
                    ScenarioSuite.Validate(detached);
                }
                return;
            case "authority_native_api_surface":
                var surface = new PublicSurfaceTests();
                surface.AssemblyExportsOnlyTheDocumentedPublicTypes();
                surface.EnumNamesOrderAndValuesMatchTheExpectedApi();
                surface.RootMembersMatchTheExpectedApi();
                surface.TestingMembersMatchTheExpectedApi();
                surface.ReportingMembersMatchTheExpectedApi();
                surface.NullableAnnotationsMatchTheExpectedApi();
                return;
            case "authority_no_cross_binding_import":
                var sourceAuthority = new SourceAuthorityTests();
                sourceAuthority.ProductionSourceInventoryMatchesPolicy();
                sourceAuthority.ProductionAssemblyUsesOnlyBclReferencesAndManagedCalls();
                return;
            case "authority_runtime_literal_loopback":
                await AssertSevenLiteralLoopbackEndpointsAsync().ConfigureAwait(false);
                return;
            case "authority_runtime_no_ambient_proxy":
                await AssertEnvironmentMarkerIsIgnoredAsync("ALL_PROXY").ConfigureAwait(false);
                return;
            case "authority_runtime_same_instance_order":
                await new ScenarioRuntimeTests()
                    .NoDispatchResponsesProduceAnOrderedInconclusiveSuite()
                    .ConfigureAwait(false);
                return;
            case "authority_runtime_concurrent_independence":
                await new ScenarioRuntimeTests().ConcurrentRunsUseIndependentOriginsAndState().ConfigureAwait(false);
                return;
            default:
                Assert.Fail($"Unbound C# authority vector: {id}");
                return;
        }
    }

    private static async Task AssertCallbackMarkerIsSanitizedAsync()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, disposeHandler: false);
        try
        {
            await ScenarioTest.CheckAsync(
                invoker,
                _ => throw new HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9CallbackException()).ConfigureAwait(false);
        }
        catch (ScenarioAssertionException exception)
        {
            Assert.AreEqual("HTTP Retry Check scenario diagnostics could not be written.", exception.Message);
            Assert.IsFalse(exception.ToString().Contains(CorpusMarker, StringComparison.Ordinal));
            Assert.IsNull(exception.InnerException);
            Assert.IsNull(exception.StackTrace);
            return;
        }

        Assert.Fail("The marker callback did not produce the fixed assertion failure.");
    }

    private static async Task AssertEnvironmentMarkerIsIgnoredAsync(string variable)
    {
        string? prior = Environment.GetEnvironmentVariable(variable);
        try
        {
            Environment.SetEnvironmentVariable(variable, CorpusMarker);
            using var client = RuntimeTestClients.CreateOrdinaryClient();
            SuiteResult result = await ScenarioSuite.RunAsync(client).ConfigureAwait(false);
            Assert.AreEqual(Assessment.NoUnsafeBehaviorObserved, result.Assessment);
            AssertMarkerAbsent(result);
        }
        finally
        {
            Environment.SetEnvironmentVariable(variable, prior);
        }
    }

    private static async Task AssertSevenLiteralLoopbackEndpointsAsync()
    {
        using var handler = new RecordingRedirectHandler();
        using var client = RuntimeTestClients.CreateOrdinaryClient(handler);
        SuiteResult result = await ScenarioSuite.RunAsync(client).ConfigureAwait(false);
        ScenarioSuite.Validate(result);

        Uri[] targets = handler.Targets.ToArray();
        Assert.HasCount(7, targets);
        Assert.AreEqual(7, targets.Select(static target => target.Port).Distinct().Count());
        foreach (Uri target in targets)
        {
            Assert.AreEqual("http", target.Scheme);
            Assert.AreEqual("127.0.0.1", target.Host);
            Assert.AreEqual("/case", target.AbsolutePath);
            Assert.IsTrue(target.Port is > 0 and <= ushort.MaxValue);
        }
    }

    private static void AssertMarkerAbsent(SuiteResult result)
    {
        ScenarioSuite.Validate(result);
        Report report = ScenarioReports.Create(result);
        ScenarioReports.Validate(report);
        var values = new List<string>
        {
            result.ToString() ?? string.Empty,
            report.ToString() ?? string.Empty,
            Encoding.UTF8.GetString(ScenarioReports.Encode(report)),
            Encoding.UTF8.GetString(ScenarioReports.JUnit(report)),
            Encoding.UTF8.GetString(ScenarioReports.GitHubSummary(report)),
        };
        foreach (ScenarioResult row in result.Scenarios)
        {
            values.Add(row.ToString() ?? string.Empty);
            values.Add(row.Observation.ToString() ?? string.Empty);
            values.Add(ScenarioExplanations.ScenarioText(row.Scenario));
            values.Add(ScenarioExplanations.AssessmentText(row.Assessment));
            foreach (FindingCode finding in row.Findings)
            {
                values.Add(ScenarioExplanations.FindingText(finding));
            }
        }
        foreach (ArtifactFile file in ScenarioReports.BuildArtifact(report))
        {
            values.Add(file.Name);
            values.Add(file.MediaType);
            values.Add(Encoding.UTF8.GetString(file.Contents.Span));
        }

        Assert.IsFalse(string.Join('\n', values).Contains(CorpusMarker, StringComparison.Ordinal));
    }

    private static JsonDocument LoadBundle(string name)
    {
        string? directory = AppContext.BaseDirectory;
        while (directory is not null)
        {
            string candidate = Path.Combine(
                directory,
                "conformance",
                "http-retry-check",
                "v1",
                "invalid",
                name);
            if (File.Exists(candidate))
            {
                return JsonDocument.Parse(File.ReadAllBytes(candidate));
            }

            directory = Directory.GetParent(directory)?.FullName;
        }

        throw new AssertFailedException($"Corpus bundle is unavailable: {name}");
    }

    private static bool RepresentableIn(JsonElement vector, string binding)
    {
        return vector.GetProperty("representability")
            .EnumerateArray()
            .Any(item => item.GetString() == binding);
    }

    private static string RequiredString(JsonElement value, string property)
    {
        return value.GetProperty(property).GetString()
            ?? throw new AssertFailedException($"Corpus member is not a string: {property}");
    }
}

internal sealed class HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9CallbackException : Exception
{
    internal HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9CallbackException()
        : base("HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9")
    {
    }
}

internal sealed class HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9SenderHandler : HttpMessageHandler
{
    private readonly string vector;

    internal HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9SenderHandler(string vector)
    {
        this.vector = vector;
    }

    public override string ToString() => "HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9";

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        switch (vector)
        {
            case "sanitation_response_reason":
            case "sanitation_response_header":
            case "sanitation_response_content":
                var response = new HttpResponseMessage(HttpStatusCode.NoContent)
                {
                    ReasonPhrase = CorpusControlBindingTestsMarker.Value,
                    Content = new StringContent(CorpusControlBindingTestsMarker.Value),
                };
                response.Headers.TryAddWithoutValidation("X-Corpus-Marker", CorpusControlBindingTestsMarker.Value);
                return response;
            case "sanitation_request_url":
                request.RequestUri = new Uri(
                    "http://127.0.0.1/HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9",
                    UriKind.Absolute);
                return new HttpResponseMessage(HttpStatusCode.NoContent);
            case "sanitation_request_header":
                request.Headers.TryAddWithoutValidation("X-Corpus-Marker", CorpusControlBindingTestsMarker.Value);
                return new HttpResponseMessage(HttpStatusCode.NoContent);
            case "sanitation_request_body":
                request.Content = new StringContent(CorpusControlBindingTestsMarker.Value);
                return new HttpResponseMessage(HttpStatusCode.NoContent);
            case "sanitation_replay_behavior":
                request.Content = new CorpusMarkerContent();
                await request.Content.CopyToAsync(Stream.Null, cancellationToken).ConfigureAwait(false);
                return new HttpResponseMessage(HttpStatusCode.NoContent);
            case "sanitation_sender_type":
            case "sanitation_sender_formatter":
                return new HttpResponseMessage(HttpStatusCode.NoContent);
            default:
                throw new InvalidOperationException("unknown sanitation corpus vector");
        }
    }

    private sealed class CorpusMarkerContent : HttpContent
    {
        protected override Task SerializeToStreamAsync(Stream stream, TransportContext? context)
        {
            byte[] bytes = Encoding.UTF8.GetBytes(CorpusControlBindingTestsMarker.Value);
            return stream.WriteAsync(bytes, 0, bytes.Length);
        }

        protected override bool TryComputeLength(out long length)
        {
            length = Encoding.UTF8.GetByteCount(CorpusControlBindingTestsMarker.Value);
            return true;
        }

        public override string ToString() => CorpusControlBindingTestsMarker.Value;
    }
}

internal static class CorpusControlBindingTestsMarker
{
    internal const string Value = "HTTP_RETRY_CHECK_CORPUS_MARKER_7Q4VX9";
}

internal sealed class RecordingRedirectHandler : DelegatingHandler
{
    private readonly List<Uri> targets = new();

    internal RecordingRedirectHandler()
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
        if (request.RequestUri is not null)
        {
            targets.Add(request.RequestUri);
        }

        HttpResponseMessage response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
        if (response.StatusCode != HttpStatusCode.TemporaryRedirect || response.Headers.Location is null)
        {
            return response;
        }

        Uri redirectTarget = response.Headers.Location;
        response.Dispose();
        targets.Add(redirectTarget);
        using var body = new MemoryStream();
        await request.Content!.CopyToAsync(body, cancellationToken).ConfigureAwait(false);
        using var redirected = new HttpRequestMessage(HttpMethod.Post, redirectTarget)
        {
            Version = HttpVersion.Version11,
            VersionPolicy = HttpVersionPolicy.RequestVersionExact,
            Content = new ByteArrayContent(body.ToArray()),
        };
        return await base.SendAsync(redirected, cancellationToken).ConfigureAwait(false);
    }
}
