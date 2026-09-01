using System;
using System.Collections.Generic;
using System.Linq;
using System.Net.Http;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Reporting;
using HttpRetryCheck.V1.Testing;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Authority;

[TestClass]
public sealed class PublicSurfaceTests
{
    [TestMethod]
    public void AssemblyExportsOnlyTheDocumentedPublicTypes()
    {
        string[] actual = typeof(ScenarioSuite).Assembly
            .GetExportedTypes()
            .Select(static type => type.FullName ?? string.Empty)
            .Order(StringComparer.Ordinal)
            .ToArray();

        string[] expected =
        [
            "HttpRetryCheck.V1.Assessment",
            "HttpRetryCheck.V1.CleanupState",
            "HttpRetryCheck.V1.CredentialState",
            "HttpRetryCheck.V1.FindingCode",
            "HttpRetryCheck.V1.Observation",
            "HttpRetryCheck.V1.Reporting.ArtifactFile",
            "HttpRetryCheck.V1.Reporting.Outcome",
            "HttpRetryCheck.V1.Reporting.Report",
            "HttpRetryCheck.V1.Reporting.ReportException",
            "HttpRetryCheck.V1.Reporting.ReportFinding",
            "HttpRetryCheck.V1.Reporting.ReportObservation",
            "HttpRetryCheck.V1.Reporting.ReportScenario",
            "HttpRetryCheck.V1.Reporting.ReportSummary",
            "HttpRetryCheck.V1.Reporting.ScenarioReports",
            "HttpRetryCheck.V1.ScenarioExplanations",
            "HttpRetryCheck.V1.ScenarioId",
            "HttpRetryCheck.V1.ScenarioResult",
            "HttpRetryCheck.V1.ScenarioSuite",
            "HttpRetryCheck.V1.SuiteException",
            "HttpRetryCheck.V1.SuiteFailureCode",
            "HttpRetryCheck.V1.SuiteResult",
            "HttpRetryCheck.V1.Testing.ScenarioAssertionException",
            "HttpRetryCheck.V1.Testing.ScenarioTest",
        ];

        CollectionAssert.AreEqual(expected, actual);
    }

    [TestMethod]
    public void EnumNamesOrderAndValuesMatchTheExpectedApi()
    {
        AssertEnum<ScenarioId>(
            ("AcceptThenDisconnect", 1),
            ("DisconnectBeforeAcceptance", 2),
            ("ChangedBodyRetry", 3),
            ("CrossOriginRedirectCredentials", 4),
            ("RetryLimit", 5),
            ("DelayedResponse", 6));
        AssertEnum<Assessment>(
            ("NoUnsafeBehaviorObserved", 1),
            ("UnsafeBehaviorObserved", 2),
            ("Inconclusive", 3));
        AssertEnum<CleanupState>(("Succeeded", 1), ("Failed", 2));
        AssertEnum<CredentialState>(
            ("NotObserved", 1),
            ("SourceOnly", 2),
            ("AbsentAtTarget", 3),
            ("ExposedAtTarget", 4),
            ("Missing", 5));
        AssertEnum<FindingCode>(
            ("AttemptNotObserved", 1),
            ("CaptureIncomplete", 2),
            ("ResponseIncomplete", 3),
            ("DelayIncomplete", 4),
            ("AttemptLimitExceeded", 5),
            ("RetryBeforeResponse", 6),
            ("RetryAfterAcceptedRequest", 7),
            ("RetryAfterUnconfirmedAcceptance", 8),
            ("MethodChanged", 9),
            ("DestinationChanged", 10),
            ("BodyChanged", 11),
            ("CredentialNotObserved", 12),
            ("CredentialMissing", 13),
            ("CredentialExposedAtTarget", 14),
            ("EffectNotObserved", 15),
            ("EffectLimitExceeded", 16),
            ("CleanupUnverified", 17),
            ("ScenarioIncomplete", 18));
        AssertEnum<SuiteFailureCode>(
            ("InvalidCall", 1),
            ("SuiteUnavailable", 2),
            ("InternalFailure", 3),
            ("InvalidResult", 4));
        AssertEnum<Outcome>(("Pass", 1), ("Fail", 2), ("Inconclusive", 3));
    }

    [TestMethod]
    public void RootMembersMatchTheExpectedApi()
    {
        AssertMembers<Observation>(
            "ctor(bool,uint,ulong,uint,uint,uint,uint,uint,uint,bool,uint,bool,bool,bool,CredentialState,CleanupState)",
            "property bool BodyConsistent",
            "property bool CaptureComplete",
            "property CleanupState Cleanup",
            "property CredentialState Credential",
            "property uint DelayCompleteCount",
            "property bool DestinationConsistent",
            "property ulong EffectCount",
            "property bool FirstResponseComplete",
            "property bool MethodConsistent",
            "property uint AttemptCount",
            "property uint OverlapCount",
            "property uint ResponseAttemptCount",
            "property uint ResponseCompleteCount",
            "property uint RetryAfterEffectCount",
            "property uint RetryAfterUnconfirmedCount",
            "property uint RetryBeforeResponseCount");
        AssertMembers<ScenarioResult>(
            "ctor(ScenarioId,Assessment,Observation,IReadOnlyList<FindingCode>)",
            "property Assessment Assessment",
            "property IReadOnlyList<FindingCode> Findings",
            "property Observation Observation",
            "property ScenarioId Scenario");
        AssertMembers<SuiteResult>(
            "ctor(Assessment,IReadOnlyList<ScenarioResult>)",
            "property Assessment Assessment",
            "property IReadOnlyList<ScenarioResult> Scenarios");
        AssertMembers<SuiteException>(
            "instance string ToString()",
            "property SuiteFailureCode Code",
            "property string StackTrace");
        AssertMembers(typeof(ScenarioSuite),
            "static Task<SuiteResult> RunAsync(HttpMessageInvoker,CancellationToken)",
            "static void Validate(SuiteResult)");
        AssertMembers(typeof(ScenarioExplanations),
            "static string AssessmentText(Assessment)",
            "static string FindingText(FindingCode)",
            "static string ScenarioText(ScenarioId)");

        AssertStaticClass(typeof(ScenarioSuite));
        AssertStaticClass(typeof(ScenarioExplanations));
        AssertSealedClass(typeof(Observation));
        AssertSealedClass(typeof(ScenarioResult));
        AssertSealedClass(typeof(SuiteResult));
        AssertSealedClass(typeof(SuiteException));

        AssertCancellationDefault(typeof(ScenarioSuite), nameof(ScenarioSuite.RunAsync), 1);
    }

    [TestMethod]
    public void TestingMembersMatchTheExpectedApi()
    {
        AssertMembers<ScenarioAssertionException>(
            "instance string ToString()",
            "property string StackTrace");
        AssertMembers(typeof(ScenarioTest),
            "static Task CheckAsync(HttpMessageInvoker,Action<string>,CancellationToken)");
        AssertSealedClass(typeof(ScenarioAssertionException));
        AssertStaticClass(typeof(ScenarioTest));

        MethodInfo method = typeof(ScenarioTest).GetMethod(
            nameof(ScenarioTest.CheckAsync),
            BindingFlags.Public | BindingFlags.Static)
            ?? throw new AssertFailedException("CheckAsync is missing.");
        ParameterInfo[] parameters = method.GetParameters();
        Assert.IsTrue(parameters[1].HasDefaultValue);
        Assert.IsNull(parameters[1].DefaultValue);
        AssertCancellationDefault(typeof(ScenarioTest), nameof(ScenarioTest.CheckAsync), 2);
    }

    [TestMethod]
    public void ReportingMembersMatchTheExpectedApi()
    {
        AssertMembers<ReportSummary>(
            "ctor(uint,uint,uint,uint)",
            "property uint Failed",
            "property uint Inconclusive",
            "property uint Passed",
            "property uint Scenarios");
        AssertMembers<ReportObservation>(
            "ctor(bool,uint,ulong,uint,uint,uint,uint,uint,uint,bool,uint,bool,bool,bool,CredentialState,CleanupState)",
            "property bool BodyConsistent",
            "property bool CaptureComplete",
            "property CleanupState Cleanup",
            "property CredentialState Credential",
            "property uint DelayCompleteCount",
            "property bool DestinationConsistent",
            "property ulong EffectCount",
            "property bool FirstResponseComplete",
            "property bool MethodConsistent",
            "property uint AttemptCount",
            "property uint OverlapCount",
            "property uint ResponseAttemptCount",
            "property uint ResponseCompleteCount",
            "property uint RetryAfterEffectCount",
            "property uint RetryAfterUnconfirmedCount",
            "property uint RetryBeforeResponseCount");
        AssertMembers<ReportFinding>(
            "ctor(FindingCode,string)",
            "property FindingCode Code",
            "property string Text");
        AssertMembers<ReportScenario>(
            "ctor(ScenarioId,string,Assessment,string,ReportObservation,IReadOnlyList<ReportFinding>)",
            "property Assessment Assessment",
            "property string AssessmentText",
            "property IReadOnlyList<ReportFinding> Findings",
            "property ReportObservation Observation",
            "property ScenarioId Scenario",
            "property string ScenarioText");
        AssertMembers<Report>(
            "ctor(string,string,string,string,Assessment,string,Outcome,ReportSummary,IReadOnlyList<ReportScenario>)",
            "property Assessment Assessment",
            "property string AssessmentText",
            "property string ClaimCeiling",
            "property string ExplanationIdentity",
            "property Outcome Outcome",
            "property IReadOnlyList<ReportScenario> Scenarios",
            "property string SchemaVersion",
            "property ReportSummary Summary",
            "property string SuiteIdentity");
        AssertMembers<ArtifactFile>(
            "ctor(string,string,ReadOnlyMemory<byte>)",
            "property ReadOnlyMemory<byte> Contents",
            "property string MediaType",
            "property string Name");
        AssertMembers<ReportException>(
            "instance string ToString()",
            "property string StackTrace");
        AssertMembers(typeof(ScenarioReports),
            "const string ClaimCeiling=This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.",
            "const string ExplanationIdentity=http_retry_check.scenario_explanations.v1",
            "const int MaxArtifactBytes=1048576",
            "const int MaxProjectionBytes=262144",
            "const string SchemaId=urn:http-retry-check:schema:report:v1",
            "const string SchemaVersion=http_retry_check.report.v1",
            "const string SuiteIdentity=http_retry_check.scenario_suite.v1",
            "static IReadOnlyList<ArtifactFile> BuildArtifact(Report)",
            "static Report Create(SuiteResult)",
            "static Report Decode(byte[])",
            "static byte[] Encode(Report)",
            "static byte[] GitHubSummary(Report)",
            "static byte[] JUnit(Report)",
            "static void Validate(Report)",
            "static void ValidateArtifact(IReadOnlyList<ArtifactFile>)");

        Type[] sealedClasses =
        [
            typeof(ReportSummary),
            typeof(ReportObservation),
            typeof(ReportFinding),
            typeof(ReportScenario),
            typeof(Report),
            typeof(ArtifactFile),
            typeof(ReportException),
        ];
        foreach (Type type in sealedClasses)
        {
            AssertSealedClass(type);
        }

        AssertStaticClass(typeof(ScenarioReports));
    }

    [TestMethod]
    public void NullableAnnotationsMatchTheExpectedApi()
    {
        var nullability = new NullabilityInfoContext();
        foreach (Type type in typeof(ScenarioSuite).Assembly.GetExportedTypes())
        {
            EventInfo[] events = type.GetEvents(
                BindingFlags.Public | BindingFlags.Instance | BindingFlags.Static | BindingFlags.DeclaredOnly);
            Assert.HasCount(0, events, type.FullName);

            PropertyInfo[] properties = type.GetProperties(
                BindingFlags.Public | BindingFlags.Instance | BindingFlags.Static | BindingFlags.DeclaredOnly);
            foreach (PropertyInfo property in properties)
            {
                Assert.IsNotNull(property.GetMethod, $"Public property has no getter: {type.FullName}.{property.Name}");
                Assert.IsTrue(property.GetMethod.IsPublic, $"Property getter is not public: {type.FullName}.{property.Name}");
                Assert.IsFalse(property.GetMethod.IsStatic, $"Static public property is forbidden: {type.FullName}.{property.Name}");
                Assert.IsNull(property.SetMethod, $"Public property setter is forbidden: {type.FullName}.{property.Name}");
                Assert.HasCount(0, property.GetIndexParameters(), $"Public indexer is forbidden: {type.FullName}.{property.Name}");
                AssertNullability(
                    property.PropertyType,
                    nullability.Create(property),
                    property.Name == "StackTrace" ? NullabilityState.Nullable : NullabilityState.NotNull,
                    $"{type.FullName}.{property.Name}");
            }

            MethodInfo[] methods = type.GetMethods(
                BindingFlags.Public | BindingFlags.Instance | BindingFlags.Static | BindingFlags.DeclaredOnly);
            var propertyGetters = properties
                .Select(static property => property.GetMethod)
                .Where(static method => method is not null)
                .ToHashSet();
            foreach (MethodInfo method in methods)
            {
                if (method.IsSpecialName)
                {
                    Assert.IsTrue(
                        propertyGetters.Contains(method),
                        $"Unexpected public special-name method: {type.FullName}.{method.Name}");
                    continue;
                }

                AssertNullability(
                    method.ReturnType,
                    nullability.Create(method.ReturnParameter),
                    NullabilityState.NotNull,
                    $"{type.FullName}.{method.Name} return");
                AssertParameterNullability(type, method, method.GetParameters(), nullability);
            }

            foreach (ConstructorInfo constructor in type.GetConstructors(
                BindingFlags.Public | BindingFlags.Instance | BindingFlags.DeclaredOnly))
            {
                AssertParameterNullability(type, constructor, constructor.GetParameters(), nullability);
            }
        }

        MethodInfo run = typeof(ScenarioSuite).GetMethod(nameof(ScenarioSuite.RunAsync))
            ?? throw new AssertFailedException("RunAsync is missing.");
        Assert.AreEqual(NullabilityState.NotNull, nullability.Create(run.ReturnParameter).ReadState);
        Assert.AreEqual(NullabilityState.NotNull, nullability.Create(run.GetParameters()[0]).ReadState);

        MethodInfo check = typeof(ScenarioTest).GetMethod(nameof(ScenarioTest.CheckAsync))
            ?? throw new AssertFailedException("CheckAsync is missing.");
        Assert.AreEqual(NullabilityState.NotNull, nullability.Create(check.ReturnParameter).ReadState);
        Assert.AreEqual(NullabilityState.NotNull, nullability.Create(check.GetParameters()[0]).ReadState);
        Assert.AreEqual(NullabilityState.Nullable, nullability.Create(check.GetParameters()[1]).ReadState);

        PropertyInfo suiteStack = typeof(SuiteException).GetProperty(nameof(SuiteException.StackTrace))
            ?? throw new AssertFailedException("SuiteException.StackTrace is missing.");
        PropertyInfo assertionStack = typeof(ScenarioAssertionException).GetProperty(nameof(ScenarioAssertionException.StackTrace))
            ?? throw new AssertFailedException("ScenarioAssertionException.StackTrace is missing.");
        PropertyInfo reportStack = typeof(ReportException).GetProperty(nameof(ReportException.StackTrace))
            ?? throw new AssertFailedException("ReportException.StackTrace is missing.");
        Assert.AreEqual(NullabilityState.Nullable, nullability.Create(suiteStack).ReadState);
        Assert.AreEqual(NullabilityState.Nullable, nullability.Create(assertionStack).ReadState);
        Assert.AreEqual(NullabilityState.Nullable, nullability.Create(reportStack).ReadState);
    }

    private static void AssertParameterNullability(
        Type owner,
        MethodBase method,
        IEnumerable<ParameterInfo> parameters,
        NullabilityInfoContext nullability)
    {
        foreach (ParameterInfo parameter in parameters)
        {
            var expected = owner == typeof(ScenarioTest) &&
                method.Name == nameof(ScenarioTest.CheckAsync) &&
                parameter.Position == 1
                ? NullabilityState.Nullable
                : NullabilityState.NotNull;
            AssertNullability(
                parameter.ParameterType,
                nullability.Create(parameter),
                expected,
                $"{owner.FullName}.{method.Name} parameter {parameter.Name}");
        }
    }

    private static void AssertNullability(
        Type type,
        NullabilityInfo actual,
        NullabilityState expected,
        string identity)
    {
        if (!type.IsValueType)
        {
            Assert.AreEqual(expected, actual.ReadState, identity);
        }

        Type? elementType = type.GetElementType();
        if (elementType is not null)
        {
            Assert.IsNotNull(actual.ElementType, $"{identity} element nullability is missing.");
            AssertNullability(
                elementType,
                actual.ElementType,
                NullabilityState.NotNull,
                $"{identity} element");
        }

        Type[] genericTypes = type.IsGenericType ? type.GetGenericArguments() : Array.Empty<Type>();
        Assert.AreEqual(genericTypes.Length, actual.GenericTypeArguments.Length, $"{identity} generic arity");
        for (int index = 0; index < genericTypes.Length; index++)
        {
            AssertNullability(
                genericTypes[index],
                actual.GenericTypeArguments[index],
                NullabilityState.NotNull,
                $"{identity} generic argument {index}");
        }
    }

    private static void AssertEnum<T>(params (string Name, int Value)[] expected)
        where T : struct, Enum
    {
        FieldInfo[] fields = typeof(T)
            .GetFields(BindingFlags.Public | BindingFlags.Static | BindingFlags.DeclaredOnly)
            .OrderBy(static field => field.MetadataToken)
            .ToArray();
        string[] names = fields.Select(static field => field.Name).ToArray();
        int[] values = fields
            .Select(static field => Convert.ToInt32(field.GetRawConstantValue()))
            .ToArray();
        CollectionAssert.AreEqual(expected.Select(static item => item.Name).ToArray(), names);
        CollectionAssert.AreEqual(expected.Select(static item => item.Value).ToArray(), values);
        Assert.IsFalse(Enum.IsDefined(typeof(T), 0));
    }

    private static void AssertMembers<T>(params string[] expected)
    {
        AssertMembers(typeof(T), expected);
    }

    private static void AssertMembers(Type type, params string[] expected)
    {
        string[] actual = type
            .GetMembers(BindingFlags.Public | BindingFlags.Instance | BindingFlags.Static | BindingFlags.DeclaredOnly)
            .Where(static member => member switch
            {
                MethodInfo method => !method.IsSpecialName,
                ConstructorInfo => true,
                PropertyInfo => true,
                FieldInfo => true,
                _ => false,
            })
            .Select(MemberSignature)
            .Order(StringComparer.Ordinal)
            .ToArray();
        string[] orderedExpected = expected.Order(StringComparer.Ordinal).ToArray();
        CollectionAssert.AreEqual(orderedExpected, actual, type.FullName);
    }

    private static string MemberSignature(MemberInfo member)
    {
        return member switch
        {
            ConstructorInfo constructor => $"ctor({ParameterTypes(constructor.GetParameters())})",
            PropertyInfo property => $"property {TypeName(property.PropertyType)} {property.Name}",
            MethodInfo method => $"{(method.IsStatic ? "static" : "instance")} {TypeName(method.ReturnType)} {method.Name}({ParameterTypes(method.GetParameters())})",
            FieldInfo field when field.IsLiteral => $"const {TypeName(field.FieldType)} {field.Name}={field.GetRawConstantValue()}",
            FieldInfo field => $"field {TypeName(field.FieldType)} {field.Name}",
            _ => throw new AssertFailedException($"Unsupported member kind: {member.MemberType}"),
        };
    }

    private static string ParameterTypes(IEnumerable<ParameterInfo> parameters)
    {
        return string.Join(",", parameters.Select(static parameter => TypeName(parameter.ParameterType)));
    }

    private static string TypeName(Type type)
    {
        if (type == typeof(void)) return "void";
        if (type == typeof(bool)) return "bool";
        if (type == typeof(byte)) return "byte";
        if (type == typeof(uint)) return "uint";
        if (type == typeof(ulong)) return "ulong";
        if (type == typeof(int)) return "int";
        if (type == typeof(string)) return "string";
        if (type == typeof(byte[])) return "byte[]";
        if (!type.IsGenericType) return type.Name;

        string name = type.Name[..type.Name.IndexOf('`', StringComparison.Ordinal)];
        return $"{name}<{string.Join(",", type.GetGenericArguments().Select(TypeName))}>";
    }

    private static void AssertStaticClass(Type type)
    {
        Assert.IsTrue(type.IsAbstract && type.IsSealed, type.FullName);
    }

    private static void AssertSealedClass(Type type)
    {
        Assert.IsTrue(type.IsClass && type.IsSealed && !type.IsAbstract, type.FullName);
    }

    private static void AssertCancellationDefault(Type owner, string methodName, int parameterIndex)
    {
        MethodInfo method = owner.GetMethod(methodName, BindingFlags.Public | BindingFlags.Static)
            ?? throw new AssertFailedException($"{owner.FullName}.{methodName} is missing.");
        ParameterInfo parameter = method.GetParameters()[parameterIndex];
        Assert.IsTrue(parameter.HasDefaultValue);
        Assert.IsNull(parameter.DefaultValue);
    }
}
