using System;

namespace HttpRetryCheck.V1.Testing;

public sealed class ScenarioAssertionException : Exception
{
    internal ScenarioAssertionException(string message)
        : base(message)
    {
    }

    public override string? StackTrace => null;

    public override string ToString()
    {
        return Message;
    }
}
