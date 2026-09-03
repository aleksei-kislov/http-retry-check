using System;
using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1.Reporting;

namespace HttpRetryCheck.V1.Testing;

/// <summary>Provides a test-friendly wrapper around the six-scenario suite.</summary>
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

    /// <summary>Runs the suite with default options and fails unless all six scenarios pass.</summary>
    /// <param name="client">The configured client or handler chain to test.</param>
    /// <param name="writeLine">An optional destination for one diagnostic line per finding.</param>
    /// <param name="cancellationToken">Cancels work that observes the token.</param>
    /// <returns>A task that completes when all six scenarios pass.</returns>
    /// <exception cref="ScenarioAssertionException">The result is unsafe, inconclusive, or unavailable.</exception>
    /// <exception cref="OperationCanceledException">The caller's token was cancelled.</exception>
    public static Task CheckAsync(
        HttpMessageInvoker client,
        Action<string>? writeLine = null,
        CancellationToken cancellationToken = default)
    {
        return CheckCoreAsync(client, new ScenarioOptions(), writeLine, cancellationToken);
    }

    /// <summary>Runs the suite with selected options and fails unless all six scenarios pass.</summary>
    /// <param name="client">The configured client or handler chain to test.</param>
    /// <param name="options">Timeout, observation-window, and attempt settings.</param>
    /// <param name="writeLine">An optional destination for one diagnostic line per finding.</param>
    /// <param name="cancellationToken">Cancels work that observes the token.</param>
    /// <returns>A task that completes when all six scenarios pass.</returns>
    /// <exception cref="ScenarioAssertionException">The result is unsafe, inconclusive, or unavailable.</exception>
    /// <exception cref="OperationCanceledException">The caller's token was cancelled.</exception>
    public static Task CheckAsync(
        HttpMessageInvoker client,
        ScenarioOptions options,
        Action<string>? writeLine = null,
        CancellationToken cancellationToken = default)
    {
        return CheckCoreAsync(client, options, writeLine, cancellationToken);
    }

    private static async Task CheckCoreAsync(
        HttpMessageInvoker client,
        ScenarioOptions? options,
        Action<string>? writeLine,
        CancellationToken cancellationToken)
    {
        SuiteResult result;
        try
        {
            result = await ScenarioSuite.RunAsync(
                client,
                options!,
                cancellationToken).ConfigureAwait(false);
        }
        catch (OperationCanceledException exception) when (
            cancellationToken.IsCancellationRequested &&
            exception.CancellationToken == cancellationToken)
        {
            throw;
        }
        catch (SuiteException)
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
                            "HTTP Retry Check PASS " + ReportWire.ScenarioName(row.Scenario));
                        continue;
                    }

                    string status = row.Assessment == Assessment.UnsafeBehaviorObserved
                        ? "UNSAFE"
                        : "INCONCLUSIVE";
                    foreach (var finding in row.Findings)
                    {
                        writeLine(
                            "HTTP Retry Check " + status + " " +
                            ReportWire.ScenarioName(row.Scenario) + ": " +
                            ReportWire.FindingName(finding) + ": " +
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
