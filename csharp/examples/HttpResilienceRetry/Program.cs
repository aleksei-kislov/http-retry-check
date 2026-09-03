using System;
using System.Net;
using System.Net.Http;
using System.Threading.Tasks;
using HttpRetryCheck.V1;
using Microsoft.Extensions.Http.Resilience;
using Polly;

namespace HttpRetryCheck.Examples.HttpResilienceRetry;

internal static class Program
{
    private const int ScenarioCount = 6;

    private static async Task Main()
    {
        ProfileSummary safe = await RunProfileAsync(
            "safe",
            ResiliencePipeline<HttpResponseMessage>.Empty).ConfigureAwait(false);

        var retryOptions = new HttpRetryStrategyOptions
        {
            MaxRetryAttempts = 1,
            Delay = TimeSpan.FromMilliseconds(10),
            BackoffType = DelayBackoffType.Constant,
            UseJitter = false,
            ShouldRetryAfterHeader = false,
        };
        ResiliencePipeline<HttpResponseMessage> unsafePipeline =
            new ResiliencePipelineBuilder<HttpResponseMessage>()
                .AddRetry(retryOptions)
                .Build();
        ProfileSummary unsafeRetry = await RunProfileAsync(
            "unsafe-retry",
            unsafePipeline).ConfigureAwait(false);

        Require(
            safe.Assessment == Assessment.NoUnsafeBehaviorObserved &&
                safe.Positive == ScenarioCount &&
                safe.Unsafe == 0 &&
                safe.Inconclusive == 0,
            "The safe control did not produce six positive scenarios.");
        Require(
            unsafeRetry.Assessment == Assessment.UnsafeBehaviorObserved &&
                unsafeRetry.Positive == 3 &&
                unsafeRetry.Unsafe == 3 &&
                unsafeRetry.Inconclusive == 0,
            "The retry profile did not produce the expected three unsafe and three positive scenarios.");
        Require(
            unsafeRetry.AcceptThenDisconnectRetryAfterEffect,
            "The retry profile did not observe a retry after the accepted request.");

        WriteSummary(safe);
        WriteSummary(unsafeRetry);
    }

    private static async Task<ProfileSummary> RunProfileAsync(
        string name,
        ResiliencePipeline<HttpResponseMessage> pipeline)
    {
        using HttpClient client = CreateClient(pipeline);
        SuiteResult result = await ScenarioSuite.RunAsync(client).ConfigureAwait(false);
        ScenarioSuite.Validate(result);

        Require(result.Scenarios.Count == ScenarioCount, $"{name} did not return six scenarios.");

        var positive = 0;
        var unsafeCount = 0;
        var inconclusive = 0;
        var acceptThenDisconnectRetryAfterEffect = false;
        foreach (ScenarioResult scenario in result.Scenarios)
        {
            Observation observation = scenario.Observation;
            Require(observation.AttemptCount > 0, $"{name} did not run {scenario.Scenario}.");
            Require(observation.CaptureComplete, $"{name} did not completely capture {scenario.Scenario}.");
            Require(
                observation.Cleanup == CleanupState.Succeeded,
                $"{name} did not verify cleanup for {scenario.Scenario}.");
            Require(
                observation.Protocol == "HTTP/1.1",
                $"{name} did not use exact HTTP/1.1 for {scenario.Scenario}.");

            switch (scenario.Assessment)
            {
                case Assessment.NoUnsafeBehaviorObserved:
                    positive++;
                    break;
                case Assessment.UnsafeBehaviorObserved:
                    unsafeCount++;
                    break;
                case Assessment.Inconclusive:
                    inconclusive++;
                    break;
                default:
                    throw new InvalidOperationException($"{name} returned an unknown assessment.");
            }

            if (scenario.Scenario == ScenarioId.AcceptThenDisconnect)
            {
                foreach (FindingCode finding in scenario.Findings)
                {
                    if (finding == FindingCode.RetryAfterAcceptedRequest)
                    {
                        acceptThenDisconnectRetryAfterEffect = true;
                    }
                }
            }
        }

        return new ProfileSummary(
            name,
            result.Assessment,
            positive,
            unsafeCount,
            inconclusive,
            acceptThenDisconnectRetryAfterEffect);
    }

    private static HttpClient CreateClient(ResiliencePipeline<HttpResponseMessage> pipeline)
    {
        var sockets = new SocketsHttpHandler
        {
            AllowAutoRedirect = true,
            MaxAutomaticRedirections = 2,
            UseCookies = false,
            UseProxy = false,
        };
        var resilience = new ResilienceHandler(pipeline)
        {
            InnerHandler = sockets,
        };
        return new HttpClient(resilience, disposeHandler: true)
        {
            DefaultRequestVersion = HttpVersion.Version11,
            DefaultVersionPolicy = HttpVersionPolicy.RequestVersionExact,
            Timeout = System.Threading.Timeout.InfiniteTimeSpan,
        };
    }

    private static void WriteSummary(ProfileSummary summary)
    {
        Console.WriteLine(
            $"{summary.Name}: assessment={summary.Assessment} " +
            $"positive={summary.Positive} unsafe={summary.Unsafe} inconclusive={summary.Inconclusive}");
    }

    private static void Require(bool condition, string message)
    {
        if (!condition)
        {
            throw new InvalidOperationException(message);
        }
    }

    private readonly record struct ProfileSummary(
        string Name,
        Assessment Assessment,
        int Positive,
        int Unsafe,
        int Inconclusive,
        bool AcceptThenDisconnectRetryAfterEffect);
}
