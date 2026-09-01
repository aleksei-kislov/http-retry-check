using System;
using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.V1.Testing;

public static class ScenarioTest
{
    private const string SuiteCouldNotRun =
        "HTTP Retry Check scenario suite could not run.";
    private const string InvalidResult =
        "HTTP Retry Check scenario suite returned an invalid result.";
    private const string DiagnosticsCouldNotBeWritten =
        "HTTP Retry Check scenario diagnostics could not be written.";
    private const string UnsafeResult =
        "HTTP Retry Check found unsafe behavior.";
    private const string InconclusiveResult =
        "HTTP Retry Check is inconclusive.";

    public static Task CheckAsync(
        HttpMessageInvoker client,
        Action<string>? writeLine = null,
        CancellationToken cancellationToken = default)
    {
        return CheckCoreAsync(client, writeLine, cancellationToken);
    }

    private static async Task CheckCoreAsync(
        HttpMessageInvoker client,
        Action<string>? writeLine,
        CancellationToken cancellationToken)
    {
        SuiteResult result;
        try
        {
            result = await ScenarioSuite.RunAsync(client, cancellationToken).ConfigureAwait(false);
        }
        catch (Exception exception) when (Runtime.RuntimeFailure.IsRecoverable(exception))
        {
            throw new ScenarioAssertionException(SuiteCouldNotRun);
        }

        try
        {
            ScenarioSuite.Validate(result);
        }
        catch (Exception exception) when (Runtime.RuntimeFailure.IsRecoverable(exception))
        {
            throw new ScenarioAssertionException(InvalidResult);
        }

        if (writeLine is not null)
        {
            try
            {
                foreach (var row in result.Scenarios)
                {
                    if (row.Assessment == Assessment.NoUnsafeBehaviorObserved)
                    {
                        writeLine(
                            "HTTP Retry Check PASS " + row.Scenario);
                        continue;
                    }

                    string status = row.Assessment == Assessment.UnsafeBehaviorObserved
                        ? "UNSAFE"
                        : "INCONCLUSIVE";
                    foreach (var finding in row.Findings)
                    {
                        writeLine(
                            "HTTP Retry Check " + status + " " + row.Scenario + ": " +
                            finding + ": " +
                            ScenarioExplanations.FindingText(finding));
                    }
                }
            }
            catch (Exception exception) when (Runtime.RuntimeFailure.IsRecoverable(exception))
            {
                throw new ScenarioAssertionException(DiagnosticsCouldNotBeWritten);
            }
        }

        switch (result.Assessment)
        {
            case Assessment.NoUnsafeBehaviorObserved:
                return;
            case Assessment.UnsafeBehaviorObserved:
                throw new ScenarioAssertionException(UnsafeResult);
            case Assessment.Inconclusive:
                throw new ScenarioAssertionException(InconclusiveResult);
            default:
                throw new ScenarioAssertionException(InvalidResult);
        }
    }
}
