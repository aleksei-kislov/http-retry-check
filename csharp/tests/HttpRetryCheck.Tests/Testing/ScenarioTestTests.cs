using System;
using System.Collections.Generic;
using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.Tests.Runtime;
using HttpRetryCheck.V1;
using HttpRetryCheck.V1.Testing;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Tests.Testing;

[TestClass]
public sealed class ScenarioTestTests
{
    [TestMethod]
    public async Task PositiveSuiteWritesSixFixedLinesAndReturnsNormally()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient();
        var lines = new List<string>();

        await ScenarioTest.CheckAsync(client, lines.Add);

        CollectionAssert.AreEqual(
            new[]
            {
                "HTTP Retry Check PASS accept_then_disconnect",
                "HTTP Retry Check PASS disconnect_before_acceptance",
                "HTTP Retry Check PASS changed_body_retry",
                "HTTP Retry Check PASS cross_origin_redirect_credentials",
                "HTTP Retry Check PASS retry_limit",
                "HTTP Retry Check PASS delayed_response",
            },
            lines);
    }

    [TestMethod]
    public async Task ConfiguredAttemptLimitFlowsThroughTheTestHelper()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryStatusHandler(3));

        await ScenarioTest.CheckAsync(client, new ScenarioOptions(attemptLimit: 3));
    }

    [TestMethod]
    public async Task UnsafeSuiteThrowsOnlyFixedUnsafeAssertion()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RetryingHandler());

        var exception = await CaptureAssertionAsync(() => ScenarioTest.CheckAsync(client));

        Assert.AreEqual(
            "HTTP Retry Check found unsafe behavior.",
            exception.Message);
        Assert.IsNull(exception.StackTrace);
        Assert.AreEqual(exception.Message, exception.ToString());
    }

    [TestMethod]
    public async Task UnsafeDiagnosticsUseCanonicalWireNames()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient(new RedirectExposureHandler());
        var lines = new List<string>();

        _ = await CaptureAssertionAsync(() => ScenarioTest.CheckAsync(client, lines.Add));

        CollectionAssert.Contains(
            lines,
            "HTTP Retry Check UNSAFE cross_origin_redirect_credentials: " +
            "credential_exposed_at_target: " +
            "The synthetic Authorization value reached the redirect target.");
    }

    [TestMethod]
    public async Task InconclusiveSuiteThrowsOnlyFixedInconclusiveAssertion()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);
        var lines = new List<string>();

        var exception = await CaptureAssertionAsync(() => ScenarioTest.CheckAsync(invoker, lines.Add));

        Assert.AreEqual(
            "HTTP Retry Check is inconclusive.",
            exception.Message);
        Assert.IsTrue(lines.Count >= 6);
        Assert.IsTrue(lines.TrueForAll(
            line => line.StartsWith(
                "HTTP Retry Check INCONCLUSIVE ",
                StringComparison.Ordinal)));
    }

    [TestMethod]
    public async Task SuiteFailureIsTranslatedToFixedAssertionAndFaultedTask()
    {
        var task = ScenarioTest.CheckAsync(null!);

        Assert.IsNotNull(task);
        var exception = await CaptureAssertionAsync(() => task);
        Assert.AreEqual("HTTP Retry Check scenario suite could not run.", exception.Message);
        Assert.IsFalse(task.IsCanceled);
    }

    [TestMethod]
    public async Task CallerCancellationPropagatesWithoutAssertionTranslation()
    {
        using var client = RuntimeTestClients.CreateOrdinaryClient();
        using var cancellation = new CancellationTokenSource();
        cancellation.Cancel();

        var task = ScenarioTest.CheckAsync(client, cancellationToken: cancellation.Token);

        OperationCanceledException? observed = null;
        try
        {
            await task;
        }
        catch (OperationCanceledException exception)
        {
            observed = exception;
        }

        Assert.IsNotNull(observed);
        Assert.AreEqual(cancellation.Token, observed.CancellationToken);
        Assert.IsTrue(task.IsCanceled);
    }

    [TestMethod]
    public async Task ClientExceptionPropagatesWithoutAssertionTranslation()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.SynchronousException);
        using var invoker = new HttpMessageInvoker(handler, false);

        Exception? observed = null;
        try
        {
            await ScenarioTest.CheckAsync(invoker);
        }
        catch (Exception exception)
        {
            observed = exception;
        }

        Assert.AreSame(handler.Failure, observed);
        var stackTrace = observed?.StackTrace;
        Assert.IsNotNull(stackTrace);
        StringAssert.Contains(stackTrace, "ControlledHandler.SendAsync");
    }

    [TestMethod]
    public async Task CallbackFailureIsTranslatedWithoutMarkerLeak()
    {
        using var handler = new ControlledHandler(ControlledHandlerMode.ImmediateResponse);
        using var invoker = new HttpMessageInvoker(handler, false);

        var exception = await CaptureAssertionAsync(
            () => ScenarioTest.CheckAsync(
                invoker,
                _ => throw new InvalidOperationException("marker-callback-secret")));

        Assert.AreEqual(
            "HTTP Retry Check scenario diagnostics could not be written.",
            exception.Message);
        Assert.IsFalse(exception.ToString().Contains("marker", StringComparison.Ordinal));
    }

    private static async Task<ScenarioAssertionException> CaptureAssertionAsync(Func<Task> operation)
    {
        try
        {
            await operation();
        }
        catch (ScenarioAssertionException exception)
        {
            return exception;
        }

        Assert.Fail("Expected a fixed ScenarioAssertionException.");
        throw new InvalidOperationException("unreachable assertion state");
    }
}
