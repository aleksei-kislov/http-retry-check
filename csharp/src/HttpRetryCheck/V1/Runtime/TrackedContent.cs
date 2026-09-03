using System;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;

namespace HttpRetryCheck.V1.Runtime;

internal sealed class TrackedContent : HttpContent
{
    private readonly object sync = new();
    private readonly bool changeReplay;
    private readonly TaskCompletionSource quiesced = new(
        TaskCreationOptions.RunContinuationsAsynchronously);
    private int acquisitions;
    private int active;
    private bool sealedForAcquisition;
    private bool disposed;

    internal TrackedContent(bool changeReplay)
    {
        this.changeReplay = changeReplay;
        Headers.ContentLength = RuntimeConstants.OrdinaryBody.Length;
    }

    internal void Seal()
    {
        lock (sync)
        {
            sealedForAcquisition = true;
            SignalIfQuiesced();
        }
    }

    internal Task WaitForQuiescenceAsync(CancellationToken cancellationToken)
    {
        lock (sync)
        {
            if (sealedForAcquisition && active == 0)
            {
                return Task.CompletedTask;
            }
        }

        return quiesced.Task.WaitAsync(cancellationToken);
    }

    protected override Task SerializeToStreamAsync(Stream stream, TransportContext? context)
    {
        return SerializeToStreamAsync(stream, context, CancellationToken.None);
    }

    protected override async Task SerializeToStreamAsync(
        Stream stream,
        TransportContext? context,
        CancellationToken cancellationToken)
    {
        ArgumentNullException.ThrowIfNull(stream);

        byte[] bytes;
        lock (sync)
        {
            if (sealedForAcquisition || disposed)
            {
                throw new InvalidOperationException("HTTP scenario request content is sealed");
            }

            var acquisition = acquisitions++;
            active++;
            bytes = changeReplay && acquisition != 0
                ? RuntimeConstants.ChangedBody.ToArray()
                : RuntimeConstants.OrdinaryBody.ToArray();
        }

        try
        {
            await stream.WriteAsync(bytes.AsMemory(), cancellationToken).ConfigureAwait(false);
        }
        finally
        {
            Array.Clear(bytes);
            lock (sync)
            {
                if (active != 0)
                {
                    active--;
                }

                SignalIfQuiesced();
            }
        }
    }

    protected override bool TryComputeLength(out long length)
    {
        length = RuntimeConstants.OrdinaryBody.Length;
        return true;
    }

    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            lock (sync)
            {
                disposed = true;
                sealedForAcquisition = true;
                SignalIfQuiesced();
            }
        }

        base.Dispose(disposing);
    }

    private void SignalIfQuiesced()
    {
        if (sealedForAcquisition && active == 0)
        {
            quiesced.TrySetResult();
        }
    }
}
