using System;
using System.Buffers;
using System.Collections.Generic;
using System.Text.Encodings.Web;
using System.Text.Json;

namespace HttpRetryCheck.V1.Reporting;

public static partial class ScenarioReports
{
    /// <summary>Encodes a report as canonical two-space-indented JSON.</summary>
    public static byte[] Encode(Report report)
    {
        try
        {
            Validate(report);
            var encoded = ReportJsonCodec.Encode(report);
            if (encoded.Length > MaxProjectionBytes)
            {
                throw ReportingFailure.Report();
            }
            return encoded;
        }
        catch (ReportException)
        {
            throw;
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }
    }

    /// <summary>Decodes and validates a canonical v1 report within the size limit.</summary>
    public static Report Decode(byte[] canonicalJson)
    {
        try
        {
            if (canonicalJson is null || canonicalJson.Length == 0 || canonicalJson.Length > MaxProjectionBytes ||
                canonicalJson[^1] != (byte)'\n')
            {
                throw ReportingFailure.Report();
            }

            var report = ReportJsonCodec.Decode(canonicalJson);
            Validate(report);
            var canonical = ReportJsonCodec.Encode(report);
            if (!canonicalJson.AsSpan().SequenceEqual(canonical))
            {
                throw ReportingFailure.Report();
            }
            return report;
        }
        catch (ReportException)
        {
            throw;
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Report();
        }
    }
}

internal static class ReportJsonCodec
{
    private static readonly JsonWriterOptions WriterOptions = new()
    {
        Encoder = JavaScriptEncoder.Default,
        Indented = true,
        NewLine = "\n",
        SkipValidation = false,
    };

    internal static byte[] Encode(Report report)
    {
        var buffer = new ArrayBufferWriter<byte>();
        using (var writer = new Utf8JsonWriter(buffer, WriterOptions))
        {
            WriteReport(writer, report);
        }

        var result = new byte[buffer.WrittenCount + 1];
        buffer.WrittenSpan.CopyTo(result);
        result[^1] = (byte)'\n';
        return result;
    }

    internal static Report Decode(ReadOnlySpan<byte> encoded)
    {
        var reader = new Utf8JsonReader(encoded, new JsonReaderOptions
        {
            AllowTrailingCommas = false,
            CommentHandling = JsonCommentHandling.Disallow,
            MaxDepth = 8,
        });
        var report = ReadReport(ref reader);
        Require(!reader.Read());
        return report;
    }

    private static void WriteReport(Utf8JsonWriter writer, Report report)
    {
        writer.WriteStartObject();
        writer.WriteString("schema_version", report.SchemaVersion);
        writer.WriteString("suite_identity", report.SuiteIdentity);
        writer.WriteString("explanation_identity", report.ExplanationIdentity);
        writer.WriteString("claim_ceiling", report.ClaimCeiling);
        writer.WriteString("assessment", ReportWire.AssessmentName(report.Assessment));
        writer.WriteString("assessment_text", report.AssessmentText);
        writer.WriteString("outcome", ReportWire.OutcomeName(report.Outcome));
        writer.WritePropertyName("summary");
        WriteSummary(writer, report.Summary);
        writer.WritePropertyName("scenarios");
        writer.WriteStartArray();
        foreach (var scenario in report.Scenarios)
        {
            WriteScenario(writer, scenario);
        }
        writer.WriteEndArray();
        writer.WriteEndObject();
    }

    private static void WriteSummary(Utf8JsonWriter writer, ReportSummary summary)
    {
        writer.WriteStartObject();
        writer.WriteNumber("scenarios", summary.Scenarios);
        writer.WriteNumber("passed", summary.Passed);
        writer.WriteNumber("failed", summary.Failed);
        writer.WriteNumber("inconclusive", summary.Inconclusive);
        writer.WriteEndObject();
    }

    private static void WriteScenario(Utf8JsonWriter writer, ReportScenario scenario)
    {
        writer.WriteStartObject();
        writer.WriteString("scenario", ReportWire.ScenarioName(scenario.Scenario));
        writer.WriteString("scenario_text", scenario.ScenarioText);
        writer.WriteString("assessment", ReportWire.AssessmentName(scenario.Assessment));
        writer.WriteString("assessment_text", scenario.AssessmentText);
        writer.WritePropertyName("observation");
        WriteObservation(writer, scenario.Observation);
        writer.WritePropertyName("findings");
        writer.WriteStartArray();
        foreach (var finding in scenario.Findings)
        {
            writer.WriteStartObject();
            writer.WriteString("code", ReportWire.FindingName(finding.Code));
            writer.WriteString("text", finding.Text);
            writer.WriteEndObject();
        }
        writer.WriteEndArray();
        writer.WriteEndObject();
    }

    private static void WriteObservation(Utf8JsonWriter writer, ReportObservation observation)
    {
        writer.WriteStartObject();
        writer.WriteBoolean("capture_complete", observation.CaptureComplete);
        writer.WriteNumber("attempt_count", observation.AttemptCount);
        writer.WriteNumber("effect_count", observation.EffectCount);
        writer.WriteNumber("overlap_count", observation.OverlapCount);
        writer.WriteNumber("retry_after_effect_count", observation.RetryAfterEffectCount);
        writer.WriteNumber("retry_after_unconfirmed_count", observation.RetryAfterUnconfirmedCount);
        writer.WriteNumber("retry_before_response_count", observation.RetryBeforeResponseCount);
        writer.WriteNumber("response_attempt_count", observation.ResponseAttemptCount);
        writer.WriteNumber("response_complete_count", observation.ResponseCompleteCount);
        writer.WriteBoolean("first_response_complete", observation.FirstResponseComplete);
        writer.WriteNumber("delay_complete_count", observation.DelayCompleteCount);
        writer.WriteBoolean("method_consistent", observation.MethodConsistent);
        writer.WriteBoolean("destination_consistent", observation.DestinationConsistent);
        writer.WriteBoolean("body_consistent", observation.BodyConsistent);
        writer.WriteString("credential", ReportWire.CredentialName(observation.Credential));
        writer.WriteString("cleanup", ReportWire.CleanupName(observation.Cleanup));
        writer.WriteEndObject();
    }

    private static Report ReadReport(ref Utf8JsonReader reader)
    {
        ReadStartObject(ref reader);
        ReadProperty(ref reader, "schema_version");
        var schemaVersion = ReadString(ref reader);
        ReadProperty(ref reader, "suite_identity");
        var suiteIdentity = ReadString(ref reader);
        ReadProperty(ref reader, "explanation_identity");
        var explanationIdentity = ReadString(ref reader);
        ReadProperty(ref reader, "claim_ceiling");
        var claimCeiling = ReadString(ref reader);
        ReadProperty(ref reader, "assessment");
        Require(ReportWire.TryParseAssessment(ReadString(ref reader), out var assessment));
        ReadProperty(ref reader, "assessment_text");
        var assessmentText = ReadString(ref reader);
        ReadProperty(ref reader, "outcome");
        Require(ReportWire.TryParseOutcome(ReadString(ref reader), out var outcome));
        ReadProperty(ref reader, "summary");
        var summary = ReadSummary(ref reader);
        ReadProperty(ref reader, "scenarios");
        var scenarios = ReadScenarios(ref reader);
        ReadEndObject(ref reader);
        return new Report(
            schemaVersion,
            suiteIdentity,
            explanationIdentity,
            claimCeiling,
            assessment,
            assessmentText,
            outcome,
            summary,
            scenarios);
    }

    private static ReportSummary ReadSummary(ref Utf8JsonReader reader)
    {
        ReadStartObject(ref reader);
        ReadProperty(ref reader, "scenarios");
        var scenarios = ReadUInt32(ref reader);
        ReadProperty(ref reader, "passed");
        var passed = ReadUInt32(ref reader);
        ReadProperty(ref reader, "failed");
        var failed = ReadUInt32(ref reader);
        ReadProperty(ref reader, "inconclusive");
        var inconclusive = ReadUInt32(ref reader);
        ReadEndObject(ref reader);
        return new ReportSummary(scenarios, passed, failed, inconclusive);
    }

    private static IReadOnlyList<ReportScenario> ReadScenarios(ref Utf8JsonReader reader)
    {
        ReadStartArray(ref reader);
        var values = new List<ReportScenario>(6);
        while (ReadNextArrayValue(ref reader))
        {
            Require(values.Count < 6);
            values.Add(ReadScenarioFromCurrent(ref reader));
        }
        Require(values.Count == 6);
        return new ReportReadOnlyList<ReportScenario>(values);
    }

    private static ReportScenario ReadScenarioFromCurrent(ref Utf8JsonReader reader)
    {
        Require(reader.TokenType == JsonTokenType.StartObject);
        ReadProperty(ref reader, "scenario");
        Require(ReportWire.TryParseScenario(ReadString(ref reader), out var scenario));
        ReadProperty(ref reader, "scenario_text");
        var scenarioText = ReadString(ref reader);
        ReadProperty(ref reader, "assessment");
        Require(ReportWire.TryParseAssessment(ReadString(ref reader), out var assessment));
        ReadProperty(ref reader, "assessment_text");
        var assessmentText = ReadString(ref reader);
        ReadProperty(ref reader, "observation");
        var observation = ReadObservation(ref reader);
        ReadProperty(ref reader, "findings");
        var findings = ReadFindings(ref reader);
        ReadEndObject(ref reader);
        return new ReportScenario(
            scenario,
            scenarioText,
            assessment,
            assessmentText,
            observation,
            findings);
    }

    private static ReportObservation ReadObservation(ref Utf8JsonReader reader)
    {
        ReadStartObject(ref reader);
        ReadProperty(ref reader, "capture_complete");
        var captureComplete = ReadBoolean(ref reader);
        ReadProperty(ref reader, "attempt_count");
        var attemptCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "effect_count");
        var effectCount = ReadUInt64(ref reader);
        ReadProperty(ref reader, "overlap_count");
        var overlapCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "retry_after_effect_count");
        var retryAfterEffectCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "retry_after_unconfirmed_count");
        var retryAfterUnconfirmedCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "retry_before_response_count");
        var retryBeforeResponseCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "response_attempt_count");
        var responseAttemptCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "response_complete_count");
        var responseCompleteCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "first_response_complete");
        var firstResponseComplete = ReadBoolean(ref reader);
        ReadProperty(ref reader, "delay_complete_count");
        var delayCompleteCount = ReadUInt32(ref reader);
        ReadProperty(ref reader, "method_consistent");
        var methodConsistent = ReadBoolean(ref reader);
        ReadProperty(ref reader, "destination_consistent");
        var destinationConsistent = ReadBoolean(ref reader);
        ReadProperty(ref reader, "body_consistent");
        var bodyConsistent = ReadBoolean(ref reader);
        ReadProperty(ref reader, "credential");
        Require(ReportWire.TryParseCredential(ReadString(ref reader), out var credential));
        ReadProperty(ref reader, "cleanup");
        Require(ReportWire.TryParseCleanup(ReadString(ref reader), out var cleanup));
        ReadEndObject(ref reader);
        return new ReportObservation(
            captureComplete,
            attemptCount,
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

    private static IReadOnlyList<ReportFinding> ReadFindings(ref Utf8JsonReader reader)
    {
        ReadStartArray(ref reader);
        var values = new List<ReportFinding>();
        while (ReadNextArrayValue(ref reader))
        {
            Require(values.Count < 18 && reader.TokenType == JsonTokenType.StartObject);
            ReadProperty(ref reader, "code");
            Require(ReportWire.TryParseFinding(ReadString(ref reader), out var code));
            ReadProperty(ref reader, "text");
            var text = ReadString(ref reader);
            ReadEndObject(ref reader);
            values.Add(new ReportFinding(code, text));
        }
        return new ReportReadOnlyList<ReportFinding>(values);
    }

    private static bool ReadNextArrayValue(ref Utf8JsonReader reader)
    {
        Require(reader.Read());
        if (reader.TokenType == JsonTokenType.EndArray)
        {
            return false;
        }
        return true;
    }

    private static void ReadStartObject(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.StartObject);
    }

    private static void ReadEndObject(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.EndObject);
    }

    private static void ReadStartArray(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.StartArray);
    }

    private static void ReadProperty(ref Utf8JsonReader reader, string expected)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.PropertyName &&
            reader.ValueTextEquals(expected));
    }

    private static string ReadString(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && reader.TokenType == JsonTokenType.String);
        return reader.GetString() ?? throw new FormatException();
    }

    private static bool ReadBoolean(ref Utf8JsonReader reader)
    {
        Require(reader.Read());
        return reader.TokenType switch
        {
            JsonTokenType.True => true,
            JsonTokenType.False => false,
            _ => throw new FormatException(),
        };
    }

    private static uint ReadUInt32(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && IsUnsignedIntegerToken(ref reader));
        if (!reader.TryGetUInt32(out var value))
        {
            throw new FormatException();
        }
        return value;
    }

    private static ulong ReadUInt64(ref Utf8JsonReader reader)
    {
        Require(reader.Read() && IsUnsignedIntegerToken(ref reader));
        if (!reader.TryGetUInt64(out var value))
        {
            throw new FormatException();
        }
        return value;
    }

    private static bool IsUnsignedIntegerToken(ref Utf8JsonReader reader)
    {
        if (reader.TokenType != JsonTokenType.Number || reader.HasValueSequence || reader.ValueSpan.Length == 0)
        {
            return false;
        }
        foreach (var value in reader.ValueSpan)
        {
            if (value is < (byte)'0' or > (byte)'9')
            {
                return false;
            }
        }
        return true;
    }

    private static void Require(bool condition)
    {
        if (!condition)
        {
            throw new FormatException();
        }
    }
}
