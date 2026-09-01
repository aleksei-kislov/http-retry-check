using System;

namespace HttpRetryCheck.V1.Runtime;

internal static class RuntimeConstants
{
    internal const int ScenarioCount = 6;
    internal const int ListenerCount = 7;
    internal const int MaximumAttempts = 3;
    internal const int MaximumHeaderBytes = 64 << 10;
    internal const int MaximumBodyBytes = 1 << 20;
    internal const int MaximumWireBytes = MaximumBodyBytes + 1;
    internal const string ControlledPath = "/case";
    internal const string SyntheticMarker =
        "http-retry-check-synthetic-scenario-suite-v1";
    internal const string SyntheticCredential =
        "Bearer " + SyntheticMarker;

    internal static ReadOnlySpan<byte> SyntheticMarkerBytes =>
        "http-retry-check-synthetic-scenario-suite-v1"u8;

    internal static ReadOnlySpan<byte> SyntheticCredentialBytes =>
        "Bearer http-retry-check-synthetic-scenario-suite-v1"u8;

    internal static ReadOnlySpan<byte> OrdinaryBody =>
        "{\"http_retry_check\":\"scenario-suite-original\"}\n"u8;

    internal static ReadOnlySpan<byte> ChangedBody =>
        "{\"http_retry_check\":\"scenario-suite-modified\"}\n"u8;

    internal static ReadOnlySpan<byte> NoContentResponse =>
        "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"u8;

    internal static ReadOnlySpan<byte> UnavailableResponse =>
        "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"u8;

    internal static TimeSpan ScenarioTimeout => TimeSpan.FromSeconds(5);

    internal static TimeSpan ConnectionTimeout => TimeSpan.FromSeconds(2);

    internal static TimeSpan CleanupTimeout => TimeSpan.FromSeconds(2);

    internal static TimeSpan DelayedResponseDuration => TimeSpan.FromMilliseconds(250);

    internal static TimeSpan TrailingWindow => TimeSpan.FromMilliseconds(5);
}
