using System;
using System.Collections.Generic;
using System.IO;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;

namespace HttpRetryCheck.Tests.Reporting;

internal static class ReportingCorpusTestSupport
{
    internal static string CorpusRoot { get; } = FindCorpusRoot();

    internal static byte[] ReadProjection(string caseId, string name) =>
        File.ReadAllBytes(Path.Combine(CorpusRoot, "projections", caseId, name));

    internal static string ReadInvalid(string name) =>
        File.ReadAllText(Path.Combine(CorpusRoot, "invalid", name));

    internal static SuiteResult ToSuiteResult(Report report)
    {
        var scenarios = new ScenarioResult[report.Scenarios.Count];
        for (var index = 0; index < scenarios.Length; index++)
        {
            var row = report.Scenarios[index];
            var findings = new FindingCode[row.Findings.Count];
            for (var findingIndex = 0; findingIndex < findings.Length; findingIndex++)
            {
                findings[findingIndex] = row.Findings[findingIndex].Code;
            }
            var observation = row.Observation;
            scenarios[index] = new ScenarioResult(
                row.Scenario,
                row.Assessment,
                new Observation(
                    observation.CaptureComplete,
                    observation.AttemptCount,
                    observation.AttemptLimit,
                    observation.Protocol,
                    observation.EffectCount,
                    observation.OverlapCount,
                    observation.RetryAfterEffectCount,
                    observation.RetryAfterUnconfirmedCount,
                    observation.RetryBeforeResponseCount,
                    observation.ResponseAttemptCount,
                    observation.ResponseCompleteCount,
                    observation.FirstResponseComplete,
                    observation.DelayCompleteCount,
                    observation.MethodConsistent,
                    observation.DestinationConsistent,
                    observation.BodyConsistent,
                    observation.Credential,
                    observation.Cleanup),
                findings);
        }
        return new SuiteResult(report.Assessment, scenarios);
    }

    internal static void AssertReportFailure(Action operation)
    {
        try
        {
            operation();
            throw new InvalidOperationException("expected ReportException");
        }
        catch (ReportException exception)
        {
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.AreEqual(
                "HTTP retry scenario suite report is invalid",
                exception.Message);
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.IsNull(exception.InnerException);
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.IsNull(exception.StackTrace);
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.AreEqual(exception.Message, exception.ToString());
        }
    }

    internal static void AssertArtifactFailure(Action operation)
    {
        try
        {
            operation();
            throw new InvalidOperationException("expected ReportException");
        }
        catch (ReportException exception)
        {
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.AreEqual(
                "HTTP retry scenario suite artifact is invalid",
                exception.Message);
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.IsNull(exception.InnerException);
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.IsNull(exception.StackTrace);
            Microsoft.VisualStudio.TestTools.UnitTesting.Assert.AreEqual(exception.Message, exception.ToString());
        }
    }

    internal static List<ArtifactFile> CopyArtifact(IReadOnlyList<ArtifactFile> source)
    {
        var copy = new List<ArtifactFile>(source.Count);
        for (var index = 0; index < source.Count; index++)
        {
            copy.Add(new ArtifactFile(source[index].Name, source[index].MediaType, source[index].Contents));
        }
        return copy;
    }

    private static string FindCorpusRoot()
    {
        var directory = new DirectoryInfo(AppContext.BaseDirectory);
        while (directory is not null)
        {
            var candidate = Path.Combine(directory.FullName, "conformance", "http-retry-check", "v1");
            if (File.Exists(Path.Combine(candidate, "manifest.json")))
            {
                return candidate;
            }
            directory = directory.Parent;
        }
        throw new InvalidOperationException("corpus root unavailable");
    }
}
