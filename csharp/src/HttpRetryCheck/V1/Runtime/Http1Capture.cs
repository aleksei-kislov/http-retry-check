using System;
using System.Globalization;
using System.Net;
using System.Net.Sockets;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.V1.Runtime;

internal readonly record struct CaptureResult(
    bool HeadersObserved,
    bool Complete,
    bool CaptureComplete,
    bool MethodConsistent,
    bool DestinationConsistent,
    bool BodyConsistent,
    bool CredentialExact,
    bool CredentialExposed,
    SocketWireReader? Reader);

internal readonly record struct ProbeResult(bool Trailing, bool Complete);

internal readonly record struct HeaderReadResult(byte[] Bytes, bool Complete);

internal sealed class SocketWireReader
{
    internal const int EndOfStream = -1;
    internal const int BudgetExhausted = -2;

    private readonly Socket socket;
    private readonly byte[] buffer = new byte[4096];
    private int offset;
    private int count;
    private int remaining;

    internal SocketWireReader(Socket socket, int budget)
    {
        this.socket = socket;
        remaining = budget;
    }

    internal int BufferedCount => count - offset;

    internal int RemainingBudget => remaining;

    internal async ValueTask<int> ReadByteAsync(CancellationToken cancellationToken)
    {
        if (offset != count)
        {
            return buffer[offset++];
        }

        if (remaining == 0)
        {
            return BudgetExhausted;
        }

        var requested = Math.Min(buffer.Length, remaining);
        var received = await socket.ReceiveAsync(
            buffer.AsMemory(0, requested),
            SocketFlags.None,
            cancellationToken).ConfigureAwait(false);
        if (received == 0)
        {
            return EndOfStream;
        }

        remaining -= received;
        offset = 1;
        count = received;
        return buffer[0];
    }
}

internal static class Http1Capture
{
    internal static async Task<CaptureResult> ReadAsync(
        Socket socket,
        IPEndPoint expectedEndpoint,
        CancellationToken cancellationToken)
    {
        var result = new CaptureResult(
            false,
            false,
            false,
            true,
            true,
            true,
            false,
            false,
            null);
        byte[]? header = null;
        try
        {
            var headerRead = await ReadHeaderAsync(socket, cancellationToken).ConfigureAwait(false);
            header = headerRead.Bytes;
            result = result with
            {
                CredentialExposed = header.AsSpan().IndexOf(RuntimeConstants.SyntheticMarkerBytes) >= 0,
            };
            if (!headerRead.Complete || !TryParseHeader(header, expectedEndpoint, out var parsed))
            {
                return result;
            }

            result = result with
            {
                HeadersObserved = true,
                MethodConsistent = parsed.MethodConsistent,
                DestinationConsistent = parsed.DestinationConsistent,
                CredentialExact = parsed.CredentialExact,
            };
            if (!parsed.ProtocolExact)
            {
                return result;
            }

            var reader = new SocketWireReader(socket, RuntimeConstants.MaximumWireBytes);
            var body = await ReadBodyAsync(reader, parsed, cancellationToken).ConfigureAwait(false);
            if (!body.Complete)
            {
                return result with { BodyConsistent = body.Consistent, Reader = reader };
            }

            var probe = await ProbeAsync(reader, cancellationToken).ConfigureAwait(false);
            return result with
            {
                Complete = true,
                CaptureComplete = !probe.Trailing && probe.Complete && reader.RemainingBudget != 0,
                BodyConsistent = body.Consistent,
                Reader = reader,
            };
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return result;
        }
        finally
        {
            if (header is not null)
            {
                Array.Clear(header);
            }
        }
    }

    internal static async Task<ProbeResult> ProbeAsync(
        SocketWireReader reader,
        CancellationToken cancellationToken)
    {
        if (reader.BufferedCount != 0)
        {
            return new ProbeResult(true, true);
        }

        if (reader.RemainingBudget == 0)
        {
            return new ProbeResult(false, false);
        }

        using var probeCancellation = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        probeCancellation.CancelAfter(RuntimeConstants.TrailingWindow);
        try
        {
            var value = await reader.ReadByteAsync(probeCancellation.Token).ConfigureAwait(false);
            return value switch
            {
                SocketWireReader.EndOfStream => new ProbeResult(false, true),
                SocketWireReader.BudgetExhausted => new ProbeResult(false, false),
                _ => new ProbeResult(true, true),
            };
        }
        catch (OperationCanceledException) when (
            probeCancellation.IsCancellationRequested && !cancellationToken.IsCancellationRequested)
        {
            return new ProbeResult(false, true);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return new ProbeResult(false, false);
        }
    }

    private static async Task<HeaderReadResult> ReadHeaderAsync(
        Socket socket,
        CancellationToken cancellationToken)
    {
        var header = new byte[RuntimeConstants.MaximumHeaderBytes];
        var current = new byte[1];
        var length = 0;
        try
        {
            for (; length < header.Length; length++)
            {
                var count = await socket.ReceiveAsync(
                    current.AsMemory(),
                    SocketFlags.None,
                    cancellationToken).ConfigureAwait(false);
                if (count != 1)
                {
                    return CopyHeader(header, length, complete: false);
                }

                header[length] = current[0];
                if (length >= 3 &&
                    header[length - 3] == (byte)'\r' &&
                    header[length - 2] == (byte)'\n' &&
                    header[length - 1] == (byte)'\r' &&
                    header[length] == (byte)'\n')
                {
                    return CopyHeader(header, length + 1, complete: true);
                }
            }

            return CopyHeader(header, header.Length, complete: false);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return CopyHeader(header, length, complete: false);
        }
        finally
        {
            Array.Clear(header);
            Array.Clear(current);
        }
    }

    private static HeaderReadResult CopyHeader(byte[] source, int length, bool complete)
    {
        var bytes = new byte[length];
        Buffer.BlockCopy(source, 0, bytes, 0, length);
        return new HeaderReadResult(bytes, complete);
    }

    private static bool TryParseHeader(
        byte[] header,
        IPEndPoint expectedEndpoint,
        out ParsedRequest parsed)
    {
        parsed = default;
        ReadOnlySpan<byte> head = header;
        if (head.Length < 4 ||
            !head[^4..].SequenceEqual("\r\n\r\n"u8))
        {
            return false;
        }

        var lineEnd = head.IndexOf("\r\n"u8);
        if (lineEnd <= 0 ||
            !TryParseRequestLine(
                head[..lineEnd],
                out var method,
                out var target,
                out var protocol))
        {
            return false;
        }

        var hostCount = 0;
        var destinationConsistent = false;
        var credentialCount = 0;
        var credentialExact = false;
        long contentLength = 0;
        var contentLengthSeen = false;
        var chunked = false;
        var transferEncodingSeen = false;
        var offset = lineEnd + 2;
        while (offset < head.Length - 2)
        {
            lineEnd = head[offset..].IndexOf("\r\n"u8);
            if (lineEnd < 0)
            {
                return false;
            }

            var line = head.Slice(offset, lineEnd);
            offset += lineEnd + 2;
            if (line.IsEmpty)
            {
                return false;
            }

            var colon = line.IndexOf((byte)':');
            if (colon <= 0 || !IsToken(line[..colon]))
            {
                return false;
            }

            var name = line[..colon];
            var fieldValue = TrimOptionalWhitespace(line[(colon + 1)..]);
            if (!IsFieldValue(fieldValue))
            {
                return false;
            }

            if (EqualsAsciiIgnoreCase(name, "Host"u8))
            {
                hostCount++;
                destinationConsistent = HostMatches(fieldValue, expectedEndpoint.Port);
            }
            else if (EqualsAsciiIgnoreCase(name, "Authorization"u8))
            {
                credentialCount++;
                credentialExact = fieldValue.SequenceEqual(RuntimeConstants.SyntheticCredentialBytes);
            }
            else if (EqualsAsciiIgnoreCase(name, "Content-Length"u8))
            {
                if (contentLengthSeen || !TryParseContentLength(fieldValue, out contentLength))
                {
                    return false;
                }

                contentLengthSeen = true;
            }
            else if (EqualsAsciiIgnoreCase(name, "Transfer-Encoding"u8))
            {
                if (transferEncodingSeen ||
                    !EqualsAsciiIgnoreCase(fieldValue, "chunked"u8))
                {
                    return false;
                }

                transferEncodingSeen = true;
                chunked = true;
            }
        }

        if (offset != head.Length - 2 || hostCount != 1 || (chunked && contentLengthSeen))
        {
            return false;
        }

        parsed = new ParsedRequest(
            method.SequenceEqual("POST"u8),
            target.SequenceEqual("/case"u8) && destinationConsistent,
            protocol.SequenceEqual("HTTP/1.1"u8),
            credentialCount == 1 && credentialExact,
            contentLength,
            chunked);
        return true;
    }

    private static bool TryParseRequestLine(
        ReadOnlySpan<byte> line,
        out ReadOnlySpan<byte> method,
        out ReadOnlySpan<byte> target,
        out ReadOnlySpan<byte> protocol)
    {
        method = default;
        target = default;
        protocol = default;
        var firstSpace = line.IndexOf((byte)' ');
        if (firstSpace <= 0)
        {
            return false;
        }

        var secondSpace = line[(firstSpace + 1)..].IndexOf((byte)' ');
        if (secondSpace < 0)
        {
            return false;
        }

        secondSpace += firstSpace + 1;
        method = line[..firstSpace];
        target = line[(firstSpace + 1)..secondSpace];
        protocol = line[(secondSpace + 1)..];
        return IsToken(method) && IsVisibleAscii(target) && IsProtocol(protocol);
    }

    private static bool IsVisibleAscii(ReadOnlySpan<byte> value)
    {
        if (value.IsEmpty)
        {
            return false;
        }

        foreach (var current in value)
        {
            if (current is < 0x21 or > 0x7e)
            {
                return false;
            }
        }

        return true;
    }

    private static bool IsProtocol(ReadOnlySpan<byte> value)
    {
        return value.Length == 8 &&
            value[..5].SequenceEqual("HTTP/"u8) &&
            value[5] is >= (byte)'0' and <= (byte)'9' &&
            value[6] == (byte)'.' &&
            value[7] is >= (byte)'0' and <= (byte)'9';
    }

    private static bool TryParseContentLength(ReadOnlySpan<byte> value, out long parsed)
    {
        parsed = 0;
        if (value.IsEmpty)
        {
            return false;
        }

        foreach (var current in value)
        {
            if (current is < (byte)'0' or > (byte)'9')
            {
                return false;
            }

            var digit = current - (byte)'0';
            if (parsed > (long.MaxValue - digit) / 10)
            {
                return false;
            }

            parsed = (parsed * 10) + digit;
        }

        return true;
    }

    private static bool HostMatches(ReadOnlySpan<byte> value, int port)
    {
        var expected = string.Create(
            CultureInfo.InvariantCulture,
            $"127.0.0.1:{port}");
        if (value.Length != expected.Length)
        {
            return false;
        }

        for (var index = 0; index < value.Length; index++)
        {
            if (value[index] != expected[index])
            {
                return false;
            }
        }

        return true;
    }

    private static async Task<BodyResult> ReadBodyAsync(
        SocketWireReader reader,
        ParsedRequest parsed,
        CancellationToken cancellationToken)
    {
        var body = new BodyAccumulator();
        try
        {
            if (parsed.Chunked)
            {
                return await ReadChunkedBodyAsync(reader, body, cancellationToken).ConfigureAwait(false);
            }

            for (long index = 0; index < parsed.ContentLength; index++)
            {
                var value = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
                if (value < 0)
                {
                    return body.Failed();
                }

                if (!body.Add((byte)value))
                {
                    return body.Failed();
                }
            }

            return body.Completed();
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return body.Failed();
        }
    }

    private static async Task<BodyResult> ReadChunkedBodyAsync(
        SocketWireReader reader,
        BodyAccumulator body,
        CancellationToken cancellationToken)
    {
        while (true)
        {
            var chunk = await ReadChunkSizeAsync(reader, cancellationToken).ConfigureAwait(false);
            if (!chunk.Valid)
            {
                return body.Failed();
            }

            if (chunk.Size == 0)
            {
                var trailersComplete = await ReadTrailersAsync(reader, cancellationToken).ConfigureAwait(false);
                return trailersComplete ? body.Completed() : body.Failed();
            }

            for (ulong index = 0; index < chunk.Size; index++)
            {
                var value = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
                if (value < 0 || !body.Add((byte)value))
                {
                    return body.Failed();
                }
            }

            if (!await ReadCrLfAsync(reader, cancellationToken).ConfigureAwait(false))
            {
                return body.Failed();
            }
        }
    }

    private static async Task<ChunkSize> ReadChunkSizeAsync(
        SocketWireReader reader,
        CancellationToken cancellationToken)
    {
        ulong size = 0;
        var digits = 0;
        var state = ChunkSizeState.Size;
        while (true)
        {
            var value = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
            if (value < 0)
            {
                return default;
            }

            if (value == '\r')
            {
                if (!CanEndChunkSize(state, digits))
                {
                    return default;
                }

                var lineFeed = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
                return lineFeed == '\n'
                    ? new ChunkSize(true, size)
                    : default;
            }

            if (value == '\n')
            {
                return default;
            }

            switch (state)
            {
                case ChunkSizeState.Size:
                    var digit = HexValue(value);
                    if (digit >= 0)
                    {
                        if (size > (ulong.MaxValue - (uint)digit) / 16)
                        {
                            return default;
                        }

                        size = (size * 16) + (uint)digit;
                        digits++;
                    }
                    else if (digits == 0)
                    {
                        return default;
                    }
                    else if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (IsOptionalWhitespace(value))
                    {
                        state = ChunkSizeState.BeforeExtension;
                    }
                    else
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.BeforeExtension:
                    if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (!IsOptionalWhitespace(value))
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.ExtensionNameStart:
                    if (IsTokenCharacter(value))
                    {
                        state = ChunkSizeState.ExtensionName;
                    }
                    else if (!IsOptionalWhitespace(value))
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.ExtensionName:
                    if (IsTokenCharacter(value))
                    {
                        break;
                    }

                    if (value == '=')
                    {
                        state = ChunkSizeState.ExtensionValueStart;
                    }
                    else if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (IsOptionalWhitespace(value))
                    {
                        state = ChunkSizeState.AfterExtensionName;
                    }
                    else
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.AfterExtensionName:
                    if (value == '=')
                    {
                        state = ChunkSizeState.ExtensionValueStart;
                    }
                    else if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (!IsOptionalWhitespace(value))
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.ExtensionValueStart:
                    if (value == '"')
                    {
                        state = ChunkSizeState.QuotedExtensionValue;
                    }
                    else if (IsTokenCharacter(value))
                    {
                        state = ChunkSizeState.TokenExtensionValue;
                    }
                    else if (!IsOptionalWhitespace(value))
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.TokenExtensionValue:
                    if (IsTokenCharacter(value))
                    {
                        break;
                    }

                    if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (IsOptionalWhitespace(value))
                    {
                        state = ChunkSizeState.AfterExtensionValue;
                    }
                    else
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.QuotedExtensionValue:
                    if (value == '"')
                    {
                        state = ChunkSizeState.QuotedExtensionComplete;
                    }
                    else if (value == '\\')
                    {
                        state = ChunkSizeState.QuotedExtensionPair;
                    }
                    else if (!IsQuotedText(value))
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.QuotedExtensionPair:
                    if (!IsQuotedPairValue(value))
                    {
                        return default;
                    }

                    state = ChunkSizeState.QuotedExtensionValue;
                    break;
                case ChunkSizeState.QuotedExtensionComplete:
                    if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (IsOptionalWhitespace(value))
                    {
                        state = ChunkSizeState.AfterExtensionValue;
                    }
                    else
                    {
                        return default;
                    }

                    break;
                case ChunkSizeState.AfterExtensionValue:
                    if (value == ';')
                    {
                        state = ChunkSizeState.ExtensionNameStart;
                    }
                    else if (!IsOptionalWhitespace(value))
                    {
                        return default;
                    }

                    break;
                default:
                    return default;
            }
        }
    }

    private static bool CanEndChunkSize(ChunkSizeState state, int digits)
    {
        return digits != 0 &&
            state is ChunkSizeState.Size or
                ChunkSizeState.ExtensionName or
                ChunkSizeState.TokenExtensionValue or
                ChunkSizeState.QuotedExtensionComplete;
    }

    private static bool IsOptionalWhitespace(int value)
    {
        return value is ' ' or '\t';
    }

    private static bool IsQuotedText(int value)
    {
        return value is '\t' or ' ' or 0x21 or >= 0x23 and <= 0x5b or >= 0x5d and <= 0x7e or >= 0x80;
    }

    private static bool IsQuotedPairValue(int value)
    {
        return value is '\t' or ' ' or >= 0x21 and <= 0x7e or >= 0x80;
    }

    private static async Task<bool> ReadTrailersAsync(
        SocketWireReader reader,
        CancellationToken cancellationToken)
    {
        var lineLength = 0;
        var nameLength = 0;
        var colonSeen = false;
        while (true)
        {
            var value = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
            if (value < 0)
            {
                return false;
            }

            if (value == '\r')
            {
                var lineFeed = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
                if (lineFeed != '\n')
                {
                    return false;
                }

                if (lineLength == 0)
                {
                    return true;
                }

                if (!colonSeen || nameLength == 0)
                {
                    return false;
                }

                lineLength = 0;
                nameLength = 0;
                colonSeen = false;
                continue;
            }

            if (value == '\n' || value == 0x7f || value < 0x20 && value != '\t')
            {
                return false;
            }

            if (!colonSeen)
            {
                if (value == ':')
                {
                    if (nameLength == 0)
                    {
                        return false;
                    }

                    colonSeen = true;
                }
                else if (!IsTokenCharacter((char)value))
                {
                    return false;
                }
                else
                {
                    nameLength++;
                }
            }

            lineLength++;
        }
    }

    private static async Task<bool> ReadCrLfAsync(
        SocketWireReader reader,
        CancellationToken cancellationToken)
    {
        return await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false) == '\r' &&
            await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false) == '\n';
    }

    private static int HexValue(int value)
    {
        if (value is >= '0' and <= '9')
        {
            return value - '0';
        }

        if (value is >= 'a' and <= 'f')
        {
            return value - 'a' + 10;
        }

        if (value is >= 'A' and <= 'F')
        {
            return value - 'A' + 10;
        }

        return -1;
    }

    private static bool IsToken(ReadOnlySpan<byte> value)
    {
        if (value.IsEmpty)
        {
            return false;
        }

        foreach (var current in value)
        {
            if (!IsTokenCharacter(current))
            {
                return false;
            }
        }

        return true;
    }

    private static bool IsTokenCharacter(int character)
    {
        return character is >= 'a' and <= 'z' or >= 'A' and <= 'Z' or >= '0' and <= '9' ||
            character is '!' or '#' or '$' or '%' or '&' or '\'' or '*' or '+' or '-' or '.' or '^' or
                '_' or '`' or '|' or '~';
    }

    private static bool IsFieldValue(ReadOnlySpan<byte> value)
    {
        foreach (var character in value)
        {
            if (character == '\t')
            {
                continue;
            }

            if (character < 0x20 || character == 0x7f)
            {
                return false;
            }
        }

        return true;
    }

    private static ReadOnlySpan<byte> TrimOptionalWhitespace(ReadOnlySpan<byte> value)
    {
        while (!value.IsEmpty && (value[0] == (byte)' ' || value[0] == (byte)'\t'))
        {
            value = value[1..];
        }

        while (!value.IsEmpty && (value[^1] == (byte)' ' || value[^1] == (byte)'\t'))
        {
            value = value[..^1];
        }

        return value;
    }

    private static bool EqualsAsciiIgnoreCase(ReadOnlySpan<byte> left, ReadOnlySpan<byte> right)
    {
        if (left.Length != right.Length)
        {
            return false;
        }

        for (var index = 0; index < left.Length; index++)
        {
            var current = left[index];
            if (current is >= (byte)'A' and <= (byte)'Z')
            {
                current += (byte)('a' - 'A');
            }

            var expected = right[index];
            if (expected is >= (byte)'A' and <= (byte)'Z')
            {
                expected += (byte)('a' - 'A');
            }

            if (current != expected)
            {
                return false;
            }
        }

        return true;
    }

    private readonly record struct ParsedRequest(
        bool MethodConsistent,
        bool DestinationConsistent,
        bool ProtocolExact,
        bool CredentialExact,
        long ContentLength,
        bool Chunked);

    private readonly record struct BodyResult(bool Complete, bool Consistent);

    private readonly record struct ChunkSize(bool Valid, ulong Size);

    private enum ChunkSizeState
    {
        Size,
        BeforeExtension,
        ExtensionNameStart,
        ExtensionName,
        AfterExtensionName,
        ExtensionValueStart,
        TokenExtensionValue,
        QuotedExtensionValue,
        QuotedExtensionPair,
        QuotedExtensionComplete,
        AfterExtensionValue,
    }

    private sealed class BodyAccumulator
    {
        private int count;
        private bool consistent = true;

        internal bool Add(byte value)
        {
            if (count >= RuntimeConstants.MaximumBodyBytes)
            {
                count++;
                consistent = false;
                return false;
            }

            var expected = RuntimeConstants.OrdinaryBody;
            if (count >= expected.Length || expected[count] != value)
            {
                consistent = false;
            }

            count++;
            return true;
        }

        internal BodyResult Completed()
        {
            return new BodyResult(
                true,
                consistent && count == RuntimeConstants.OrdinaryBody.Length);
        }

        internal BodyResult Failed()
        {
            return new BodyResult(false, consistent);
        }
    }
}
