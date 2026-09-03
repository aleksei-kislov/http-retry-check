using System;
using HttpRetryCheck.V1.Runtime;

namespace HttpRetryCheck.V1;

/// <summary>Sets the bounded runtime limits used by each scenario.</summary>
public sealed class ScenarioOptions
{
    /// <summary>Creates scenario options, using suite defaults for omitted durations.</summary>
    public ScenarioOptions(
        TimeSpan? scenarioTimeout = null,
        TimeSpan? connectionTimeout = null,
        TimeSpan? quietWindow = null,
        uint attemptLimit = RuntimeConstants.DefaultAttemptLimit)
    {
        ScenarioTimeout = scenarioTimeout ?? RuntimeConstants.ScenarioTimeout;
        ConnectionTimeout = connectionTimeout ?? RuntimeConstants.ConnectionTimeout;
        QuietWindow = quietWindow ?? RuntimeConstants.PostInvocationQuietPeriod;
        AttemptLimit = attemptLimit;
    }

    /// <summary>Gets the maximum time allowed for one scenario.</summary>
    public TimeSpan ScenarioTimeout { get; }

    /// <summary>Gets the maximum time allowed for one accepted connection.</summary>
    public TimeSpan ConnectionTimeout { get; }

    /// <summary>Gets how long the suite watches for a retry after the client returns.</summary>
    public TimeSpan QuietWindow { get; }

    /// <summary>Gets the maximum number of attempts considered safe.</summary>
    public uint AttemptLimit { get; }

    internal bool IsValid()
    {
        return IsValidDuration(ScenarioTimeout) &&
            IsValidDuration(ConnectionTimeout) &&
            IsValidDuration(QuietWindow) &&
            AttemptLimit is >= RuntimeConstants.MinimumAttemptLimit and
                <= RuntimeConstants.MaximumAttemptLimit;
    }

    private static bool IsValidDuration(TimeSpan duration)
    {
        return duration > TimeSpan.Zero && duration <= TimeSpan.FromMinutes(1);
    }
}
