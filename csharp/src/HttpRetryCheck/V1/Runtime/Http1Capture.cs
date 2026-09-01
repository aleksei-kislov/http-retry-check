using System;
using System.Collections.Generic;
using System.Globalization;
using System.Net;
using System.Net.Sockets;
using System.Text;
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
        var defaultResult = new CaptureResult(
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
            header = await ReadHeaderAsync(socket, cancellationToken).ConfigureAwait(false);
            if (header is null || !TryParseHeader(header, expectedEndpoint, out var parsed))
            {
                return defaultResult;
            }

            var result = defaultResult with
            {
                HeadersObserved = true,
                MethodConsistent = parsed.MethodConsistent,
                DestinationConsistent = parsed.DestinationConsistent,
                CredentialExact = parsed.CredentialExact,
                CredentialExposed = parsed.CredentialExposed,
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
            return defaultResult;
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

    private static async Task<byte[]?> ReadHeaderAsync(
        Socket socket,
        CancellationToken cancellationToken)
    {
        var header = new byte[RuntimeConstants.MaximumHeaderBytes];
        var current = new byte[1];
        try
        {
            for (var length = 0; length < header.Length; length++)
            {
                var count = await socket.ReceiveAsync(
                    current.AsMemory(),
                    SocketFlags.None,
                    cancellationToken).ConfigureAwait(false);
                if (count != 1)
                {
                    Array.Clear(header);
                    return null;
                }

                header[length] = current[0];
                if (length >= 3 &&
                    header[length - 3] == (byte)'\r' &&
                    header[length - 2] == (byte)'\n' &&
                    header[length - 1] == (byte)'\r' &&
                    header[length] == (byte)'\n')
                {
                    var result = new byte[length + 1];
                    Buffer.BlockCopy(header, 0, result, 0, result.Length);
                    Array.Clear(header);
                    return result;
                }
            }

            Array.Clear(header);
            return null;
        }
        catch
        {
            Array.Clear(header);
            throw;
        }
        finally
        {
            Array.Clear(current);
        }
    }

    private static bool TryParseHeader(
        byte[] header,
        IPEndPoint expectedEndpoint,
        out ParsedRequest parsed)
    {
        parsed = default;
        string value;
        try
        {
            value = Encoding.Latin1.GetString(header);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return false;
        }

        var lines = value.Split("\r\n", StringSplitOptions.None);
        if (lines.Length < 3 || lines[^1].Length != 0 || lines[^2].Length != 0)
        {
            return false;
        }

        var requestLine = lines[0].Split(' ', StringSplitOptions.None);
        if (requestLine.Length != 3 ||
            !IsToken(requestLine[0]) ||
            requestLine[1].Length == 0 ||
            requestLine[1].Contains(' ', StringComparison.Ordinal) ||
            !TryParseProtocol(requestLine[2]))
        {
            return false;
        }

        var hostValues = new List<string>(1);
        var credentials = new List<string>(2);
        long? contentLength = null;
        var chunked = false;
        var transferEncodingSeen = false;
        for (var index = 1; index < lines.Length - 2; index++)
        {
            var line = lines[index];
            var colon = line.IndexOf(':', StringComparison.Ordinal);
            if (colon <= 0 || !IsToken(line.AsSpan(0, colon)))
            {
                return false;
            }

            var name = line[..colon];
            var fieldValue = TrimOptionalWhitespace(line.AsSpan(colon + 1));
            if (!IsFieldValue(fieldValue))
            {
                return false;
            }

            var fieldText = fieldValue.ToString();
            if (name.Equals("Host", StringComparison.OrdinalIgnoreCase))
            {
                hostValues.Add(fieldText);
            }
            else if (name.Equals("Authorization", StringComparison.OrdinalIgnoreCase))
            {
                credentials.Add(fieldText);
            }
            else if (name.Equals("Content-Length", StringComparison.OrdinalIgnoreCase))
            {
                if (!long.TryParse(
                        fieldText,
                        NumberStyles.None,
                        CultureInfo.InvariantCulture,
                        out var parsedLength) ||
                    parsedLength < 0 ||
                    (contentLength.HasValue && contentLength.Value != parsedLength))
                {
                    return false;
                }

                contentLength = parsedLength;
            }
            else if (name.Equals("Transfer-Encoding", StringComparison.OrdinalIgnoreCase))
            {
                if (transferEncodingSeen ||
                    !fieldText.Equals("chunked", StringComparison.OrdinalIgnoreCase))
                {
                    return false;
                }

                transferEncodingSeen = true;
                chunked = true;
            }
        }

        if (hostValues.Count != 1 || (chunked && contentLength.HasValue))
        {
            return false;
        }

        var expectedHost = string.Create(
            CultureInfo.InvariantCulture,
            $"127.0.0.1:{expectedEndpoint.Port}");
        var credentialExact = credentials.Count == 1 &&
            credentials[0].Equals(RuntimeConstants.SyntheticCredential, StringComparison.Ordinal);
        var credentialExposed = false;
        foreach (var credential in credentials)
        {
            if (credential.Contains(RuntimeConstants.SyntheticCredential, StringComparison.Ordinal))
            {
                credentialExposed = true;
            }
        }

        parsed = new ParsedRequest(
            requestLine[0].Equals("POST", StringComparison.Ordinal),
            requestLine[1].Equals(RuntimeConstants.ControlledPath, StringComparison.Ordinal) &&
                hostValues[0].Equals(expectedHost, StringComparison.Ordinal),
            requestLine[2].Equals("HTTP/1.1", StringComparison.Ordinal),
            credentialExact,
            credentialExposed,
            contentLength ?? 0,
            chunked);
        return true;
    }

    private static async Task<BodyResult> ReadBodyAsync(
        SocketWireReader reader,
        ParsedRequest parsed,
        CancellationToken cancellationToken)
    {
        var body = new BodyAccumulator();
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
        var extension = false;
        while (true)
        {
            var value = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
            if (value < 0)
            {
                return default;
            }

            if (value == '\r')
            {
                var lineFeed = await reader.ReadByteAsync(cancellationToken).ConfigureAwait(false);
                return lineFeed == '\n' && digits != 0
                    ? new ChunkSize(true, size)
                    : default;
            }

            if (value == '\n' || value < 0x20 && value != '\t')
            {
                return default;
            }

            if (extension)
            {
                continue;
            }

            if (value == ';')
            {
                extension = true;
                continue;
            }

            var digit = HexValue(value);
            if (digit < 0 || size > (ulong.MaxValue - (uint)digit) / 16)
            {
                return default;
            }

            size = (size * 16) + (uint)digit;
            digits++;
        }
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

            if (value == '\n' || value < 0x20 && value != '\t')
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

    private static bool TryParseProtocol(string value)
    {
        if (!value.StartsWith("HTTP/", StringComparison.Ordinal))
        {
            return false;
        }

        var version = value.AsSpan(5);
        var dot = version.IndexOf('.');
        if (dot <= 0 || dot == version.Length - 1)
        {
            return false;
        }

        return AllDigits(version[..dot]) && AllDigits(version[(dot + 1)..]);
    }

    private static bool AllDigits(ReadOnlySpan<char> value)
    {
        foreach (var character in value)
        {
            if (character is < '0' or > '9')
            {
                return false;
            }
        }

        return true;
    }

    private static bool IsToken(string value)
    {
        return IsToken(value.AsSpan());
    }

    private static bool IsToken(ReadOnlySpan<char> value)
    {
        if (value.IsEmpty)
        {
            return false;
        }

        foreach (var character in value)
        {
            if (!IsTokenCharacter(character))
            {
                return false;
            }
        }

        return true;
    }

    private static bool IsTokenCharacter(char character)
    {
        return char.IsAsciiLetterOrDigit(character) ||
            character is '!' or '#' or '$' or '%' or '&' or '\'' or '*' or '+' or '-' or '.' or '^' or
                '_' or '`' or '|' or '~';
    }

    private static bool IsFieldValue(ReadOnlySpan<char> value)
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

    private static ReadOnlySpan<char> TrimOptionalWhitespace(ReadOnlySpan<char> value)
    {
        while (!value.IsEmpty && value[0] is ' ' or '\t')
        {
            value = value[1..];
        }

        while (!value.IsEmpty && value[^1] is ' ' or '\t')
        {
            value = value[..^1];
        }

        return value;
    }

    private readonly record struct ParsedRequest(
        bool MethodConsistent,
        bool DestinationConsistent,
        bool ProtocolExact,
        bool CredentialExact,
        bool CredentialExposed,
        long ContentLength,
        bool Chunked);

    private readonly record struct BodyResult(bool Complete, bool Consistent);

    private readonly record struct ChunkSize(bool Valid, ulong Size);

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
