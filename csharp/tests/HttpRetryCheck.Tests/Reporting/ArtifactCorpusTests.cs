using System;
using System.Buffers;
using System.Collections.Generic;
using System.Linq;
using System.Runtime.CompilerServices;
using System.Text;
using System.Text.Json;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Reporting;

[TestClass]
public sealed class ArtifactCorpusTests
{
    [TestMethod]
    public void ArtifactVectorsAreRejectedOrProveIndependentValues()
    {
        var report = ScenarioReports.Decode(
            ReportingCorpusTestSupport.ReadProjection("positive", "report.json"));
        var artifact = ScenarioReports.BuildArtifact(report);
        using var descriptor = JsonDocument.Parse(ReportingCorpusTestSupport.ReadInvalid("artifact.json"));
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var value in descriptor.RootElement.GetProperty("cases").EnumerateArray())
        {
            if (!value.GetProperty("representability").EnumerateArray().Any(item => item.GetString() == "csharp"))
            {
                continue;
            }
            var id = value.GetProperty("id").GetString()!;
            Assert.IsTrue(seen.Add(id), $"duplicate corpus case {id}");
            if (id == "artifact_payload_detachment")
            {
                ProveDetachment();
                continue;
            }
            ReportingCorpusTestSupport.AssertArtifactFailure(() => Exercise(id, report, artifact));
        }
        Assert.AreEqual(19, seen.Count);
    }

    [TestMethod]
    public void ArtifactFileDoesNotReuseAnInjectedReportError()
    {
        var injected = CaptureReportException(() => ScenarioReports.Validate(null!));
        injected.HelpLink = "HTTP_RETRY_CHECK_ARTIFACT_EXCEPTION_MARKER";
        injected.Data["marker"] = "HTTP_RETRY_CHECK_ARTIFACT_EXCEPTION_MARKER";
        using var memory = new ThrowingMemoryManager(injected);

        var actual = CaptureReportException(() => _ = new ArtifactFile(
            "report.json",
            "application/json",
            memory.CreateReadOnlyMemory()));

        Assert.AreNotSame(injected, actual);
        Assert.AreEqual("HTTP retry scenario suite artifact is invalid", actual.Message);
        Assert.IsNull(actual.HelpLink);
        Assert.IsNull(actual.InnerException);
        Assert.AreEqual(0, actual.Data.Count);
        Assert.IsNull(actual.StackTrace);
        Assert.AreEqual(actual.Message, actual.ToString());
    }

    private static void Exercise(string id, Report report, IReadOnlyList<ArtifactFile> artifact)
    {
        switch (id)
        {
            case "artifact_missing_file":
                ScenarioReports.ValidateArtifact(ReportingCorpusTestSupport.CopyArtifact(artifact).Take(3).ToArray());
                return;
            case "artifact_extra_file":
                var extra = ReportingCorpusTestSupport.CopyArtifact(artifact);
                extra.Add(new ArtifactFile("summary.md", "text/markdown; charset=utf-8", new byte[] { 1 }));
                ScenarioReports.ValidateArtifact(extra);
                return;
            case "artifact_reordered_files":
                var reordered = ReportingCorpusTestSupport.CopyArtifact(artifact);
                (reordered[1], reordered[2]) = (reordered[2], reordered[1]);
                ScenarioReports.ValidateArtifact(reordered);
                return;
            case "artifact_duplicate_file":
                var duplicated = ReportingCorpusTestSupport.CopyArtifact(artifact);
                duplicated[2] = new ArtifactFile(
                    duplicated[1].Name,
                    duplicated[1].MediaType,
                    duplicated[1].Contents);
                ScenarioReports.ValidateArtifact(duplicated);
                return;
            case "artifact_corrupt_file":
                ValidateWithMutatedFile(artifact, 1, bytes =>
                {
                    bytes[0] ^= 1;
                    return bytes;
                });
                return;
            case "artifact_manifest_whitespace":
                ValidateWithMutatedFile(artifact, 0, bytes =>
                    ReplaceBytes(bytes, "{\n", "{ \n"));
                return;
            case "artifact_manifest_order":
                ValidateWithMutatedFile(artifact, 0, bytes => ReplaceBytes(
                    bytes,
                    "  \"schema_version\": \"http_retry_check.artifact_manifest.v1\",\n" +
                    "  \"report_schema_version\": \"http_retry_check.report.v1\",\n",
                    "  \"report_schema_version\": \"http_retry_check.report.v1\",\n" +
                    "  \"schema_version\": \"http_retry_check.artifact_manifest.v1\",\n"));
                return;
            case "artifact_manifest_unknown_member":
                ValidateWithMutatedFile(artifact, 0, bytes =>
                    ReplaceBytes(bytes, "{\n", "{\n  \"marker_unknown\": true,\n"));
                return;
            case "artifact_manifest_identity_drift":
                ValidateWithMutatedFile(artifact, 0, bytes => ReplaceBytes(
                    bytes,
                    "http_retry_check.artifact_manifest.v1",
                    "http_retry_check.artifact_manifest.v2"));
                return;
            case "artifact_crossed_projection":
                var crossed = ReportingCorpusTestSupport.CopyArtifact(artifact);
                crossed[3] = new ArtifactFile(
                    "summary.md",
                    "text/markdown; charset=utf-8",
                    ReportingCorpusTestSupport.ReadProjection("mixed", "summary.md"));
                ScenarioReports.ValidateArtifact(crossed);
                return;
            case "artifact_file_above_max":
                _ = new ArtifactFile(
                    "report.json",
                    "application/json",
                    new byte[ScenarioReports.MaxProjectionBytes + 1]);
                return;
            case "artifact_aggregate_above_max":
                _ = new ArtifactFile(
                    "summary.md",
                    "text/markdown; charset=utf-8",
                    new byte[ScenarioReports.MaxProjectionBytes + 1]);
                return;
            case "artifact_build_invalid_report":
                var invalid = (Report)RuntimeHelpers.GetUninitializedObject(typeof(Report));
                _ = ScenarioReports.BuildArtifact(invalid);
                return;
            case "artifact_build_null_report":
                _ = ScenarioReports.BuildArtifact(null!);
                return;
            case "artifact_file_null_constructor":
                _ = new ArtifactFile(null!, "application/json", new byte[] { 1 });
                return;
            case "artifact_file_malformed_constructor":
                _ = new ArtifactFile("./report.json", "application/json", new byte[] { 1 });
                return;
            case "artifact_validate_null":
                ScenarioReports.ValidateArtifact(null!);
                return;
            case "artifact_mutation_after_validation":
                var mutable = ReportingCorpusTestSupport.CopyArtifact(artifact);
                ScenarioReports.ValidateArtifact(mutable);
                var corrupt = mutable[1].Contents.ToArray();
                corrupt[0] ^= 1;
                mutable[1] = new ArtifactFile("report.json", "application/json", corrupt);
                ScenarioReports.ValidateArtifact(mutable);
                return;
            default:
                throw new InvalidOperationException($"uncovered corpus case {id}");
        }
    }

    private static void ProveDetachment()
    {
        var source = new byte[] { 1, 2, 3 };
        var file = new ArtifactFile("report.json", "application/json", source);
        source[0] = 4;
        CollectionAssert.AreEqual(new byte[] { 1, 2, 3 }, file.Contents.ToArray());
        var first = file.Contents.ToArray();
        var second = file.Contents.ToArray();
        Assert.AreNotSame(first, second);
        first[0] = 9;
        CollectionAssert.AreEqual(new byte[] { 1, 2, 3 }, second);
        CollectionAssert.AreEqual(new byte[] { 1, 2, 3 }, file.Contents.ToArray());
    }

    private static void ValidateWithMutatedFile(
        IReadOnlyList<ArtifactFile> artifact,
        int index,
        Func<byte[], byte[]> mutate)
    {
        var candidate = ReportingCorpusTestSupport.CopyArtifact(artifact);
        var contents = mutate(candidate[index].Contents.ToArray());
        candidate[index] = new ArtifactFile(candidate[index].Name, candidate[index].MediaType, contents);
        ScenarioReports.ValidateArtifact(candidate);
    }

    private static byte[] ReplaceBytes(byte[] bytes, string oldValue, string newValue)
    {
        var source = Encoding.UTF8.GetString(bytes);
        var changed = source.Replace(oldValue, newValue, StringComparison.Ordinal);
        Assert.AreNotEqual(source, changed);
        return Encoding.UTF8.GetBytes(changed);
    }

    private static ReportException CaptureReportException(Action operation)
    {
        try
        {
            operation();
        }
        catch (ReportException exception)
        {
            return exception;
        }

        Assert.Fail("Expected a fixed ReportException.");
        throw new InvalidOperationException("unreachable assertion state");
    }

    private sealed class ThrowingMemoryManager : MemoryManager<byte>
    {
        private readonly Exception failure;

        internal ThrowingMemoryManager(Exception failure)
        {
            this.failure = failure;
        }

        internal ReadOnlyMemory<byte> CreateReadOnlyMemory() => CreateMemory(1);

        public override Span<byte> GetSpan() => throw failure;

        public override MemoryHandle Pin(int elementIndex = 0) => throw failure;

        public override void Unpin()
        {
        }

        protected override void Dispose(bool disposing)
        {
        }
    }
}
