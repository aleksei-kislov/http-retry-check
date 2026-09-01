using System;
using System.Collections;
using System.Collections.Generic;
using System.IO;
using System.Text.Json.Nodes;
using HttpRetryCheck.V1;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Semantics;

[TestClass]
public sealed class SemanticCorpusTests
{
    private const string InvalidSchema = "http_retry_check.conformance_invalid.v1";
    private const string RowsSchema = "http_retry_check.conformance_rows.v1";
    private const string ResultsSchema = "http_retry_check.conformance_results.v1";

    [TestMethod]
    public void ValidCorpusRowsAndResultsCanBeConstructedAndValidated()
    {
        var corpus = LoadSemanticCorpus();
        Assert.AreEqual(42, corpus.Rows.Count);
        Assert.AreEqual(14, corpus.Results.Count);

        var positiveRows = 0;
        var reachableFindings = new HashSet<FindingCode>();
        foreach (var entry in corpus.Rows)
        {
            var source = entry.Value;
            Assert.IsTrue(IsRepresentableIn(source, "csharp"), entry.Key);
            var row = BuildRow(source, null);

            Assert.AreEqual(MapScenario(RequiredString(source, "scenario")), row.Scenario, entry.Key);
            Assert.AreEqual(MapAssessment(RequiredString(source, "assessment")), row.Assessment, entry.Key);
            if (row.Assessment == Assessment.NoUnsafeBehaviorObserved)
            {
                positiveRows++;
                Assert.AreEqual(0, row.Findings.Count, entry.Key);
            }

            for (var index = 0; index < row.Findings.Count; index++)
            {
                var finding = row.Findings[index];
                Assert.AreNotEqual(FindingCode.ScenarioIncomplete, finding, entry.Key);
                reachableFindings.Add(finding);
            }
        }

        Assert.AreEqual(9, positiveRows);
        var expectedReachable = new[]
        {
            FindingCode.AttemptNotObserved,
            FindingCode.CaptureIncomplete,
            FindingCode.ResponseIncomplete,
            FindingCode.DelayIncomplete,
            FindingCode.AttemptLimitExceeded,
            FindingCode.RetryBeforeResponse,
            FindingCode.RetryAfterAcceptedRequest,
            FindingCode.RetryAfterUnconfirmedAcceptance,
            FindingCode.MethodChanged,
            FindingCode.DestinationChanged,
            FindingCode.BodyChanged,
            FindingCode.CredentialNotObserved,
            FindingCode.CredentialMissing,
            FindingCode.CredentialExposedAtTarget,
            FindingCode.EffectNotObserved,
            FindingCode.EffectLimitExceeded,
            FindingCode.CleanupUnverified,
        };
        CollectionAssert.AreEquivalent(expectedReachable, Copy(reachableFindings));
        Assert.IsFalse(reachableFindings.Contains(FindingCode.ScenarioIncomplete));

        var mixedSeen = false;
        foreach (var entry in corpus.Results)
        {
            var source = entry.Value;
            Assert.IsTrue(IsRepresentableIn(source, "csharp"), entry.Key);
            var result = BuildResult(source);
            ScenarioSuite.Validate(result);

            Assert.AreEqual(MapAssessment(RequiredString(source, "assessment")), result.Assessment, entry.Key);
            Assert.AreEqual(6, result.Scenarios.Count, entry.Key);
            if (entry.Key == "result_mixed_unsafe_over_inconclusive")
            {
                mixedSeen = true;
                Assert.AreEqual(Assessment.UnsafeBehaviorObserved, result.Assessment);
                Assert.IsTrue(ContainsAssessment(result.Scenarios, Assessment.Inconclusive));
                Assert.IsTrue(ContainsAssessment(result.Scenarios, Assessment.UnsafeBehaviorObserved));
            }
        }

        Assert.IsTrue(mixedSeen);
    }

    [TestMethod]
    public void InvalidSemanticCorpusVectorsAreRejected()
    {
        var corpus = LoadSemanticCorpus();
        var bundle = LoadObject(Path.Combine(CorpusRoot(), "invalid", "semantic.json"));
        Assert.AreEqual(InvalidSchema, RequiredString(bundle, "schema_version"));
        Assert.AreEqual("semantic", RequiredString(bundle, "category"));
        var vectors = RequiredArray(bundle, "cases");
        Assert.AreEqual(64, vectors.Count);

        var rejected = 0;
        var detached = 0;
        var notRepresentable = 0;
        var forgedScenarioIncomplete = false;
        var requiredZeroCases = new HashSet<string>(StringComparer.Ordinal);

        foreach (var node in vectors)
        {
            var vector = RequiredObject(node);
            var identifier = RequiredString(vector, "id");
            if (!IsRepresentableIn(vector, "csharp"))
            {
                Assert.AreEqual("semantic_native_mutation_after_validation", identifier);
                Assert.AreEqual("native_mutate_after_validate", RequiredString(vector, "operation"));
                notRepresentable++;
                continue;
            }

            var baseIdentifier = RequiredString(vector, "base_case");
            Assert.IsTrue(corpus.Results.TryGetValue(baseIdentifier, out var storedBase), identifier);
            var candidate = RequiredObject(storedBase!.DeepClone());

            if (identifier == "semantic_caller_collection_detached")
            {
                Assert.AreEqual("valid_after_caller_mutation", RequiredString(vector, "expected"));
                Assert.AreEqual("native_mutate_original_collection", RequiredString(vector, "operation"));
                var result = BuildResult(candidate, out var suppliedRows, out var suppliedFindings);
                foreach (var findings in suppliedFindings)
                {
                    findings.Add(FindingCode.ScenarioIncomplete);
                }

                suppliedRows!.Clear();
                ScenarioSuite.Validate(result);
                Assert.AreEqual(6, result.Scenarios.Count);
                for (var index = 0; index < result.Scenarios.Count; index++)
                {
                    Assert.AreEqual(0, result.Scenarios[index].Findings.Count);
                }

                detached++;
                continue;
            }

            Assert.AreEqual("invalid_result", RequiredString(vector, "expected"), identifier);
            ApplySemanticMutation(candidate, vector);

            SuiteException? failure = null;
            try
            {
                var result = BuildResult(candidate);
                ScenarioSuite.Validate(result);
            }
            catch (SuiteException exception)
            {
                failure = exception;
            }

            Assert.IsNotNull(failure, identifier);
            var retainedFailure = failure!;
            Assert.AreEqual(SuiteFailureCode.InvalidResult, retainedFailure.Code, identifier);
            Assert.AreEqual("HTTP scenario suite result is invalid", retainedFailure.Message, identifier);
            Assert.IsNull(retainedFailure.InnerException, identifier);
            Assert.IsNull(retainedFailure.StackTrace, identifier);
            Assert.AreEqual(retainedFailure.Message, retainedFailure.ToString(), identifier);
            rejected++;

            if (identifier == "semantic_scenario_incomplete_forged")
            {
                forgedScenarioIncomplete = true;
                var findings = RequiredArray(RequiredArray(candidate, "scenarios")[0]!.AsObject(), "findings");
                Assert.AreEqual("scenario_incomplete", findings[findings.Count - 1]!.GetValue<string>());
            }

            if (HasCoverageSuffix(vector, ".required_zero"))
            {
                requiredZeroCases.Add(identifier);
            }
        }

        Assert.AreEqual(62, rejected);
        Assert.AreEqual(1, detached);
        Assert.AreEqual(1, notRepresentable);
        Assert.IsTrue(forgedScenarioIncomplete);
        CollectionAssert.AreEquivalent(
            new[]
            {
                "semantic_attempt_required_zero",
                "semantic_effect_required_zero",
                "semantic_retry_after_effect_required_zero",
                "semantic_retry_after_unconfirmed_required_zero",
                "semantic_response_attempt_required_zero",
                "semantic_delay_required_zero",
            },
            Copy(requiredZeroCases));
    }

    [TestMethod]
    public void SanitationVectorIdsDoNotAppearInSemanticValues()
    {
        var bundle = LoadObject(Path.Combine(CorpusRoot(), "invalid", "sanitation.json"));
        Assert.AreEqual(InvalidSchema, RequiredString(bundle, "schema_version"));
        Assert.AreEqual("sanitation", RequiredString(bundle, "category"));
        var marker = RequiredString(bundle, "marker");
        var vectors = RequiredArray(bundle, "cases");
        Assert.AreEqual(16, vectors.Count);

        var csharpIdentifiers = new HashSet<string>(StringComparer.Ordinal);
        foreach (var node in vectors)
        {
            var vector = RequiredObject(node);
            if (!IsRepresentableIn(vector, "csharp"))
            {
                continue;
            }

            Assert.AreEqual(marker, RequiredString(vector, "operand"));
            csharpIdentifiers.Add(RequiredString(vector, "id"));
        }

        CollectionAssert.AreEquivalent(
            new[]
            {
                "sanitation_callback_error_value",
                "sanitation_callback_error_type",
                "sanitation_response_reason",
                "sanitation_response_header",
                "sanitation_response_content",
                "sanitation_request_url",
                "sanitation_request_header",
                "sanitation_request_body",
                "sanitation_replay_behavior",
                "sanitation_sender_type",
                "sanitation_sender_formatter",
                "sanitation_environment_proxy",
                "sanitation_environment_credential",
            },
            Copy(csharpIdentifiers));

        var corpus = LoadSemanticCorpus();
        var positive = BuildResult(corpus.Results["result_all_positive"]);
        var rendered = new List<string>
        {
            positive.ToString() ?? string.Empty,
            positive.Scenarios[0].ToString() ?? string.Empty,
            positive.Scenarios[0].Observation.ToString() ?? string.Empty,
            positive.Assessment.ToString(),
            ScenarioExplanations.ScenarioText(ScenarioId.AcceptThenDisconnect),
            ScenarioExplanations.ScenarioText((ScenarioId)int.MaxValue),
            ScenarioExplanations.AssessmentText(Assessment.NoUnsafeBehaviorObserved),
            ScenarioExplanations.AssessmentText((Assessment)int.MaxValue),
            ScenarioExplanations.FindingText(FindingCode.CaptureIncomplete),
            ScenarioExplanations.FindingText((FindingCode)int.MaxValue),
        };

        SuiteException? failure = null;
        try
        {
            _ = new ScenarioResult(
                ScenarioId.AcceptThenDisconnect,
                Assessment.NoUnsafeBehaviorObserved,
                positive.Scenarios[0].Observation,
                new CorpusMarkerThrowingList<FindingCode>(marker));
        }
        catch (SuiteException exception)
        {
            failure = exception;
        }

        Assert.IsNotNull(failure);
        rendered.Add(failure!.Message);
        rendered.Add(failure.ToString());
        rendered.Add(failure.StackTrace ?? string.Empty);
        rendered.Add(failure.InnerException?.ToString() ?? string.Empty);
        foreach (var value in rendered)
        {
            Assert.IsFalse(value.Contains(marker, StringComparison.Ordinal));
        }
    }

    private static SemanticCorpus LoadSemanticCorpus()
    {
        var rows = new Dictionary<string, JsonObject>(StringComparer.Ordinal);
        foreach (var category in new[] { "positive", "unsafe", "inconclusive" })
        {
            var bundle = LoadObject(Path.Combine(CorpusRoot(), "results", category, "rows.json"));
            Assert.AreEqual(RowsSchema, RequiredString(bundle, "schema_version"));
            Assert.AreEqual(category, RequiredString(bundle, "category"));
            foreach (var node in RequiredArray(bundle, "cases"))
            {
                var row = RequiredObject(node);
                rows.Add(RequiredString(row, "id"), RequiredObject(row.DeepClone()));
            }
        }

        var results = new Dictionary<string, JsonObject>(StringComparer.Ordinal);
        foreach (var category in new[] { "positive", "unsafe", "inconclusive", "mixed" })
        {
            var bundle = LoadObject(Path.Combine(CorpusRoot(), "results", category, "results.json"));
            Assert.AreEqual(ResultsSchema, RequiredString(bundle, "schema_version"));
            Assert.AreEqual(category, RequiredString(bundle, "category"));
            foreach (var node in RequiredArray(bundle, "cases"))
            {
                var source = RequiredObject(node);
                var expandedRows = new JsonArray();
                foreach (var rowNode in RequiredArray(source, "rows"))
                {
                    var rowIdentifier = rowNode!.GetValue<string>();
                    Assert.IsTrue(rows.TryGetValue(rowIdentifier, out var row), rowIdentifier);
                    expandedRows.Add(row!.DeepClone());
                }

                var expanded = new JsonObject
                {
                    ["id"] = RequiredString(source, "id"),
                    ["representability"] = RequiredArray(source, "representability").DeepClone(),
                    ["assessment"] = RequiredString(source, "assessment"),
                    ["scenarios"] = expandedRows,
                };
                results.Add(RequiredString(source, "id"), expanded);
            }
        }

        return new SemanticCorpus(rows, results);
    }

    private static SuiteResult BuildResult(JsonObject source)
    {
        return BuildResult(source, out _, out _);
    }

    private static SuiteResult BuildResult(
        JsonObject source,
        out List<ScenarioResult>? suppliedRows,
        out List<List<FindingCode>> suppliedFindings)
    {
        suppliedFindings = new List<List<FindingCode>>();
        var assessment = MapAssessment(OptionalString(source, "assessment"));
        if (source["scenarios"] is null)
        {
            suppliedRows = null;
            return new SuiteResult(assessment, null!);
        }

        suppliedRows = new List<ScenarioResult>();
        foreach (var node in RequiredArray(source, "scenarios"))
        {
            suppliedRows.Add(BuildRow(RequiredObject(node), suppliedFindings));
        }

        return new SuiteResult(assessment, suppliedRows);
    }

    private static ScenarioResult BuildRow(JsonObject source, List<List<FindingCode>>? retainedFindings)
    {
        var scenario = MapScenario(OptionalString(source, "scenario"));
        var assessment = MapAssessment(OptionalString(source, "assessment"));
        var observation = BuildObservation(RequiredObject(source["observation"]));
        IReadOnlyList<FindingCode> findings;
        if (source["findings"] is null)
        {
            findings = null!;
        }
        else
        {
            var supplied = new List<FindingCode>();
            foreach (var node in RequiredArray(source, "findings"))
            {
                supplied.Add(MapFinding(node?.GetValue<string>()));
            }

            retainedFindings?.Add(supplied);
            findings = supplied;
        }

        return new ScenarioResult(scenario, assessment, observation, findings);
    }

    private static Observation BuildObservation(JsonObject source)
    {
        return new Observation(
            RequiredBoolean(source, "capture_complete"),
            RequiredUInt32(source, "attempt_count"),
            RequiredUInt64(source, "effect_count"),
            RequiredUInt32(source, "overlap_count"),
            RequiredUInt32(source, "retry_after_effect_count"),
            RequiredUInt32(source, "retry_after_unconfirmed_count"),
            RequiredUInt32(source, "retry_before_response_count"),
            RequiredUInt32(source, "response_attempt_count"),
            RequiredUInt32(source, "response_complete_count"),
            RequiredBoolean(source, "first_response_complete"),
            RequiredUInt32(source, "delay_complete_count"),
            RequiredBoolean(source, "method_consistent"),
            RequiredBoolean(source, "destination_consistent"),
            RequiredBoolean(source, "body_consistent"),
            MapCredential(OptionalString(source, "credential")),
            MapCleanup(OptionalString(source, "cleanup")));
    }

    private static void ApplySemanticMutation(JsonObject result, JsonObject vector)
    {
        var operation = RequiredString(vector, "operation");
        var target = RequiredString(vector, "target");
        var operand = OptionalString(vector, "operand") ?? string.Empty;
        switch (operation)
        {
            case "set":
                SetAt(result, target, ParseOperand(operand));
                break;
            case "null":
                SetAt(result, target, null);
                break;
            case "remove":
                RemoveAt(result, target);
                break;
            case "duplicate":
                DuplicateAt(result, target);
                break;
            case "append_copy":
                RequiredArray(result, "scenarios").Add(GetAt(result, operand).DeepClone());
                break;
            case "swap":
                SwapAt(result, target, operand);
                break;
            case "append":
                GetAt(result, target).AsArray().Add(ParseOperand(operand));
                break;
            case "set_positive":
                SetPositive(result, target);
                break;
            case "erase_unsafe":
                EraseUnsafe(result, target);
                break;
            default:
                throw new InvalidOperationException("Unsupported semantic corpus operation");
        }
    }

    private static void SetPositive(JsonObject result, string target)
    {
        var row = GetAt(result, target).AsObject();
        row["assessment"] = "no_unsafe_behavior_observed";
        row["findings"] = new JsonArray();
        result["assessment"] = "no_unsafe_behavior_observed";
    }

    private static void EraseUnsafe(JsonObject result, string target)
    {
        var row = GetAt(result, target).AsObject();
        RequiredObject(row["observation"])["capture_complete"] = false;
        row["assessment"] = "inconclusive";
        row["findings"] = new JsonArray("capture_incomplete");
        result["assessment"] = "inconclusive";
    }

    private static void DuplicateAt(JsonObject root, string pointer)
    {
        var (parent, final) = ParentAt(root, pointer);
        var array = parent.AsArray();
        var index = ParseIndex(final, array.Count);
        array.Insert(index + 1, array[index]!.DeepClone());
    }

    private static void RemoveAt(JsonObject root, string pointer)
    {
        var (parent, final) = ParentAt(root, pointer);
        var array = parent.AsArray();
        array.RemoveAt(ParseIndex(final, array.Count));
    }

    private static void SwapAt(JsonObject root, string leftPointer, string rightPointer)
    {
        var left = GetAt(root, leftPointer).DeepClone();
        var right = GetAt(root, rightPointer).DeepClone();
        SetAt(root, leftPointer, right);
        SetAt(root, rightPointer, left);
    }

    private static void SetAt(JsonObject root, string pointer, JsonNode? value)
    {
        var (parent, final) = ParentAt(root, pointer);
        if (parent is JsonObject parentObject)
        {
            parentObject[final] = value;
            return;
        }

        var parentArray = parent.AsArray();
        parentArray[ParseIndex(final, parentArray.Count)] = value;
    }

    private static JsonNode GetAt(JsonObject root, string pointer)
    {
        var parts = PointerParts(pointer);
        JsonNode current = root;
        foreach (var part in parts)
        {
            current = current switch
            {
                JsonObject currentObject => currentObject[part] ??
                    throw new InvalidOperationException("Missing semantic corpus target"),
                JsonArray currentArray => currentArray[ParseIndex(part, currentArray.Count)] ??
                    throw new InvalidOperationException("Null semantic corpus target"),
                _ => throw new InvalidOperationException("Invalid semantic corpus target"),
            };
        }

        return current;
    }

    private static (JsonNode Parent, string Final) ParentAt(JsonObject root, string pointer)
    {
        var parts = PointerParts(pointer);
        if (parts.Length == 0)
        {
            throw new InvalidOperationException("Empty semantic corpus target");
        }

        JsonNode current = root;
        for (var index = 0; index < parts.Length - 1; index++)
        {
            current = current switch
            {
                JsonObject currentObject => currentObject[parts[index]] ??
                    throw new InvalidOperationException("Missing semantic corpus parent"),
                JsonArray currentArray => currentArray[ParseIndex(parts[index], currentArray.Count)] ??
                    throw new InvalidOperationException("Null semantic corpus parent"),
                _ => throw new InvalidOperationException("Invalid semantic corpus parent"),
            };
        }

        return (current, parts[parts.Length - 1]);
    }

    private static string[] PointerParts(string pointer)
    {
        if (!pointer.StartsWith("/", StringComparison.Ordinal) || pointer == "/" ||
            pointer.Contains("//", StringComparison.Ordinal))
        {
            throw new InvalidOperationException("Invalid semantic corpus pointer");
        }

        return pointer.Split('/', StringSplitOptions.RemoveEmptyEntries);
    }

    private static int ParseIndex(string value, int length)
    {
        if (!int.TryParse(value, out var index) || index < 0 || index >= length)
        {
            throw new InvalidOperationException("Invalid semantic corpus index");
        }

        return index;
    }

    private static JsonNode? ParseOperand(string operand)
    {
        return JsonNode.Parse(operand);
    }

    private static bool HasCoverageSuffix(JsonObject vector, string suffix)
    {
        foreach (var node in RequiredArray(vector, "coverage"))
        {
            if (node!.GetValue<string>().EndsWith(suffix, StringComparison.Ordinal))
            {
                return true;
            }
        }

        return false;
    }

    private static bool IsRepresentableIn(JsonObject source, string binding)
    {
        foreach (var node in RequiredArray(source, "representability"))
        {
            if (node!.GetValue<string>() == binding)
            {
                return true;
            }
        }

        return false;
    }

    private static bool ContainsAssessment(IReadOnlyList<ScenarioResult> rows, Assessment assessment)
    {
        for (var index = 0; index < rows.Count; index++)
        {
            if (rows[index].Assessment == assessment)
            {
                return true;
            }
        }

        return false;
    }

    private static ScenarioId MapScenario(string? value)
    {
        return value switch
        {
            "accept_then_disconnect" => ScenarioId.AcceptThenDisconnect,
            "disconnect_before_acceptance" => ScenarioId.DisconnectBeforeAcceptance,
            "changed_body_retry" => ScenarioId.ChangedBodyRetry,
            "cross_origin_redirect_credentials" => ScenarioId.CrossOriginRedirectCredentials,
            "retry_limit" => ScenarioId.RetryLimit,
            "delayed_response" => ScenarioId.DelayedResponse,
            "" => (ScenarioId)0,
            _ => (ScenarioId)int.MaxValue,
        };
    }

    private static Assessment MapAssessment(string? value)
    {
        return value switch
        {
            "no_unsafe_behavior_observed" => Assessment.NoUnsafeBehaviorObserved,
            "unsafe_behavior_observed" => Assessment.UnsafeBehaviorObserved,
            "inconclusive" => Assessment.Inconclusive,
            "" => (Assessment)0,
            _ => (Assessment)int.MaxValue,
        };
    }

    private static CredentialState MapCredential(string? value)
    {
        return value switch
        {
            "not_observed" => CredentialState.NotObserved,
            "source_only" => CredentialState.SourceOnly,
            "absent_at_target" => CredentialState.AbsentAtTarget,
            "exposed_at_target" => CredentialState.ExposedAtTarget,
            "missing" => CredentialState.Missing,
            "" => (CredentialState)0,
            _ => (CredentialState)int.MaxValue,
        };
    }

    private static CleanupState MapCleanup(string? value)
    {
        return value switch
        {
            "succeeded" => CleanupState.Succeeded,
            "failed" => CleanupState.Failed,
            "" => (CleanupState)0,
            _ => (CleanupState)int.MaxValue,
        };
    }

    private static FindingCode MapFinding(string? value)
    {
        return value switch
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
            "" => (FindingCode)0,
            _ => (FindingCode)int.MaxValue,
        };
    }

    private static JsonObject LoadObject(string path)
    {
        return JsonNode.Parse(File.ReadAllText(path))?.AsObject() ??
            throw new InvalidOperationException("Corpus JSON root is unavailable");
    }

    private static string CorpusRoot()
    {
        DirectoryInfo? current = new DirectoryInfo(AppContext.BaseDirectory);
        for (var depth = 0; depth < 12 && current is not null; depth++)
        {
            var candidate = Path.Combine(current.FullName, "conformance", "http-retry-check", "v1");
            if (File.Exists(Path.Combine(candidate, "manifest.json")))
            {
                return candidate;
            }

            current = current.Parent;
        }

        throw new InvalidOperationException("Shared HTTP Retry Check corpus is unavailable");
    }

    private static JsonObject RequiredObject(JsonNode? node)
    {
        return node?.AsObject() ?? throw new InvalidOperationException("Required corpus object is unavailable");
    }

    private static JsonArray RequiredArray(JsonObject source, string name)
    {
        return source[name]?.AsArray() ??
            throw new InvalidOperationException("Required corpus array is unavailable");
    }

    private static string RequiredString(JsonObject source, string name)
    {
        return OptionalString(source, name) ??
            throw new InvalidOperationException("Required corpus string is unavailable");
    }

    private static string? OptionalString(JsonObject source, string name)
    {
        return source[name]?.GetValue<string>();
    }

    private static bool RequiredBoolean(JsonObject source, string name)
    {
        return source[name]?.GetValue<bool>() ??
            throw new InvalidOperationException("Required corpus boolean is unavailable");
    }

    private static uint RequiredUInt32(JsonObject source, string name)
    {
        return source[name]?.GetValue<uint>() ??
            throw new InvalidOperationException("Required corpus uint32 is unavailable");
    }

    private static ulong RequiredUInt64(JsonObject source, string name)
    {
        return source[name]?.GetValue<ulong>() ??
            throw new InvalidOperationException("Required corpus uint64 is unavailable");
    }

    private static T[] Copy<T>(IEnumerable<T> source)
    {
        var copy = new List<T>();
        foreach (var item in source)
        {
            copy.Add(item);
        }

        return copy.ToArray();
    }

    private sealed record SemanticCorpus(
        Dictionary<string, JsonObject> Rows,
        Dictionary<string, JsonObject> Results);

    private sealed class CorpusMarkerThrowingList<T> : IReadOnlyList<T>
    {
        private readonly string marker;

        internal CorpusMarkerThrowingList(string marker)
        {
            this.marker = marker;
        }

        public int Count => throw new InvalidOperationException(marker);

        public T this[int index] => throw new InvalidOperationException(marker);

        public IEnumerator<T> GetEnumerator()
        {
            throw new InvalidOperationException(marker);
        }

        IEnumerator IEnumerable.GetEnumerator() => GetEnumerator();
    }
}
