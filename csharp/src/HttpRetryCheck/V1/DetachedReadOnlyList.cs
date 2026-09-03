using System;
using System.Collections;
using System.Collections.Generic;

namespace HttpRetryCheck.V1;

internal sealed class DetachedReadOnlyList<T> : IReadOnlyList<T>
{
    private readonly T[] items;

    private DetachedReadOnlyList(T[] items)
    {
        this.items = items;
    }

    public int Count => items.Length;

    public T this[int index] => items[index];

    public IEnumerator<T> GetEnumerator()
    {
        for (var index = 0; index < items.Length; index++)
        {
            yield return items[index];
        }
    }

    IEnumerator IEnumerable.GetEnumerator() => GetEnumerator();

    internal static bool TryCopy(
        IReadOnlyList<T>? source,
        int maximumCount,
        out DetachedReadOnlyList<T>? detached)
    {
        detached = null;
        if (source is null)
        {
            return false;
        }

        try
        {
            var count = source.Count;
            if (count < 0 || count > maximumCount)
            {
                return false;
            }

            var copy = new T[count];
            for (var index = 0; index < count; index++)
            {
                copy[index] = source[index];
            }

            detached = new DetachedReadOnlyList<T>(copy);
            return true;
        }
        catch (Exception exception) when (RecoverableException.IsRecoverable(exception))
        {
            return false;
        }
    }

}

internal static class RecoverableException
{
    internal static bool IsRecoverable(Exception exception)
    {
        return exception is not OutOfMemoryException and
            not StackOverflowException and
            not AccessViolationException;
    }
}
