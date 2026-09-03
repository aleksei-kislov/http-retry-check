using System;

namespace HttpRetryCheck.V1.Reporting;

/// <summary>Thrown when a report or artifact is invalid.</summary>
public sealed class ReportException : Exception
{
    internal ReportException(bool artifact)
        : base(artifact
            ? "HTTP retry scenario suite artifact is invalid"
            : "HTTP retry scenario suite report is invalid")
    {
    }

    /// <inheritdoc />
    public override string? StackTrace => null;

    /// <inheritdoc />
    public override string ToString() => Message;
}
