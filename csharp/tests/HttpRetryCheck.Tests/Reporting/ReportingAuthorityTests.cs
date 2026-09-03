using System;
using System.Collections.Generic;
using System.Linq;
using System.Reflection;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Reporting;

[TestClass]
public sealed class ReportingAuthorityTests
{
    [TestMethod]
    public void ReportingPublicTypesMatchTheExpectedApi()
    {
        var expected = new[]
        {
            typeof(ArtifactFile),
            typeof(Outcome),
            typeof(Report),
            typeof(ReportException),
            typeof(ReportFinding),
            typeof(ReportObservation),
            typeof(ReportScenario),
            typeof(ReportSummary),
            typeof(ScenarioReports),
        }.Select(type => type.FullName).OrderBy(value => value, StringComparer.Ordinal).ToArray();
        var actual = typeof(ScenarioReports).Assembly.ExportedTypes
            .Where(type => type.Namespace == "HttpRetryCheck.V1.Reporting")
            .Select(type => type.FullName)
            .OrderBy(value => value, StringComparer.Ordinal)
            .ToArray();
        CollectionAssert.AreEqual(expected, actual);
    }

    [TestMethod]
    public void OutcomeAndConstantsMatchTheExpectedApi()
    {
        CollectionAssert.AreEqual(
            new[] { "Pass=1", "Fail=2", "Inconclusive=3" },
            Enum.GetValues<Outcome>().Select(value => $"{value}={(int)value}").ToArray());
        var constants = typeof(ScenarioReports)
            .GetFields(BindingFlags.Public | BindingFlags.Static | BindingFlags.DeclaredOnly)
            .ToDictionary(field => field.Name, field => field.GetRawConstantValue(), StringComparer.Ordinal);
        Assert.AreEqual("http_retry_check.report.v2", constants[nameof(ScenarioReports.SchemaVersion)]);
        Assert.AreEqual(
            "urn:http-retry-check:schema:report:v2",
            constants[nameof(ScenarioReports.SchemaId)]);
        Assert.AreEqual("http_retry_check.scenario_suite.v1", constants[nameof(ScenarioReports.SuiteIdentity)]);
        Assert.AreEqual(
            "http_retry_check.scenario_explanations.v1",
            constants[nameof(ScenarioReports.ExplanationIdentity)]);
        Assert.AreEqual(
            "This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.",
            constants[nameof(ScenarioReports.ClaimCeiling)]);
        Assert.AreEqual(262144, constants[nameof(ScenarioReports.MaxProjectionBytes)]);
        Assert.AreEqual(1048576, constants[nameof(ScenarioReports.MaxArtifactBytes)]);
    }

    [TestMethod]
    public void ReportingMembersAndConstructorsMatchTheExpectedApi()
    {
        AssertProperties<ReportSummary>(
            ("Scenarios", typeof(uint)),
            ("Passed", typeof(uint)),
            ("Failed", typeof(uint)),
            ("Inconclusive", typeof(uint)));
        AssertConstructor<ReportSummary>(typeof(uint), typeof(uint), typeof(uint), typeof(uint));

        AssertProperties<ReportObservation>(
            ("CaptureComplete", typeof(bool)),
            ("AttemptCount", typeof(uint)),
            ("AttemptLimit", typeof(uint)),
            ("Protocol", typeof(string)),
            ("EffectCount", typeof(ulong)),
            ("OverlapCount", typeof(uint)),
            ("RetryAfterEffectCount", typeof(uint)),
            ("RetryAfterUnconfirmedCount", typeof(uint)),
            ("RetryBeforeResponseCount", typeof(uint)),
            ("ResponseAttemptCount", typeof(uint)),
            ("ResponseCompleteCount", typeof(uint)),
            ("FirstResponseComplete", typeof(bool)),
            ("DelayCompleteCount", typeof(uint)),
            ("MethodConsistent", typeof(bool)),
            ("DestinationConsistent", typeof(bool)),
            ("BodyConsistent", typeof(bool)),
            ("Credential", typeof(CredentialState)),
            ("Cleanup", typeof(CleanupState)));
        AssertConstructor<ReportObservation>(
            typeof(bool), typeof(uint), typeof(uint), typeof(string), typeof(ulong), typeof(uint), typeof(uint), typeof(uint), typeof(uint),
            typeof(uint), typeof(uint), typeof(bool), typeof(uint), typeof(bool), typeof(bool), typeof(bool),
            typeof(CredentialState), typeof(CleanupState));

        AssertProperties<ReportFinding>(("Code", typeof(FindingCode)), ("Text", typeof(string)));
        AssertConstructor<ReportFinding>(typeof(FindingCode), typeof(string));
        AssertProperties<ReportScenario>(
            ("Scenario", typeof(ScenarioId)),
            ("ScenarioText", typeof(string)),
            ("Assessment", typeof(Assessment)),
            ("AssessmentText", typeof(string)),
            ("Observation", typeof(ReportObservation)),
            ("Findings", typeof(IReadOnlyList<ReportFinding>)));
        AssertConstructor<ReportScenario>(
            typeof(ScenarioId), typeof(string), typeof(Assessment), typeof(string), typeof(ReportObservation),
            typeof(IReadOnlyList<ReportFinding>));
        AssertProperties<Report>(
            ("SchemaVersion", typeof(string)),
            ("SuiteIdentity", typeof(string)),
            ("ExplanationIdentity", typeof(string)),
            ("ClaimCeiling", typeof(string)),
            ("Assessment", typeof(Assessment)),
            ("AssessmentText", typeof(string)),
            ("Outcome", typeof(Outcome)),
            ("Summary", typeof(ReportSummary)),
            ("Scenarios", typeof(IReadOnlyList<ReportScenario>)));
        AssertConstructor<Report>(
            typeof(string), typeof(string), typeof(string), typeof(string), typeof(Assessment), typeof(string),
            typeof(Outcome), typeof(ReportSummary), typeof(IReadOnlyList<ReportScenario>));
        AssertProperties<ArtifactFile>(
            ("Name", typeof(string)),
            ("MediaType", typeof(string)),
            ("Contents", typeof(ReadOnlyMemory<byte>)));
        AssertConstructor<ArtifactFile>(typeof(string), typeof(string), typeof(ReadOnlyMemory<byte>));

        Assert.AreEqual(0, typeof(ReportException).GetConstructors(BindingFlags.Public | BindingFlags.Instance).Length);
        var exceptionMembers = typeof(ReportException)
            .GetMembers(BindingFlags.Public | BindingFlags.Instance | BindingFlags.DeclaredOnly)
            .Where(member => member.MemberType is MemberTypes.Method or MemberTypes.Property)
            .Select(member => member.Name)
            .OrderBy(value => value, StringComparer.Ordinal)
            .ToArray();
        CollectionAssert.AreEqual(new[] { "StackTrace", "ToString", "get_StackTrace" }, exceptionMembers);
    }

    [TestMethod]
    public void ScenarioReportsMethodsMatchTheExpectedApi()
    {
        var methods = typeof(ScenarioReports)
            .GetMethods(BindingFlags.Public | BindingFlags.Static | BindingFlags.DeclaredOnly)
            .Select(method => method.ToString())
            .OrderBy(value => value, StringComparer.Ordinal)
            .ToArray();
        var expected = new[]
        {
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.BuildArtifact))!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.Create))!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.Decode))!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.Encode))!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.GitHubSummary))!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.JUnit))!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.Validate), new[] { typeof(Report) })!.ToString(),
            typeof(ScenarioReports).GetMethod(nameof(ScenarioReports.ValidateArtifact))!.ToString(),
        }.OrderBy(value => value, StringComparer.Ordinal).ToArray();
        CollectionAssert.AreEqual(expected, methods);
    }

    private static void AssertProperties<T>(params (string Name, Type Type)[] expected)
    {
        var actual = typeof(T)
            .GetProperties(BindingFlags.Public | BindingFlags.Instance | BindingFlags.DeclaredOnly)
            .Select(property => (property.Name, property.PropertyType))
            .ToArray();
        CollectionAssert.AreEqual(expected, actual);
        Assert.IsTrue(actual.All(property => typeof(T).GetProperty(property.Name)!.CanRead));
        Assert.IsTrue(actual.All(property => !typeof(T).GetProperty(property.Name)!.CanWrite));
        Assert.AreEqual(
            0,
            typeof(T).GetMethods(BindingFlags.Public | BindingFlags.Instance | BindingFlags.DeclaredOnly)
                .Count(method => !method.IsSpecialName));
    }

    private static void AssertConstructor<T>(params Type[] parameterTypes)
    {
        var constructors = typeof(T).GetConstructors(BindingFlags.Public | BindingFlags.Instance);
        Assert.AreEqual(1, constructors.Length);
        CollectionAssert.AreEqual(
            parameterTypes,
            constructors[0].GetParameters().Select(parameter => parameter.ParameterType).ToArray());
    }
}
