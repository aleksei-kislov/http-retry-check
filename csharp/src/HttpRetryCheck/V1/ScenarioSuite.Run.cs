using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1.Runtime;

namespace HttpRetryCheck.V1;

/// <summary>Runs and validates the six HTTP retry scenarios.</summary>
public static partial class ScenarioSuite
{
    /// <summary>Runs all six scenarios with a caller-provided client.</summary>
    /// <remarks>
    /// The token bounds the suite's listeners, connections, and scenario work.
    /// This method still waits for <see cref="HttpMessageInvoker.SendAsync"/>
    /// to return, so a handler that ignores the token can block completion after
    /// cancellation. It also waits for request-body activity started by the
    /// client to stop before reporting cleanup as verified. Unexpected exceptions
    /// from caller-owned handler or response code are rethrown after cleanup.
    /// </remarks>
    /// <param name="client">The configured client or handler chain to test.</param>
    /// <param name="cancellationToken">Cancels work that observes the token.</param>
    /// <returns>The validated six-scenario result.</returns>
    /// <exception cref="System.OperationCanceledException">
    /// The caller's token was cancelled. The exception is raised after cleanup.
    /// </exception>
    /// <exception cref="SuiteException">The call is invalid or the suite cannot run.</exception>
    public static Task<SuiteResult> RunAsync(
        HttpMessageInvoker client,
        CancellationToken cancellationToken = default)
    {
        return ScenarioRuntime.RunAsync(client, cancellationToken);
    }

    /// <summary>Runs all six scenarios with caller-selected runtime limits.</summary>
    /// <param name="client">The configured client or handler chain to test.</param>
    /// <param name="options">Timeout, observation-window, and attempt settings.</param>
    /// <param name="cancellationToken">Cancels work that observes the token.</param>
    /// <returns>The validated six-scenario result.</returns>
    /// <exception cref="System.OperationCanceledException">
    /// The caller's token was cancelled. The exception is raised after cleanup.
    /// </exception>
    /// <exception cref="SuiteException">The call is invalid or the suite cannot run.</exception>
    public static Task<SuiteResult> RunAsync(
        HttpMessageInvoker client,
        ScenarioOptions options,
        CancellationToken cancellationToken = default)
    {
        return ScenarioRuntime.RunAsync(client, options, cancellationToken);
    }
}
