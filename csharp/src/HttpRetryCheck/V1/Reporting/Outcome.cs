namespace HttpRetryCheck.V1.Reporting;

/// <summary>Maps a suite assessment to a CI result.</summary>
public enum Outcome
{
    /// <summary>All six scenarios have positive observations.</summary>
    Pass = 1,

    /// <summary>At least one scenario has an unsafe observation.</summary>
    Fail = 2,

    /// <summary>No scenario is unsafe and at least one is inconclusive.</summary>
    Inconclusive = 3,
}
