using System;
using System.Buffers;
using System.Buffers.Binary;
using System.Collections.Generic;
using System.Globalization;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;

namespace HttpRetryCheck.V1.Reporting;

public static partial class ScenarioReports
{
    /// <summary>Builds the four evidence artifact files in memory.</summary>
    public static IReadOnlyList<ArtifactFile> BuildArtifact(Report report)
    {
        try
        {
            if (report is null)
            {
                throw ReportingFailure.Artifact();
            }

            byte[] reportJson;
            byte[] junit;
            byte[] summary;
            try
            {
                reportJson = Encode(report);
                junit = JUnit(report);
                summary = GitHubSummary(report);
            }
            catch (ReportException)
            {
                throw ReportingFailure.Artifact();
            }

            var payloads = new[]
            {
                new ArtifactSnapshot(ArtifactCodec.ReportName, ArtifactCodec.JsonMediaType, reportJson),
                new ArtifactSnapshot(ArtifactCodec.JUnitName, ArtifactCodec.XmlMediaType, junit),
                new ArtifactSnapshot(ArtifactCodec.SummaryName, ArtifactCodec.MarkdownMediaType, summary),
            };
            var manifest = ArtifactCodec.CreateManifest(payloads);
            var manifestJson = ArtifactCodec.EncodeManifest(manifest);
            var files = new[]
            {
                new ArtifactFile(ArtifactCodec.ManifestName, ArtifactCodec.JsonMediaType, manifestJson),
                new ArtifactFile(ArtifactCodec.ReportName, ArtifactCodec.JsonMediaType, reportJson),
                new ArtifactFile(ArtifactCodec.JUnitName, ArtifactCodec.XmlMediaType, junit),
                new ArtifactFile(ArtifactCodec.SummaryName, ArtifactCodec.MarkdownMediaType, summary),
            };
            if (!ArtifactCodec.ValidTotal(files))
            {
                throw ReportingFailure.Artifact();
            }
            return new ReportReadOnlyList<ArtifactFile>(files, takeOwnership: true);
        }
        catch (ReportException)
        {
            throw ReportingFailure.Artifact();
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Artifact();
        }
    }

    /// <summary>Validates all four files by rebuilding the artifact.</summary>
    public static void ValidateArtifact(IReadOnlyList<ArtifactFile> files)
    {
        try
        {
            if (files is null || files.Count != 4)
            {
                throw ReportingFailure.Artifact();
            }

            var snapshots = new ArtifactSnapshot[4];
            long total = 0;
            for (var index = 0; index < snapshots.Length; index++)
            {
                var file = files[index];
                if (file is null)
                {
                    throw ReportingFailure.Artifact();
                }
                var contents = file.CopyContents();
                if (!ArtifactCodec.IsExpectedFile(index, file.Name, file.MediaType) || contents.Length == 0 ||
                    contents.Length > MaxProjectionBytes)
                {
                    throw ReportingFailure.Artifact();
                }
                total += contents.Length;
                snapshots[index] = new ArtifactSnapshot(file.Name, file.MediaType, contents);
            }
            if (total > MaxArtifactBytes)
            {
                throw ReportingFailure.Artifact();
            }

            var manifest = ArtifactCodec.DecodeManifest(snapshots[0].Contents);
            if (!ArtifactCodec.ValidateManifest(manifest, snapshots.AsSpan(1, 3)))
            {
                throw ReportingFailure.Artifact();
            }

            Report report;
            try
            {
                report = Decode(snapshots[1].Contents);
            }
            catch (ReportException)
            {
                throw ReportingFailure.Artifact();
            }
            var rebuilt = BuildArtifact(report);
            for (var index = 0; index < snapshots.Length; index++)
            {
                var rebuiltContents = rebuilt[index].CopyContents();
                if (snapshots[index].Name != rebuilt[index].Name ||
                    snapshots[index].MediaType != rebuilt[index].MediaType ||
                    !snapshots[index].Contents.AsSpan().SequenceEqual(rebuiltContents))
                {
                    throw ReportingFailure.Artifact();
                }
            }
        }
        catch (ReportException)
        {
            throw ReportingFailure.Artifact();
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Artifact();
        }
    }
}

internal sealed class ArtifactSnapshot
{
    internal ArtifactSnapshot(string name, string mediaType, byte[] contents)
    {
        Name = name;
        MediaType = mediaType;
        Contents = (byte[])contents.Clone();
    }

    internal string Name { get; }
    internal string MediaType { get; }
    internal byte[] Contents { get; }
}

internal readonly record struct ArtifactDescriptor(
    string Name,
    string MediaType,
    ulong SizeBytes,
    string Sha256);

internal sealed class ArtifactManifest
{
    internal ArtifactManifest(
        string schemaVersion,
        string reportSchemaVersion,
        string claimCeiling,
        string digestDomain,
        string aggregateSha256,
        ArtifactDescriptor[] files)
    {
        SchemaVersion = schemaVersion;
        ReportSchemaVersion = reportSchemaVersion;
        ClaimCeiling = claimCeiling;
        DigestDomain = digestDomain;
        AggregateSha256 = aggregateSha256;
        Files = (ArtifactDescriptor[])files.Clone();
    }

    internal string SchemaVersion { get; }
    internal string ReportSchemaVersion { get; }
    internal string ClaimCeiling { get; }
    internal string DigestDomain { get; }
    internal string AggregateSha256 { get; }
    internal ArtifactDescriptor[] Files { get; }
}

internal static class ArtifactCodec
{
    internal const string ManifestSchema = "http_retry_check.artifact_manifest.v1";
    internal const string DigestDomain = "http_retry_check.artifact_set.v1";
    internal const string ManifestName = "manifest.json";
    internal const string ReportName = "report.json";
    internal const string JUnitName = "junit.xml";
    internal const string SummaryName = "summary.md";
    internal const string JsonMediaType = "application/json";
    internal const string XmlMediaType = "application/xml";
    internal const string MarkdownMediaType = "text/markdown; charset=utf-8";

    private static readonly UTF8Encoding Utf8 = new(encoderShouldEmitUTF8Identifier: false, throwOnInvalidBytes: true);
    private static readonly JsonWriterOptions WriterOptions = new()
    {
        Encoder = JavaScriptEncoder.Default,
        Indented = true,
        NewLine = "\n",
        SkipValidation = false,
    };

    internal static bool IsFileIdentity(string name, string mediaType) =>
        (name == ManifestName && mediaType == JsonMediaType) ||
        (name == ReportName && mediaType == JsonMediaType) ||
        (name == JUnitName && mediaType == XmlMediaType) ||
        (name == SummaryName && mediaType == MarkdownMediaType);

    internal static bool IsExpectedFile(int index, string name, string mediaType) => index switch
    {
        0 => name == ManifestName && mediaType == JsonMediaType,
        1 => name == ReportName && mediaType == JsonMediaType,
        2 => name == JUnitName && mediaType == XmlMediaType,
        3 => name == SummaryName && mediaType == MarkdownMediaType,
        _ => false,
    };

    internal static bool ValidTotal(IReadOnlyList<ArtifactFile> files)
    {
        if (files.Count != 4)
        {
            return false;
        }
        long total = 0;
        for (var index = 0; index < files.Count; index++)
        {
            if (!IsExpectedFile(index, files[index].Name, files[index].MediaType))
            {
                return false;
            }
            var length = files[index].Contents.Length;
            if (length == 0 || length > ScenarioReports.MaxProjectionBytes)
            {
                return false;
            }
            total += length;
        }
        return total <= ScenarioReports.MaxArtifactBytes;
    }

    internal static ArtifactManifest CreateManifest(ReadOnlySpan<ArtifactSnapshot> payloads)
    {
        if (payloads.Length != 3)
        {
            throw ReportingFailure.Artifact();
        }
        var descriptors = new ArtifactDescriptor[3];
        for (var index = 0; index < descriptors.Length; index++)
        {
            descriptors[index] = DescriptorFor(payloads[index]);
        }
        var aggregate = AggregateDigest(descriptors);
        return new ArtifactManifest(
            ManifestSchema,
            ScenarioReports.SchemaVersion,
            ScenarioReports.ClaimCeiling,
            DigestDomain,
            aggregate,
            descriptors);
    }

    internal static byte[] EncodeManifest(ArtifactManifest manifest)
    {
        var buffer = new ArrayBufferWriter<byte>();
        using (var writer = new Utf8JsonWriter(buffer, WriterOptions))
        {
            writer.WriteStartObject();
            writer.WriteString("schema_version", manifest.SchemaVersion);
            writer.WriteString("report_schema_version", manifest.ReportSchemaVersion);
            writer.WriteString("claim_ceiling", manifest.ClaimCeiling);
            writer.WriteString("digest_domain", manifest.DigestDomain);
            writer.WriteString("aggregate_sha256", manifest.AggregateSha256);
            writer.WritePropertyName("files");
            writer.WriteStartArray();
            foreach (var descriptor in manifest.Files)
            {
                writer.WriteStartObject();
                writer.WriteString("name", descriptor.Name);
                writer.WriteString("media_type", descriptor.MediaType);
                writer.WriteNumber("size_bytes", descriptor.SizeBytes);
                writer.WriteString("sha256", descriptor.Sha256);
                writer.WriteEndObject();
            }
            writer.WriteEndArray();
            writer.WriteEndObject();
        }
        var result = new byte[buffer.WrittenCount + 1];
        buffer.WrittenSpan.CopyTo(result);
        result[^1] = (byte)'\n';
        if (result.Length > ScenarioReports.MaxProjectionBytes)
        {
            throw ReportingFailure.Artifact();
        }
        return result;
    }

    internal static ArtifactManifest DecodeManifest(ReadOnlySpan<byte> encoded)
    {
        if (encoded.Length == 0 || encoded.Length > ScenarioReports.MaxProjectionBytes ||
            encoded[^1] != (byte)'\n')
        {
            throw ReportingFailure.Artifact();
        }
        var reader = new Utf8JsonReader(encoded, new JsonReaderOptions
        {
            AllowTrailingCommas = false,
            CommentHandling = JsonCommentHandling.Disallow,
            MaxDepth = 8,
        });
        ReadStartObject(ref reader);
        ReadProperty(ref reader, "schema_version");
        var schemaVersion = ReadString(ref reader);
        ReadProperty(ref reader, "report_schema_version");
        var reportSchemaVersion = ReadString(ref reader);
        ReadProperty(ref reader, "claim_ceiling");
        var claimCeiling = ReadString(ref reader);
        ReadProperty(ref reader, "digest_domain");
        var digestDomain = ReadString(ref reader);
        ReadProperty(ref reader, "aggregate_sha256");
        var aggregate = ReadString(ref reader);
        ReadProperty(ref reader, "files");
        ReadStartArray(ref reader);
        var descriptors = new List<ArtifactDescriptor>(3);
        while (ReadNextArrayValue(ref reader))
        {
            Require(descriptors.Count < 3 && reader.TokenType == JsonTokenType.StartObject);
            ReadProperty(ref reader, "name");
            var name = ReadString(ref reader);
            ReadProperty(ref reader, "media_type");
            var mediaType = ReadString(ref reader);
            ReadProperty(ref reader, "size_bytes");
            var sizeBytes = ReadUInt64(ref reader);
            ReadProperty(ref reader, "sha256");
            var sha256 = ReadString(ref reader);
            ReadEndObject(ref reader);
            descriptors.Add(new ArtifactDescriptor(name, mediaType, sizeBytes, sha256));
        }
        Require(descriptors.Count == 3);
        ReadEndObject(ref reader);
        Require(!reader.Read());
        var manifest = new ArtifactManifest(
            schemaVersion,
            reportSchemaVersion,
            claimCeiling,
            digestDomain,
            aggregate,
            descriptors.ToArray());
        var canonical = EncodeManifest(manifest);
        if (!encoded.SequenceEqual(canonical))
        {
            throw ReportingFailure.Artifact();
        }
        return manifest;
    }

    internal static bool ValidateManifest(ArtifactManifest manifest, ReadOnlySpan<ArtifactSnapshot> payloads)
    {
        if (manifest.SchemaVersion != ManifestSchema ||
            manifest.ReportSchemaVersion != ScenarioReports.SchemaVersion ||
            manifest.ClaimCeiling != ScenarioReports.ClaimCeiling || manifest.DigestDomain != DigestDomain ||
            !ValidSha256(manifest.AggregateSha256) || manifest.Files.Length != 3 || payloads.Length != 3)
        {
            return false;
        }
        for (var index = 0; index < manifest.Files.Length; index++)
        {
            if (manifest.Files[index] != DescriptorFor(payloads[index]))
            {
                return false;
            }
        }
        return manifest.AggregateSha256 == AggregateDigest(manifest.Files);
    }

    private static ArtifactDescriptor DescriptorFor(ArtifactSnapshot file) => new(
        file.Name,
        file.MediaType,
        checked((ulong)file.Contents.LongLength),
        Sha256(file.Contents));

    private static string AggregateDigest(ReadOnlySpan<ArtifactDescriptor> files)
    {
        using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
        AppendDigestAtom(hash, DigestDomain);
        AppendDigestAtom(hash, ManifestSchema);
        AppendDigestAtom(hash, ScenarioReports.SchemaVersion);
        AppendDigestAtom(hash, ScenarioReports.ClaimCeiling);
        foreach (var file in files)
        {
            AppendDigestAtom(hash, file.Name);
            AppendDigestAtom(hash, file.MediaType);
            AppendDigestAtom(hash, file.SizeBytes.ToString(CultureInfo.InvariantCulture));
            AppendDigestAtom(hash, file.Sha256);
        }
        return LowerHex(hash.GetHashAndReset());
    }

    private static void AppendDigestAtom(IncrementalHash hash, string value)
    {
        var encoded = Utf8.GetBytes(value);
        Span<byte> length = stackalloc byte[sizeof(ulong)];
        BinaryPrimitives.WriteUInt64BigEndian(length, checked((ulong)encoded.LongLength));
        hash.AppendData(length);
        hash.AppendData(encoded);
    }

    private static string Sha256(ReadOnlySpan<byte> contents) => LowerHex(SHA256.HashData(contents));

    private static string LowerHex(ReadOnlySpan<byte> bytes)
    {
        const string digits = "0123456789abcdef";
        return string.Create(bytes.Length * 2, bytes.ToArray(), static (destination, state) =>
        {
            for (var index = 0; index < state.Length; index++)
            {
                destination[index * 2] = digits[state[index] >> 4];
                destination[(index * 2) + 1] = digits[state[index] & 0x0f];
            }
        });
    }

    private static bool ValidSha256(string value)
    {
        if (value.Length != 64)
        {
            return false;
        }
        foreach (var character in value)
        {
            if (character is not (>= '0' and <= '9') and not (>= 'a' and <= 'f'))
            {
                return false;
            }
        }
        return true;
    }

    private static bool ReadNextArrayValue(ref Utf8JsonReader reader)
    {
        Require(reader.Read());
        return reader.TokenType != JsonTokenType.EndArray;
    }

    private static void ReadStartObject(ref Utf8JsonReader reader) =>
        Require(reader.Read() && reader.TokenType == JsonTokenType.StartObject);

    private static void ReadEndObject(ref Utf8JsonReader reader) =>
        Require(reader.Read() && reader.TokenType == JsonTokenType.EndObject);

    private static void ReadStartArray(ref Utf8JsonReader reader) =>
        Require(reader.Read() && reader.TokenType == JsonTokenType.StartArray);

    private static void ReadProperty(ref Utf8JsonReader reader, string expected) =>
        Require(reader.Read() && reader.TokenType == JsonTokenType.PropertyName && reader.ValueTextEquals(expected));

    private static string ReadString(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.String);
        return reader.GetString() ?? throw new FormatException();
    }

    private static ulong ReadUInt64(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.Number && !reader.HasValueSequence &&
            reader.ValueSpan.Length != 0);
        foreach (var value in reader.ValueSpan)
        {
            Require(value is >= (byte)'0' and <= (byte)'9');
        }
        if (!reader.TryGetUInt64(out var result))
        {
            throw new FormatException();
        }
        return result;
    }

    private static void Require(bool condition)
    {
        if (!condition)
        {
            throw new FormatException();
        }
    }
}
