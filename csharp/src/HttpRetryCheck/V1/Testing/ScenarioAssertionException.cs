using System;

namespace HttpRetryCheck.V1.Testing;

/// <summary>Indicates that the suite was unsafe, inconclusive, or unable to run as a test.</summary>
public sealed class ScenarioAssertionException : Exception
{
    internal ScenarioAssertionException(string message)
        : base(message)
    {
    }

    /// <inheritdoc />
    public override string? StackTrace => null;

    /// <inheritdoc />
    public override string ToString()
    {
        return Message;
    }
}
