using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Reporting;

[TestClass]
public sealed class ProjectionParityTests
{
    [TestMethod]
    [DataRow("positive")]
    [DataRow("unsafe")]
    [DataRow("inconclusive")]
    [DataRow("mixed")]
    public void ProjectionArtifactsAreByteIdentical(string caseId)
    {
        var reportBytes = ReportingCorpusTestSupport.ReadProjection(caseId, "report.json");
        var report = ScenarioReports.Decode(reportBytes);
        ScenarioReports.Validate(report);
        CollectionAssert.AreEqual(reportBytes, ScenarioReports.Encode(report));
        CollectionAssert.AreEqual(
            ReportingCorpusTestSupport.ReadProjection(caseId, "junit.xml"),
            ScenarioReports.JUnit(report));
        CollectionAssert.AreEqual(
            ReportingCorpusTestSupport.ReadProjection(caseId, "summary.md"),
            ScenarioReports.GitHubSummary(report));

        var artifact = ScenarioReports.BuildArtifact(report);
        Assert.AreEqual(4, artifact.Count);
        Assert.IsFalse(artifact is IList<ArtifactFile>);
        var names = new[] { "manifest.json", "report.json", "junit.xml", "summary.md" };
        for (var index = 0; index < names.Length; index++)
        {
            Assert.AreEqual(names[index], artifact[index].Name);
            CollectionAssert.AreEqual(
                ReportingCorpusTestSupport.ReadProjection(caseId, names[index]),
                artifact[index].Contents.ToArray());
        }
        ScenarioReports.ValidateArtifact(artifact);

        var rebuilt = ScenarioReports.Create(ReportingCorpusTestSupport.ToSuiteResult(report));
        CollectionAssert.AreEqual(reportBytes, ScenarioReports.Encode(rebuilt));
    }

    [TestMethod]
    public void ReportAndArtifactValuesAreIndependent()
    {
        var report = ScenarioReports.Decode(
            ReportingCorpusTestSupport.ReadProjection("positive", "report.json"));
        Assert.IsFalse(report.Scenarios is IList<ReportScenario>);
        foreach (var scenario in report.Scenarios)
        {
            Assert.IsFalse(scenario.Findings is IList<ReportFinding>);
        }

        var source = new byte[] { 1, 2, 3 };
        var file = new ArtifactFile("report.json", "application/json", source);
        source[0] = 9;
        CollectionAssert.AreEqual(new byte[] { 1, 2, 3 }, file.Contents.ToArray());

        var exposed = file.Contents;
        Assert.IsTrue(MemoryMarshal.TryGetArray(exposed, out ArraySegment<byte> segment));
        segment.Array![segment.Offset] = 8;
        CollectionAssert.AreEqual(new byte[] { 1, 2, 3 }, file.Contents.ToArray());
    }

    [TestMethod]
    public void ReportedFailuresContainNoCauseOrStack()
    {
        ReportingCorpusTestSupport.AssertReportFailure(() => ScenarioReports.Decode(Array.Empty<byte>()));
        ReportingCorpusTestSupport.AssertArtifactFailure(() => ScenarioReports.ValidateArtifact(null!));
        ReportingCorpusTestSupport.AssertArtifactFailure(
            () => ScenarioReports.BuildArtifact(null!));
        ReportingCorpusTestSupport.AssertArtifactFailure(
            () => _ = new ArtifactFile(null!, "application/json", new byte[] { 1 }));
    }
}
