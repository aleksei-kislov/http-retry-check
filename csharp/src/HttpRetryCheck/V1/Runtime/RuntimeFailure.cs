using System;

namespace HttpRetryCheck.V1.Runtime;

internal static class RuntimeFailure
{
    internal static bool IsRecoverable(Exception exception)
    {
        return exception is not OutOfMemoryException and
            not StackOverflowException and
            not AccessViolationException;
    }
}
