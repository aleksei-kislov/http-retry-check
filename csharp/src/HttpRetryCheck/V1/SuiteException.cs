using System;

namespace HttpRetryCheck.V1;

/// <summary>Represents an invalid call, unavailable suite, internal failure, or invalid result.</summary>
public sealed class SuiteException : Exception
{
    internal SuiteException(SuiteFailureCode code)
        : base(MessageFor(Normalize(code)))
    {
        Code = Normalize(code);
    }

    /// <summary>Gets the stable category for the suite failure.</summary>
    public SuiteFailureCode Code { get; }

    /// <inheritdoc />
    public override string? StackTrace => null;

    /// <inheritdoc />
    public override string ToString() => Message;

    private static SuiteFailureCode Normalize(SuiteFailureCode code)
    {
        return code is SuiteFailureCode.InvalidCall or
            SuiteFailureCode.SuiteUnavailable or
            SuiteFailureCode.InternalFailure or
            SuiteFailureCode.InvalidResult
            ? code
            : SuiteFailureCode.InternalFailure;
    }

    private static string MessageFor(SuiteFailureCode code)
    {
        return code switch
        {
            SuiteFailureCode.InvalidCall => "HTTP scenario suite call is invalid",
            SuiteFailureCode.SuiteUnavailable => "HTTP scenario suite is unavailable",
            SuiteFailureCode.InternalFailure => "HTTP scenario suite failed internally",
            SuiteFailureCode.InvalidResult => "HTTP scenario suite result is invalid",
            _ => "HTTP scenario suite failed internally",
        };
    }
}
