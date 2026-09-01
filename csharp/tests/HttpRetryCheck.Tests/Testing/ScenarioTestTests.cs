using System;
using System.Collections.Generic;
using System.Net.Http;
using System.Threading.Tasks;
using HttpRetryCheck.Tests.Runtime;
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

        Assert.AreEqual(6, lines.Count);
        Assert.IsTrue(lines.TrueForAll(
            line => line.StartsWith(
                "HTTP Retry Check PASS ",
                StringComparison.Ordinal)));
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
