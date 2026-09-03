using System;
using System.Collections.Generic;
using System.Linq;
using System.Reflection;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Authority;

[TestClass]
public sealed class ProjectionTextAuthorityTests
{
    private static readonly char[] ForbiddenCharacters = ['"', '\'', '<', '>', '&', '+', '\\'];

    [TestMethod]
    public void FixedProjectionValuesRemainBytePortable()
    {
        foreach ((string name, string value) in FixedProjectionValues())
        {
            Assert.IsNotEmpty(value, name);
            foreach (char character in value)
            {
                Assert.IsTrue(
                    character is >= (char)0x20 and <= (char)0x7e,
                    $"{name} contains a non-printable or non-ASCII character U+{(int)character:X4}.");
                Assert.IsFalse(
                    ForbiddenCharacters.Contains(character),
                    $"{name} contains forbidden character U+{(int)character:X4}.");
            }
        }
    }

    private static IEnumerable<(string Name, string Value)> FixedProjectionValues()
    {
        foreach (ScenarioId scenario in Enum.GetValues<ScenarioId>())
        {
            yield return ($"scenario text {scenario}", ScenarioExplanations.ScenarioText(scenario));
        }

        foreach (Assessment assessment in Enum.GetValues<Assessment>())
        {
            yield return ($"assessment text {assessment}", ScenarioExplanations.AssessmentText(assessment));
        }

        foreach (FindingCode finding in Enum.GetValues<FindingCode>())
        {
            yield return ($"finding text {finding}", ScenarioExplanations.FindingText(finding));
        }

        foreach (FieldInfo field in typeof(ScenarioReports).GetFields(
            BindingFlags.Public | BindingFlags.Static))
        {
            if (field.IsLiteral && field.FieldType == typeof(string))
            {
                yield return ($"ScenarioReports.{field.Name}", RequiredConstant(field));
            }
        }

        Type artifactCodec = RequiredType("HttpRetryCheck.V1.Reporting.ArtifactCodec");
        foreach (FieldInfo field in artifactCodec.GetFields(
            BindingFlags.NonPublic | BindingFlags.Static))
        {
            if (field.IsLiteral && field.FieldType == typeof(string) &&
                (field.Name.Contains("Schema", StringComparison.Ordinal) ||
                    field.Name.Contains("Domain", StringComparison.Ordinal) ||
                    field.Name.EndsWith("MediaType", StringComparison.Ordinal)))
            {
                yield return ($"ArtifactCodec.{field.Name}", RequiredConstant(field));
            }
        }

        yield return (
            "ReportProjectionCodec.JUnitSuiteName",
            RequiredConstant(RequiredType("HttpRetryCheck.V1.Reporting.ReportProjectionCodec")
                .GetField("JUnitSuiteName", BindingFlags.NonPublic | BindingFlags.Static)
                ?? throw new AssertFailedException("JUnit suite name is unavailable.")));

        foreach ((string method, Type enumType) in WireNameMethods())
        {
            MethodInfo member = RequiredType("HttpRetryCheck.V1.Reporting.ReportWire").GetMethod(
                method,
                BindingFlags.NonPublic | BindingFlags.Static)
                ?? throw new AssertFailedException($"ReportWire.{method} is unavailable.");
            foreach (object value in Enum.GetValues(enumType))
            {
                object? result = member.Invoke(null, [value]);
                yield return (
                    $"ReportWire.{method}({value})",
                    result as string
                        ?? throw new AssertFailedException($"ReportWire.{method} did not return text."));
            }
        }
    }

    private static IEnumerable<(string Method, Type EnumType)> WireNameMethods()
    {
        yield return ("OutcomeName", typeof(Outcome));
        yield return ("ScenarioName", typeof(ScenarioId));
        yield return ("AssessmentName", typeof(Assessment));
        yield return ("CredentialName", typeof(CredentialState));
        yield return ("CleanupName", typeof(CleanupState));
        yield return ("FindingName", typeof(FindingCode));
    }

    private static Type RequiredType(string name) =>
        typeof(ScenarioReports).Assembly.GetType(name, throwOnError: false)
        ?? throw new AssertFailedException($"Projection type {name} is unavailable.");

    private static string RequiredConstant(FieldInfo field) =>
        field.GetRawConstantValue() as string
        ?? throw new AssertFailedException($"Projection constant {field.Name} is unavailable.");
}
