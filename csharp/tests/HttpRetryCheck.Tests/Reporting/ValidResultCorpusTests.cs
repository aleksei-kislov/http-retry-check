using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.Json;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Reporting;

[TestClass]
public sealed class ValidResultCorpusTests
{
    [TestMethod]
    public void ValidCorpusResultsCreateDeterministicEvidence()
    {
        var rows = LoadRows();
        Assert.AreEqual(42, rows.Count);
        var resultCount = 0;
        foreach (var category in new[] { "positive", "unsafe", "inconclusive", "mixed" })
        {
            var path = Path.Combine(
                ReportingCorpusTestSupport.CorpusRoot,
                "results",
                category,
                "results.json");
            using var document = JsonDocument.Parse(File.ReadAllBytes(path));
            foreach (var value in document.RootElement.GetProperty("cases").EnumerateArray())
            {
                if (!IsCSharp(value))
                {
                    continue;
                }
                var scenarios = value.GetProperty("rows")
                    .EnumerateArray()
                    .Select(item => rows[item.GetString()!])
                    .ToArray();
                var result = new SuiteResult(ParseAssessment(value.GetProperty("assessment").GetString()!), scenarios);
                ScenarioSuite.Validate(result);
                var report = ScenarioReports.Create(result);
                ScenarioReports.Validate(report);
                CollectionAssert.AreEqual(ScenarioReports.Encode(report), ScenarioReports.Encode(report));
                CollectionAssert.AreEqual(ScenarioReports.JUnit(report), ScenarioReports.JUnit(report));
                CollectionAssert.AreEqual(
                    ScenarioReports.GitHubSummary(report),
                    ScenarioReports.GitHubSummary(report));
                var artifact = ScenarioReports.BuildArtifact(report);
                ScenarioReports.ValidateArtifact(artifact);
                resultCount++;
            }
        }
        Assert.AreEqual(14, resultCount);
    }

    private static Dictionary<string, ScenarioResult> LoadRows()
    {
        var rows = new Dictionary<string, ScenarioResult>(StringComparer.Ordinal);
        foreach (var category in new[] { "positive", "unsafe", "inconclusive" })
        {
            var path = Path.Combine(
                ReportingCorpusTestSupport.CorpusRoot,
                "results",
                category,
                "rows.json");
            using var document = JsonDocument.Parse(File.ReadAllBytes(path));
            foreach (var value in document.RootElement.GetProperty("cases").EnumerateArray())
            {
                if (!IsCSharp(value))
                {
                    continue;
                }
                var observation = value.GetProperty("observation");
                var findings = value.GetProperty("findings")
                    .EnumerateArray()
                    .Select(item => ParseFinding(item.GetString()!))
                    .ToArray();
                var row = new ScenarioResult(
                    ParseScenario(value.GetProperty("scenario").GetString()!),
                    ParseAssessment(value.GetProperty("assessment").GetString()!),
                    new Observation(
                        observation.GetProperty("capture_complete").GetBoolean(),
                        observation.GetProperty("attempt_count").GetUInt32(),
                        observation.GetProperty("effect_count").GetUInt64(),
                        observation.GetProperty("overlap_count").GetUInt32(),
                        observation.GetProperty("retry_after_effect_count").GetUInt32(),
                        observation.GetProperty("retry_after_unconfirmed_count").GetUInt32(),
                        observation.GetProperty("retry_before_response_count").GetUInt32(),
                        observation.GetProperty("response_attempt_count").GetUInt32(),
                        observation.GetProperty("response_complete_count").GetUInt32(),
                        observation.GetProperty("first_response_complete").GetBoolean(),
                        observation.GetProperty("delay_complete_count").GetUInt32(),
                        observation.GetProperty("method_consistent").GetBoolean(),
                        observation.GetProperty("destination_consistent").GetBoolean(),
                        observation.GetProperty("body_consistent").GetBoolean(),
                        ParseCredential(observation.GetProperty("credential").GetString()!),
                        ParseCleanup(observation.GetProperty("cleanup").GetString()!)),
                    findings);
                Assert.IsTrue(rows.TryAdd(value.GetProperty("id").GetString()!, row));
            }
        }
        return rows;
    }

    private static bool IsCSharp(JsonElement value) => value
        .GetProperty("representability")
        .EnumerateArray()
        .Any(item => item.GetString() == "csharp");

    private static ScenarioId ParseScenario(string value) => value switch
    {
        "accept_then_disconnect" => ScenarioId.AcceptThenDisconnect,
        "disconnect_before_acceptance" => ScenarioId.DisconnectBeforeAcceptance,
        "changed_body_retry" => ScenarioId.ChangedBodyRetry,
        "cross_origin_redirect_credentials" => ScenarioId.CrossOriginRedirectCredentials,
        "retry_limit" => ScenarioId.RetryLimit,
        "delayed_response" => ScenarioId.DelayedResponse,
        _ => throw new InvalidOperationException(value),
    };

    private static Assessment ParseAssessment(string value) => value switch
    {
        "no_unsafe_behavior_observed" => Assessment.NoUnsafeBehaviorObserved,
        "unsafe_behavior_observed" => Assessment.UnsafeBehaviorObserved,
        "inconclusive" => Assessment.Inconclusive,
        _ => throw new InvalidOperationException(value),
    };

    private static CredentialState ParseCredential(string value) => value switch
    {
        "not_observed" => CredentialState.NotObserved,
        "source_only" => CredentialState.SourceOnly,
        "absent_at_target" => CredentialState.AbsentAtTarget,
        "exposed_at_target" => CredentialState.ExposedAtTarget,
        "missing" => CredentialState.Missing,
        _ => throw new InvalidOperationException(value),
    };

    private static CleanupState ParseCleanup(string value) => value switch
    {
        "succeeded" => CleanupState.Succeeded,
        "failed" => CleanupState.Failed,
        _ => throw new InvalidOperationException(value),
    };

    private static FindingCode ParseFinding(string value) => value switch
    {
        "attempt_not_observed" => FindingCode.AttemptNotObserved,
        "capture_incomplete" => FindingCode.CaptureIncomplete,
        "response_incomplete" => FindingCode.ResponseIncomplete,
        "delay_incomplete" => FindingCode.DelayIncomplete,
        "attempt_limit_exceeded" => FindingCode.AttemptLimitExceeded,
        "retry_before_response" => FindingCode.RetryBeforeResponse,
        "retry_after_accepted_request" => FindingCode.RetryAfterAcceptedRequest,
        "retry_after_unconfirmed_acceptance" => FindingCode.RetryAfterUnconfirmedAcceptance,
        "method_changed" => FindingCode.MethodChanged,
        "destination_changed" => FindingCode.DestinationChanged,
        "body_changed" => FindingCode.BodyChanged,
        "credential_not_observed" => FindingCode.CredentialNotObserved,
        "credential_missing" => FindingCode.CredentialMissing,
        "credential_exposed_at_target" => FindingCode.CredentialExposedAtTarget,
        "effect_not_observed" => FindingCode.EffectNotObserved,
        "effect_limit_exceeded" => FindingCode.EffectLimitExceeded,
        "cleanup_unverified" => FindingCode.CleanupUnverified,
        "scenario_incomplete" => FindingCode.ScenarioIncomplete,
        _ => throw new InvalidOperationException(value),
    };
}
