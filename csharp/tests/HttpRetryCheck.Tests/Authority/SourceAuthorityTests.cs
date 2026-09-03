using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Reflection.Metadata;
using System.Reflection.PortableExecutable;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text.Json;
using System.Xml.Linq;
using HttpRetryCheck.V1;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Authority;

[TestClass]
public sealed class SourceAuthorityTests
{
    [TestMethod]
    public void ProductionSourceInventoryMatchesPolicy()
    {
        string sourceRoot = Path.Combine(RepositoryRoot(), "csharp", "src", "HttpRetryCheck");
        string[] actual = Directory
            .EnumerateFiles(sourceRoot, "*.cs", SearchOption.AllDirectories)
            .Where(static path => !path.Contains($"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}", StringComparison.Ordinal))
            .Select(path => Path.GetRelativePath(sourceRoot, path).Replace(Path.DirectorySeparatorChar, '/'))
            .Order(StringComparer.Ordinal)
            .ToArray();
        Assert.IsNotEmpty(actual);

        string[] compiled = CompiledProductionDocuments(sourceRoot);
        string[] expectedCompiled = actual
            .Append("obj/Release/net10.0/.NETCoreApp,Version=v10.0.AssemblyAttributes.cs")
            .Append("obj/Release/net10.0/HttpRetryCheck.AssemblyInfo.cs")
            .Order(StringComparer.Ordinal)
            .ToArray();
        CollectionAssert.AreEqual(expectedCompiled, compiled);

        string[] forbidden =
        [
            "System.Diagnostics.Process",
            "ProcessStartInfo",
            "System.Reflection",
            "Assembly.Load",
            "AssemblyLoadContext",
            "Activator.CreateInstance",
            "[DllImport",
            "[LibraryImport",
            "NativeLibrary.",
            "Marshal.",
            "System.Environment",
            "Environment.",
            "System.Net.Dns",
            "Dns.",
            "new HttpClient",
            "HttpClient.DefaultProxy",
            "WebRequest.DefaultWebProxy",
            "WebProxy",
            "File.Write",
            "File.Create",
            "File.Open",
            "Directory.Create",
            "Directory.Delete",
            "FileStream",
            "System.IO.Pipes",
            "NamedPipe",
            "TcpClient",
            "http://localhost",
            "https://localhost",
            "System.Management",
            "Microsoft.Win32",
            "Microsoft.CodeCoverage.Instrumentation",
        ];

        foreach (string relativePath in actual)
        {
            string path = Path.Combine(sourceRoot, relativePath);
            string source = File.ReadAllText(path);
            foreach (string token in forbidden)
            {
                Assert.IsFalse(
                    source.Contains(token, StringComparison.Ordinal),
                    $"{relativePath} contains forbidden authority token {token}.");
            }
        }
    }

    [TestMethod]
    public void ProductionAssemblyUsesOnlyBclReferencesAndManagedCalls()
    {
        Assembly assembly = typeof(ScenarioSuite).Assembly;
        string[] nonBclReferences = assembly
            .GetReferencedAssemblies()
            .Select(static name => name.Name ?? string.Empty)
            .Where(static name => !name.Equals("netstandard", StringComparison.Ordinal) &&
                !name.StartsWith("System.", StringComparison.Ordinal))
            .Order(StringComparer.Ordinal)
            .ToArray();
        CollectionAssert.AreEqual(Array.Empty<string>(), nonBclReferences);

        Assert.IsFalse(assembly.GetCustomAttributes<InternalsVisibleToAttribute>().Any());
        foreach (Type type in assembly.GetTypes())
        {
            if (type.Namespace?.StartsWith(
                    "Microsoft.CodeCoverage.Instrumentation",
                    StringComparison.Ordinal) is true)
            {
                continue;
            }

            bool compilerGenerated = type.GetCustomAttribute<CompilerGeneratedAttribute>() is not null;
            foreach (FieldInfo field in type.GetFields(
                BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Static | BindingFlags.DeclaredOnly))
            {
                Assert.IsTrue(
                    compilerGenerated ||
                    field.GetCustomAttribute<CompilerGeneratedAttribute>() is not null ||
                    field.IsLiteral ||
                    field.IsInitOnly,
                    $"Mutable static field is forbidden: {type.FullName}.{field.Name}");
            }

            foreach (MethodInfo method in type.GetMethods(
                BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Static | BindingFlags.Instance | BindingFlags.DeclaredOnly))
            {
                Assert.AreEqual(
                    0,
                    (int)(method.Attributes & MethodAttributes.PinvokeImpl),
                    $"P/Invoke is forbidden: {type.FullName}.{method.Name}");
                Assert.IsNull(method.GetCustomAttribute<DllImportAttribute>());
                Assert.IsNull(method.GetCustomAttribute<UnmanagedCallersOnlyAttribute>());
            }
        }
    }

    [TestMethod]
    public void ToolchainProjectsAndLockedDependenciesMatchPolicy()
    {
        string root = RepositoryRoot();
        using (JsonDocument global = JsonDocument.Parse(File.ReadAllBytes(Path.Combine(root, "global.json"))))
        {
            JsonElement document = global.RootElement;
            CollectionAssert.AreEqual(new[] { "sdk" }, document.EnumerateObject().Select(static property => property.Name).ToArray());
            JsonElement sdk = document.GetProperty("sdk");
            CollectionAssert.AreEqual(
                new[] { "version", "rollForward", "allowPrerelease" },
                sdk.EnumerateObject().Select(static property => property.Name).ToArray());
            Assert.AreEqual("10.0.303", sdk.GetProperty("version").GetString());
            Assert.AreEqual("disable", sdk.GetProperty("rollForward").GetString());
            Assert.IsFalse(sdk.GetProperty("allowPrerelease").GetBoolean());
        }

        string productionProject = Path.Combine(root, "csharp", "src", "HttpRetryCheck", "HttpRetryCheck.csproj");
        XDocument build = XDocument.Load(
            Path.Combine(root, "csharp", "Directory.Build.props"),
            LoadOptions.PreserveWhitespace);
        XElement buildRoot = AssertExactProjectChildren(build, "PropertyGroup", "PropertyGroup");
        AssertExactAttributes(buildRoot);
        XElement[] buildGroups = buildRoot.Elements().ToArray();
        AssertExactProperties(
            buildGroups[0],
            ("TargetFramework", "net10.0"),
            ("LangVersion", "14.0"),
            ("Nullable", "enable"),
            ("ImplicitUsings", "disable"),
            ("EnableNETAnalyzers", "true"),
            ("Deterministic", "true"),
            ("DebugType", "embedded"),
            ("CheckForOverflowUnderflow", "true"),
            ("RestorePackagesWithLockFile", "true"),
            ("RestoreEnablePackagePruning", "true"),
            ("DisableImplicitLibraryPacksFolder", "true"),
            ("NuGetAudit", "true"));
        AssertExactConditionedProperties(
            buildGroups[1],
            "'$(ContinuousIntegrationBuild)' == 'true'",
            ("TreatWarningsAsErrors", "true"),
            ("WarningsAsErrors", "$(WarningsAsErrors);NU1900;NU1901;NU1902;NU1903;NU1904;NU1905"),
            ("AnalysisLevel", "latest"),
            ("NuGetAuditMode", "all"),
            ("NuGetAuditLevel", "moderate"));

        XDocument production = XDocument.Load(productionProject, LoadOptions.PreserveWhitespace);
        XElement productionRoot = AssertExactProjectChildren(production, "PropertyGroup", "ItemGroup");
        AssertExactAttributes(productionRoot, ("Sdk", "Microsoft.NET.Sdk"));
        XElement productionProperties = productionRoot.Elements().First();
        XElement versionPrefix = productionProperties.Element("VersionPrefix")
            ?? throw new AssertFailedException("VersionPrefix is missing.");
        Assert.IsTrue(
            System.Text.RegularExpressions.Regex.IsMatch(
                versionPrefix.Value,
                @"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$",
                System.Text.RegularExpressions.RegexOptions.CultureInvariant),
            "VersionPrefix must be a SemVer-compatible release version.");
        versionPrefix.Value = "<semver>";
        AssertExactProperties(
            productionProperties,
            ("AssemblyName", "HttpRetryCheck"),
            ("RootNamespace", "HttpRetryCheck"),
            ("DebugType", "portable"),
            ("PackageId", "HttpRetryCheck"),
            ("VersionPrefix", "<semver>"),
            ("IsPackable", "true"),
            ("Authors", "Aleksei Kislov"),
            ("Description", "Tests .NET HTTP clients against six controlled HTTP/1 retry scenarios and produces deterministic evidence."),
            ("PackageLicenseExpression", "Apache-2.0"),
            ("PackageProjectUrl", "https://github.com/aleksei-kislov/http-retry-check"),
            ("RepositoryUrl", "https://github.com/aleksei-kislov/http-retry-check"),
            ("RepositoryType", "git"),
            ("PackageReadmeFile", "PACKAGE.md"),
            ("PublishRepositoryUrl", "true"),
            ("EmbedUntrackedSources", "true"),
            ("IncludeSymbols", "true"),
            ("SymbolPackageFormat", "snupkg"),
            ("IsAotCompatible", "true"),
            ("IsTrimmable", "true"),
            ("GenerateDocumentationFile", "true"));
        XElement productionItems = productionRoot.Elements().Last();
        Assert.AreEqual("ItemGroup", productionItems.Name.LocalName);
        AssertExactAttributes(productionItems);
        XElement[] packageFiles = productionItems.Elements().ToArray();
        CollectionAssert.AreEqual(new[] { "None", "None", "None" }, packageFiles.Select(static item => item.Name.LocalName).ToArray());
        AssertExactAttributes(packageFiles[0], ("Update", "PACKAGE.md"), ("Pack", "true"), ("PackagePath", ""));
        AssertExactAttributes(packageFiles[1], ("Include", "../../../LICENSE"), ("Pack", "true"), ("PackagePath", ""));
        AssertExactAttributes(packageFiles[2], ("Include", "../../../NOTICE"), ("Pack", "true"), ("PackagePath", ""));
        CollectionAssert.AreEqual(
            new[] { Path.GetFullPath(Path.Combine(root, "csharp", "Directory.Build.props")) },
            Directory.EnumerateFiles(Path.Combine(root, "csharp"), "Directory.Build.props", SearchOption.AllDirectories)
                .Where(static path => !path.Contains(
                    $"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}",
                    StringComparison.Ordinal))
                .Select(Path.GetFullPath)
                .Order(StringComparer.Ordinal)
                .ToArray());
        Assert.HasCount(
            0,
            Directory.EnumerateFiles(root, "Directory.Build.targets", SearchOption.AllDirectories)
                .Where(static path => !path.Contains(
                    $"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}",
                    StringComparison.Ordinal)));

        XDocument packages = XDocument.Load(
            Path.Combine(root, "csharp", "Directory.Packages.props"),
            LoadOptions.PreserveWhitespace);
        XElement packagesRoot = AssertExactProjectChildren(packages, "PropertyGroup", "ItemGroup");
        AssertExactAttributes(packagesRoot);
        CollectionAssert.AreEqual(
            new[] { Path.GetFullPath(Path.Combine(root, "csharp", "Directory.Packages.props")) },
            Directory.EnumerateFiles(Path.Combine(root, "csharp"), "Directory.Packages.props", SearchOption.AllDirectories)
                .Where(static path => !path.Contains(
                    $"{Path.DirectorySeparatorChar}obj{Path.DirectorySeparatorChar}",
                    StringComparison.Ordinal))
                .Select(Path.GetFullPath)
                .Order(StringComparer.Ordinal)
                .ToArray());
        XElement[] packageChildren = packagesRoot.Elements().ToArray();
        AssertExactProperties(
            packageChildren[0],
            ("ManagePackageVersionsCentrally", "true"),
            ("CentralPackageTransitivePinningEnabled", "false"),
            ("CentralPackageVersionOverrideEnabled", "false"));
        AssertExactAttributes(packageChildren[1]);
        XElement[] versions = packageChildren[1].Elements().ToArray();
        CollectionAssert.AreEqual(
            new[] { "PackageVersion", "PackageVersion", "PackageVersion" },
            versions.Select(static element => element.Name.LocalName).ToArray());
        AssertExactAttributes(versions[0], ("Include", "Microsoft.NET.Test.Sdk"), ("Version", "[18.9.0]"));
        AssertExactAttributes(versions[1], ("Include", "MSTest.TestAdapter"), ("Version", "[4.4.0]"));
        AssertExactAttributes(versions[2], ("Include", "MSTest.TestFramework"), ("Version", "[4.4.0]"));
        foreach (XElement version in versions)
        {
            Assert.HasCount(0, version.Elements());
        }

        XDocument tests = XDocument.Load(
            Path.Combine(root, "csharp", "tests", "HttpRetryCheck.Tests", "HttpRetryCheck.Tests.csproj"),
            LoadOptions.PreserveWhitespace);
        string[] testReferences = tests.Descendants("PackageReference")
            .Select(static element => $"{element.Attribute("Include")?.Value}:{element.Attribute("PrivateAssets")?.Value}")
            .ToArray();
        CollectionAssert.AreEqual(
            new[]
            {
                "Microsoft.NET.Test.Sdk:all",
                "MSTest.TestAdapter:all",
                "MSTest.TestFramework:all",
            },
            testReferences);
        XElement[] projectReferences = tests.Descendants("ProjectReference").ToArray();
        Assert.HasCount(1, projectReferences);
        Assert.AreEqual(
            "../../src/HttpRetryCheck/HttpRetryCheck.csproj",
            projectReferences[0].Attribute("Include")?.Value);

        AssertProductionToolLock(Path.Combine(root, "csharp", "src", "HttpRetryCheck", "packages.lock.json"));
        AssertTestLock(Path.Combine(root, "csharp", "tests", "HttpRetryCheck.Tests", "packages.lock.json"));
        AssertNuGetConfiguration(Path.Combine(root, "csharp", "NuGet.Config"));
    }

    [TestMethod]
    public void CSharpScaffoldMatchesTheEmbeddedTemplate()
    {
        string root = RepositoryRoot();
        AssertFileIdentity(
            Path.Combine(root, "internal", "httpcheck", "v1", "scaffold", "templates", "csharp", "HttpRetryTests.cs"),
            579,
            "9ca5036883b0ac6c2cf50cdeae8fdb2725a13eeaf14d21ba3867467e0ee4e0e7");
        AssertFileIdentity(
            Path.Combine(root, "internal", "httpcheck", "v1", "scaffold", "templates", "csharp", "README.md"),
            567,
            "dcc2970c231657ce8060f667b4877c1bcfa34eb38e8ff49681136a3b5b25e6ec");

        XDocument testProject = XDocument.Load(
            Path.Combine(root, "csharp", "tests", "HttpRetryCheck.Tests", "HttpRetryCheck.Tests.csproj"),
            LoadOptions.PreserveWhitespace);
        XElement[] linked = testProject.Descendants("Compile")
            .Where(static element => element.Attribute("Link") is not null)
            .ToArray();
        Assert.HasCount(1, linked);
        Assert.AreEqual("Scaffold/HttpRetryTests.cs", linked[0].Attribute("Link")?.Value);
        const string expectedInclude =
            "../../../internal/httpcheck/v1/scaffold/templates/csharp/HttpRetryTests.cs";
        Assert.AreEqual(expectedInclude, linked[0].Attribute("Include")?.Value);
        string projectDirectory = Path.GetDirectoryName(
            Path.Combine(root, "csharp", "tests", "HttpRetryCheck.Tests", "HttpRetryCheck.Tests.csproj"))
            ?? throw new AssertFailedException("Test project directory is unavailable.");
        string linkedPath = Path.GetFullPath(expectedInclude, projectDirectory);
        string canonicalPath = Path.GetFullPath(Path.Combine(
            root,
            "internal",
            "httpcheck",
            "v1",
            "scaffold",
            "templates",
            "csharp",
            "HttpRetryTests.cs"));
        Assert.AreEqual(canonicalPath, linkedPath);
    }

    private static string RepositoryRoot()
    {
        DirectoryInfo? directory = new(AppContext.BaseDirectory);
        while (directory is not null)
        {
            if (File.Exists(Path.Combine(directory.FullName, "global.json")) &&
                Directory.Exists(Path.Combine(directory.FullName, "csharp")) &&
                Directory.Exists(Path.Combine(directory.FullName, "conformance")))
            {
                return directory.FullName;
            }

            directory = directory.Parent;
        }

        throw new AssertFailedException("Repository root is unavailable.");
    }

    private static XElement AssertExactProjectChildren(
        XDocument document,
        params string[] expectedChildren)
    {
        XElement root = document.Root
            ?? throw new AssertFailedException("MSBuild project root is missing.");
        Assert.AreEqual("Project", root.Name.LocalName);
        CollectionAssert.AreEqual(
            expectedChildren,
            root.Elements().Select(static element => element.Name.LocalName).ToArray());
        return root;
    }

    private static void AssertExactProperties(
        XElement group,
        params (string Name, string Value)[] expected)
    {
        Assert.AreEqual("PropertyGroup", group.Name.LocalName);
        Assert.HasCount(0, group.Attributes());
        AssertExactPropertyChildren(group, expected);
    }

    private static void AssertExactConditionedProperties(
        XElement group,
        string condition,
        params (string Name, string Value)[] expected)
    {
        Assert.AreEqual("PropertyGroup", group.Name.LocalName);
        AssertExactAttributes(group, ("Condition", condition));
        AssertExactPropertyChildren(group, expected);
    }

    private static void AssertExactPropertyChildren(
        XElement group,
        params (string Name, string Value)[] expected)
    {
        XElement[] actual = group.Elements().ToArray();
        CollectionAssert.AreEqual(
            expected.Select(static item => item.Name).ToArray(),
            actual.Select(static element => element.Name.LocalName).ToArray());
        CollectionAssert.AreEqual(
            expected.Select(static item => item.Value).ToArray(),
            actual.Select(static element => element.Value).ToArray());
        foreach (XElement property in actual)
        {
            Assert.HasCount(0, property.Attributes());
            Assert.HasCount(0, property.Elements());
        }
    }

    private static string[] CompiledProductionDocuments(string sourceRoot)
    {
        string sourcePrefix = sourceRoot.Replace('\\', '/').TrimEnd('/') + "/";
        const string ciSourcePrefix = "/_/csharp/src/HttpRetryCheck/";
        using var stream = File.OpenRead(typeof(ScenarioSuite).Assembly.Location);
        using var peReader = new PEReader(stream);
        DebugDirectoryEntry[] debugEntries = peReader.ReadDebugDirectory().ToArray();
        DebugDirectoryEntry[] embedded = debugEntries
            .Where(static entry => entry.Type == DebugDirectoryEntryType.EmbeddedPortablePdb)
            .ToArray();
        Assert.HasCount(0, embedded);
        Assert.HasCount(1, debugEntries.Where(static entry => entry.Type == DebugDirectoryEntryType.CodeView));
        string pdbPath = Path.ChangeExtension(typeof(ScenarioSuite).Assembly.Location, ".pdb");
        Assert.IsTrue(File.Exists(pdbPath), "The production portable PDB is missing.");
        using var pdbStream = File.OpenRead(pdbPath);
        using MetadataReaderProvider provider = MetadataReaderProvider.FromPortablePdbStream(pdbStream);
        MetadataReader reader = provider.GetMetadataReader();
        var documents = new List<string>();
        foreach (DocumentHandle handle in reader.Documents)
        {
            string path = reader.GetString(reader.GetDocument(handle).Name).Replace('\\', '/');
            if (path.StartsWith(sourcePrefix, StringComparison.Ordinal))
            {
                documents.Add(path[sourcePrefix.Length..]);
                continue;
            }

            Assert.IsTrue(
                path.StartsWith(ciSourcePrefix, StringComparison.Ordinal),
                $"Compiled source escaped the production project: {path}");
            documents.Add(path[ciSourcePrefix.Length..]);
        }

        return documents.Order(StringComparer.Ordinal).ToArray();
    }

    private static void AssertProductionToolLock(string path)
    {
        using JsonDocument document = JsonDocument.Parse(File.ReadAllBytes(path));
        JsonElement frameworks = document.RootElement.GetProperty("dependencies");
        CollectionAssert.AreEqual(new[] { "net10.0" }, frameworks.EnumerateObject().Select(static property => property.Name).ToArray());
        JsonProperty[] packages = frameworks.GetProperty("net10.0").EnumerateObject().ToArray();
        Assert.HasCount(1, packages);
        AssertDirect(packages, "Microsoft.NET.ILLink.Tasks", "[10.0.11, )", "10.0.11");
    }

    private static void AssertTestLock(string path)
    {
        using JsonDocument document = JsonDocument.Parse(File.ReadAllBytes(path));
        JsonElement dependencies = document.RootElement.GetProperty("dependencies").GetProperty("net10.0");
        JsonProperty[] packages = dependencies.EnumerateObject()
            .Where(static property => !property.Value.GetProperty("type").GetString()!.Equals("Project", StringComparison.Ordinal))
            .ToArray();
        Assert.HasCount(12, packages);

        AssertDirect(packages, "Microsoft.NET.Test.Sdk", "[18.9.0, 18.9.0]", "18.9.0");
        AssertDirect(packages, "MSTest.TestAdapter", "[4.4.0, 4.4.0]", "4.4.0");
        AssertDirect(packages, "MSTest.TestFramework", "[4.4.0, 4.4.0]", "4.4.0");
        Assert.AreEqual(3, packages.Count(static package => package.Value.GetProperty("type").GetString() == "Direct"));
        Assert.AreEqual(9, packages.Count(static package => package.Value.GetProperty("type").GetString() == "Transitive"));
    }

    private static void AssertNuGetConfiguration(string path)
    {
        XDocument document = XDocument.Load(path, LoadOptions.PreserveWhitespace);
        XElement root = document.Root
            ?? throw new AssertFailedException("NuGet configuration root is missing.");
        Assert.AreEqual("configuration", root.Name.LocalName);
        Assert.HasCount(0, root.Attributes());
        XElement[] sections = root.Elements().ToArray();
        CollectionAssert.AreEqual(
            new[] { "packageSources", "auditSources", "packageSourceMapping", "disabledPackageSources" },
            sections.Select(static section => section.Name.LocalName).ToArray());

        AssertSourceSection(sections[0]);
        AssertSourceSection(sections[1]);

        XElement mapping = sections[2];
        Assert.HasCount(0, mapping.Attributes());
        XElement[] mappingChildren = mapping.Elements().ToArray();
        Assert.HasCount(1, mappingChildren);
        Assert.AreEqual("packageSource", mappingChildren[0].Name.LocalName);
        AssertExactAttributes(mappingChildren[0], ("key", "nuget.org"));
        XElement[] patterns = mappingChildren[0].Elements().ToArray();
        Assert.HasCount(1, patterns);
        Assert.AreEqual("package", patterns[0].Name.LocalName);
        AssertExactAttributes(patterns[0], ("pattern", "*"));

        Assert.HasCount(0, sections[3].Attributes());
        XElement[] disabled = sections[3].Elements().ToArray();
        Assert.HasCount(1, disabled);
        Assert.AreEqual("clear", disabled[0].Name.LocalName);
        Assert.HasCount(0, disabled[0].Attributes());
    }

    private static void AssertSourceSection(XElement section)
    {
        Assert.HasCount(0, section.Attributes());
        XElement[] children = section.Elements().ToArray();
        Assert.HasCount(2, children);
        Assert.AreEqual("clear", children[0].Name.LocalName);
        Assert.HasCount(0, children[0].Attributes());
        Assert.AreEqual("add", children[1].Name.LocalName);
        AssertExactAttributes(
            children[1],
            ("key", "nuget.org"),
            ("value", "https://api.nuget.org/v3/index.json"),
            ("protocolVersion", "3"));
    }

    private static void AssertExactAttributes(
        XElement element,
        params (string Name, string Value)[] expected)
    {
        XAttribute[] actual = element.Attributes().ToArray();
        CollectionAssert.AreEqual(
            expected.Select(static item => item.Name).ToArray(),
            actual.Select(static attribute => attribute.Name.LocalName).ToArray());
        CollectionAssert.AreEqual(
            expected.Select(static item => item.Value).ToArray(),
            actual.Select(static attribute => attribute.Value).ToArray());
    }

    private static void AssertDirect(JsonProperty[] packages, string name, string requested, string resolved)
    {
        JsonElement package = packages.Single(item => item.NameEquals(name)).Value;
        Assert.AreEqual("Direct", package.GetProperty("type").GetString());
        Assert.AreEqual(requested, package.GetProperty("requested").GetString());
        Assert.AreEqual(resolved, package.GetProperty("resolved").GetString());
    }

    private static void AssertFileIdentity(string path, int expectedSize, string expectedSha256)
    {
        byte[] contents = File.ReadAllBytes(path);
        Assert.HasCount(expectedSize, contents);
        Assert.AreEqual(expectedSha256, Convert.ToHexStringLower(SHA256.HashData(contents)));
    }
}
