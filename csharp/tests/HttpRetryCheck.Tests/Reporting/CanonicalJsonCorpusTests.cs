using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Text.Json;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Reporting;

[TestClass]
public sealed class CanonicalJsonCorpusTests
{
    [TestMethod]
    public void CanonicalReportAndManifestUseLfOnlyAndRejectCrlf()
    {
        var frozen = ReportingCorpusTestSupport.ReadProjection("positive", "report.json");
        var report = ScenarioReports.Decode(frozen);
        var encoded = ScenarioReports.Encode(report);
        AssertLfOnly(encoded);

        var artifact = ScenarioReports.BuildArtifact(report);
        AssertLfOnly(artifact[0].Contents.ToArray());
        AssertLfOnly(artifact[1].Contents.ToArray());

        var crlf = Encoding.UTF8.GetBytes(
            Encoding.UTF8.GetString(frozen).Replace("\n", "\r\n", StringComparison.Ordinal));
        ReportingCorpusTestSupport.AssertReportFailure(() => ScenarioReports.Decode(crlf));
    }

    [TestMethod]
    public void InvalidCanonicalJsonVectorsAreRejected()
    {
        var canonical = ReportingCorpusTestSupport.ReadProjection("positive", "report.json");
        using var descriptor = JsonDocument.Parse(ReportingCorpusTestSupport.ReadInvalid("canonical-json.json"));
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var value in descriptor.RootElement.GetProperty("cases").EnumerateArray())
        {
            if (!IsCSharp(value))
            {
                continue;
            }
            var id = value.GetProperty("id").GetString()!;
            Assert.IsTrue(seen.Add(id), $"duplicate corpus case {id}");
            var candidate = Mutate(id, canonical);
            ReportingCorpusTestSupport.AssertReportFailure(() => ScenarioReports.Decode(candidate));
        }
        Assert.AreEqual(35, seen.Count);
    }

    private static bool IsCSharp(JsonElement descriptor) => descriptor
        .GetProperty("representability")
        .EnumerateArray()
        .Any(value => value.GetString() == "csharp");

    private static void AssertLfOnly(byte[] value)
    {
        Assert.IsTrue(value.Length > 1);
        Assert.AreEqual((byte)'\n', value[^1]);
        Assert.AreNotEqual((byte)'\n', value[^2]);
        Assert.IsFalse(value.Contains((byte)'\r'));
    }

    private static byte[] Mutate(string id, byte[] canonical)
    {
        var text = Encoding.UTF8.GetString(canonical);
        return id switch
        {
            "json_empty" => Array.Empty<byte>(),
            "json_oversized" => Encoding.ASCII.GetBytes(new string(' ', ScenarioReports.MaxProjectionBytes + 1)),
            "json_invalid_utf8" => new byte[] { 0xff },
            "json_bom" => Combine(new byte[] { 0xef, 0xbb, 0xbf }, canonical),
            "json_missing_terminal_lf" => canonical[..^1],
            "json_alternate_indentation" => Encoding.UTF8.GetBytes(DoubleIndent(text)),
            "json_alternate_whitespace" => Replace(text, "\"schema_version\":", "\"schema_version\" :"),
            "json_member_order" => SwapTopLevelIdentityLines(text),
            "json_alternate_escaping" => Replace(
                text,
                "\"claim_ceiling\": \"This",
                "\"claim_ceiling\": \"\\u0054his"),
            "json_duplicate_member" => Replace(
                text,
                "{\n  \"schema_version\":",
                "{\n  \"schema_version\": \"http_retry_check.report.v2\",\n  \"schema_version\":"),
            "json_unknown_member" => Replace(text, "{\n", "{\n  \"marker_unknown\": true,\n"),
            "json_null" => Encoding.ASCII.GetBytes("null\n"),
            "json_wrong_type" => Replace(text, "\"summary\": {", "\"summary\": \"marker_wrong_type\","),
            "json_fractional_number" => Replace(text, "\"passed\": 6", "\"passed\": 6.0"),
            "json_exponent_number" => Replace(text, "\"passed\": 6", "\"passed\": 6e0"),
            "json_overflowing_number" => Replace(text, "\"passed\": 6", "\"passed\": 18446744073709551616"),
            "json_excessive_depth" => Encoding.ASCII.GetBytes("[[[[[[[[[0]]]]]]]]]\n"),
            "json_excessive_members" => Encoding.ASCII.GetBytes(ExcessiveMembers()),
            "json_excessive_items" => Encoding.ASCII.GetBytes("[" + string.Join(',', Enumerable.Repeat("0", 33)) + "]\n"),
            "json_wrong_identity" => Replace(
                text,
                "http_retry_check.report.v2",
                "http_retry_check.report.v1"),
            "json_trailing_data" => Combine(canonical, Encoding.ASCII.GetBytes("trailing")),
            "json_negative_summary_scenarios" => Replace(text, "\"scenarios\": 6", "\"scenarios\": -1"),
            "json_negative_summary_passed" => Replace(text, "\"passed\": 6", "\"passed\": -1"),
            "json_negative_summary_failed" => Replace(text, "\"failed\": 0", "\"failed\": -1"),
            "json_negative_summary_inconclusive" => Replace(text, "\"inconclusive\": 0", "\"inconclusive\": -1"),
            "json_negative_scenarios_0_observation_attempt_count" => Replace(text, "\"attempt_count\": 1", "\"attempt_count\": -1"),
            "json_negative_scenarios_0_observation_attempt_limit" => Replace(text, "\"attempt_limit\": 2", "\"attempt_limit\": -1"),
            "json_negative_scenarios_0_observation_effect_count" => Replace(text, "\"effect_count\": 1", "\"effect_count\": -1"),
            "json_negative_scenarios_0_observation_overlap_count" => Replace(text, "\"overlap_count\": 0", "\"overlap_count\": -1"),
            "json_negative_scenarios_0_observation_retry_after_effect_count" => Replace(text, "\"retry_after_effect_count\": 0", "\"retry_after_effect_count\": -1"),
            "json_negative_scenarios_0_observation_retry_after_unconfirmed_count" => Replace(text, "\"retry_after_unconfirmed_count\": 0", "\"retry_after_unconfirmed_count\": -1"),
            "json_negative_scenarios_0_observation_retry_before_response_count" => Replace(text, "\"retry_before_response_count\": 0", "\"retry_before_response_count\": -1"),
            "json_negative_scenarios_0_observation_response_attempt_count" => Replace(text, "\"response_attempt_count\": 0", "\"response_attempt_count\": -1"),
            "json_negative_scenarios_0_observation_response_complete_count" => Replace(text, "\"response_complete_count\": 0", "\"response_complete_count\": -1"),
            "json_negative_scenarios_0_observation_delay_complete_count" => Replace(text, "\"delay_complete_count\": 0", "\"delay_complete_count\": -1"),
            _ => throw new InvalidOperationException($"uncovered corpus case {id}"),
        };
    }

    private static byte[] Replace(string source, string oldValue, string newValue)
    {
        var changed = source.Replace(oldValue, newValue, StringComparison.Ordinal);
        Assert.AreNotEqual(source, changed);
        return Encoding.UTF8.GetBytes(changed);
    }

    private static byte[] SwapTopLevelIdentityLines(string source)
    {
        const string first = "  \"schema_version\": \"http_retry_check.report.v2\",\n";
        const string second = "  \"suite_identity\": \"http_retry_check.scenario_suite.v1\",\n";
        return Replace(source, first + second, second + first);
    }

    private static string DoubleIndent(string source)
    {
        var lines = source.Split('\n');
        for (var index = 0; index < lines.Length; index++)
        {
            var count = 0;
            while (count < lines[index].Length && lines[index][count] == ' ')
            {
                count++;
            }
            if (count != 0)
            {
                lines[index] = new string(' ', count * 2) + lines[index][count..];
            }
        }
        return string.Join('\n', lines);
    }

    private static string ExcessiveMembers()
    {
        var members = Enumerable.Range(0, 33).Select(index => $"\"marker{index}\":0");
        return "{" + string.Join(',', members) + "}\n";
    }

    private static byte[] Combine(byte[] first, byte[] second)
    {
        var result = new byte[first.Length + second.Length];
        first.CopyTo(result, 0);
        second.CopyTo(result, first.Length);
        return result;
    }
}
