using System;
using System.Globalization;
using System.Text;

namespace HttpRetryCheck.V1.Reporting;

public static partial class ScenarioReports
{
    /// <summary>Returns the deterministic JUnit XML projection.</summary>
    public static byte[] JUnit(Report report)
    {
        try
        {
            Validate(report);
            var encoded = ReportProjectionCodec.JUnit(report);
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

    /// <summary>Returns the deterministic GitHub-flavored Markdown projection.</summary>
    public static byte[] GitHubSummary(Report report)
    {
        try
        {
            Validate(report);
            var encoded = ReportProjectionCodec.GitHubSummary(report);
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
}

internal static class ReportProjectionCodec
{
    private const string JUnitSuiteName = "HTTP Retry Check scenario suite";
    private static readonly UTF8Encoding Utf8 = new(encoderShouldEmitUTF8Identifier: false, throwOnInvalidBytes: true);

    internal static byte[] JUnit(Report report)
    {
        var builder = new StringBuilder(4096);
        builder.Append("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n");
        builder.Append("<testsuite name=\"");
        AppendXmlAttribute(builder, JUnitSuiteName);
        builder.Append("\" tests=\"").Append(report.Summary.Scenarios.ToString(CultureInfo.InvariantCulture));
        builder.Append("\" failures=\"").Append(report.Summary.Failed.ToString(CultureInfo.InvariantCulture));
        builder.Append("\" errors=\"").Append(report.Summary.Inconclusive.ToString(CultureInfo.InvariantCulture)).Append("\">\n");
        builder.Append("  <properties>\n");
        AppendProperty(builder, "schema_version", ScenarioReports.SchemaVersion);
        AppendProperty(builder, "suite_identity", ScenarioReports.SuiteIdentity);
        AppendProperty(builder, "explanation_identity", ScenarioReports.ExplanationIdentity);
        AppendProperty(builder, "claim_ceiling", ScenarioReports.ClaimCeiling);
        AppendProperty(builder, "outcome", ReportWire.OutcomeName(report.Outcome));
        builder.Append("  </properties>\n");
        foreach (var scenario in report.Scenarios)
        {
            builder.Append("  <testcase name=\"");
            AppendXmlAttribute(builder, scenario.ScenarioText);
            builder.Append("\" classname=\"");
            AppendXmlAttribute(builder, ScenarioReports.SchemaVersion);
            builder.Append("\"");
            if (scenario.Assessment == Assessment.NoUnsafeBehaviorObserved)
            {
                builder.Append("></testcase>\n");
                continue;
            }

            builder.Append(">\n    <");
            builder.Append(scenario.Assessment == Assessment.UnsafeBehaviorObserved ? "failure" : "error");
            builder.Append(" message=\"");
            AppendXmlAttribute(builder, scenario.AssessmentText);
            builder.Append("\">");
            for (var index = 0; index < scenario.Findings.Count; index++)
            {
                if (index != 0)
                {
                    builder.Append("&#xA;");
                }
                AppendXmlText(builder, scenario.Findings[index].Text);
            }
            builder.Append("</");
            builder.Append(scenario.Assessment == Assessment.UnsafeBehaviorObserved ? "failure" : "error");
            builder.Append(">\n  </testcase>\n");
        }
        builder.Append("</testsuite>\n");
        return Utf8.GetBytes(builder.ToString());
    }

    internal static byte[] GitHubSummary(Report report)
    {
        var builder = new StringBuilder(4096);
        builder.Append("# HTTP Retry Check report\n\n> ");
        builder.Append(ScenarioReports.ClaimCeiling);
        builder.Append("\n\n**Result:** `").Append(ReportWire.OutcomeName(report.Outcome));
        builder.Append("` — ").Append(report.Summary.Passed.ToString(CultureInfo.InvariantCulture));
        builder.Append(" passed, ").Append(report.Summary.Failed.ToString(CultureInfo.InvariantCulture));
        builder.Append(" unsafe, ").Append(report.Summary.Inconclusive.ToString(CultureInfo.InvariantCulture));
        builder.Append(" inconclusive.\n\n| Scenario | Result | Findings |\n| --- | --- | --- |\n");
        foreach (var scenario in report.Scenarios)
        {
            builder.Append("| `");
            AppendMarkdownCell(builder, ReportWire.ScenarioName(scenario.Scenario));
            builder.Append("` | ");
            builder.Append(MarkdownResult(scenario.Assessment));
            builder.Append(" | ");
            if (scenario.Findings.Count == 0)
            {
                builder.Append("None");
            }
            else
            {
                for (var index = 0; index < scenario.Findings.Count; index++)
                {
                    if (index != 0)
                    {
                        builder.Append("<br>");
                    }
                    builder.Append('`');
                    AppendMarkdownCell(builder, ReportWire.FindingName(scenario.Findings[index].Code));
                    builder.Append("`: ");
                    AppendMarkdownCell(builder, scenario.Findings[index].Text);
                }
            }
            builder.Append(" |\n");
        }
        return Utf8.GetBytes(builder.ToString());
    }

    private static string MarkdownResult(Assessment assessment) => assessment switch
    {
        Assessment.NoUnsafeBehaviorObserved => "Pass",
        Assessment.UnsafeBehaviorObserved => "Unsafe",
        Assessment.Inconclusive => "Inconclusive",
        _ => throw ReportingFailure.Report(),
    };

    private static void AppendProperty(StringBuilder builder, string name, string value)
    {
        builder.Append("    <property name=\"");
        AppendXmlAttribute(builder, name);
        builder.Append("\" value=\"");
        AppendXmlAttribute(builder, value);
        builder.Append("\"></property>\n");
    }

    private static void AppendXmlAttribute(StringBuilder builder, string value)
    {
        foreach (var character in value)
        {
            switch (character)
            {
                case '&':
                    builder.Append("&amp;");
                    break;
                case '<':
                    builder.Append("&lt;");
                    break;
                case '>':
                    builder.Append("&gt;");
                    break;
                case '"':
                    builder.Append("&#34;");
                    break;
                case '\t':
                    builder.Append("&#x9;");
                    break;
                case '\n':
                    builder.Append("&#xA;");
                    break;
                case '\r':
                    builder.Append("&#xD;");
                    break;
                default:
                    builder.Append(character);
                    break;
            }
        }
    }

    private static void AppendXmlText(StringBuilder builder, string value)
    {
        foreach (var character in value)
        {
            switch (character)
            {
                case '&':
                    builder.Append("&amp;");
                    break;
                case '<':
                    builder.Append("&lt;");
                    break;
                case '>':
                    builder.Append("&gt;");
                    break;
                case '\t':
                    builder.Append("&#x9;");
                    break;
                case '\n':
                    builder.Append("&#xA;");
                    break;
                case '\r':
                    builder.Append("&#xD;");
                    break;
                default:
                    builder.Append(character);
                    break;
            }
        }
    }

    private static void AppendMarkdownCell(StringBuilder builder, string value)
    {
        foreach (var character in value)
        {
            switch (character)
            {
                case '&':
                    builder.Append("&amp;");
                    break;
                case '<':
                    builder.Append("&lt;");
                    break;
                case '>':
                    builder.Append("&gt;");
                    break;
                case '\\':
                    builder.Append("\\\\");
                    break;
                case '|':
                    builder.Append("\\|");
                    break;
                case '\r':
                case '\n':
                    builder.Append(' ');
                    break;
                default:
                    builder.Append(character);
                    break;
            }
        }
    }
}
