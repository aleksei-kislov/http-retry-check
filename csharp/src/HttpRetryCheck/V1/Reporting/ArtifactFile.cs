using System;

namespace HttpRetryCheck.V1.Reporting;

/// <summary>Stores an evidence artifact file in memory.</summary>
public sealed class ArtifactFile
{
    private readonly byte[] _contents;

    /// <summary>Creates an artifact file by copying its contents.</summary>
    public ArtifactFile(string name, string mediaType, ReadOnlyMemory<byte> contents)
    {
        try
        {
            if (!ArtifactCodec.IsFileIdentity(name, mediaType) || contents.Length == 0 ||
                contents.Length > ScenarioReports.MaxProjectionBytes)
            {
                throw ReportingFailure.Artifact();
            }

            Name = name;
            MediaType = mediaType;
            _contents = contents.ToArray();
        }
        catch (Exception exception) when (ReportingFailure.IsRecoverable(exception))
        {
            throw ReportingFailure.Artifact();
        }
    }

    public string Name { get; }
    public string MediaType { get; }

    /// <summary>Returns a new copy of the file bytes.</summary>
    public ReadOnlyMemory<byte> Contents => new((byte[])_contents.Clone());

    internal byte[] CopyContents() => (byte[])_contents.Clone();
}
