using System;
using System.Collections.Generic;
using System.Net;
using System.Net.Sockets;
using System.Threading;

namespace HttpRetryCheck.V1.Runtime;

internal enum EndpointRole
{
    Source = 1,
    RedirectTarget = 2,
}

internal sealed class AdmittedEndpoint
{
    private readonly object sync = new();
    private bool closed;

    internal AdmittedEndpoint(TcpListener listener, IPEndPoint address, EndpointRole role)
    {
        Listener = listener;
        Address = address;
        Role = role;
    }

    internal TcpListener Listener { get; }

    internal IPEndPoint Address { get; }

    internal EndpointRole Role { get; }

    internal bool Close()
    {
        lock (sync)
        {
            if (closed)
            {
                return true;
            }

            try
            {
                Listener.Stop();
                closed = true;
                return true;
            }
            catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
            {
                return false;
            }
        }
    }
}

internal sealed class AdmittedScenario
{
    internal AdmittedScenario(ScenarioId scenario, IReadOnlyList<AdmittedEndpoint> endpoints)
    {
        Scenario = scenario;
        Endpoints = endpoints;
    }

    internal ScenarioId Scenario { get; }

    internal IReadOnlyList<AdmittedEndpoint> Endpoints { get; }

    internal bool Close()
    {
        var succeeded = true;
        foreach (var endpoint in Endpoints)
        {
            succeeded = endpoint.Close() && succeeded;
        }

        return succeeded;
    }
}

internal sealed class AdmittedSuite
{
    internal AdmittedSuite(IReadOnlyList<AdmittedScenario> scenarios)
    {
        Scenarios = scenarios;
    }

    internal IReadOnlyList<AdmittedScenario> Scenarios { get; }

    internal bool CloseFrom(int index)
    {
        var succeeded = true;
        for (var current = index; current < Scenarios.Count; current++)
        {
            succeeded = Scenarios[current].Close() && succeeded;
        }

        return succeeded;
    }
}

internal readonly record struct AdmissionResult(
    AdmittedSuite? Suite,
    SuiteFailureCode Failure);

internal sealed class AdmissionObserver
{
    private readonly Action<int, int> endpointOpened;

    internal AdmissionObserver(Action<int, int> endpointOpened)
    {
        this.endpointOpened = endpointOpened ?? throw new ArgumentNullException(nameof(endpointOpened));
    }

    internal void EndpointOpened(int ordinal, int port)
    {
        endpointOpened(ordinal, port);
    }
}

internal static class SuiteAdmission
{
    internal static AdmissionResult Open(
        CancellationToken cancellationToken,
        AdmissionObserver? observer,
        int maximumObservedAttempts)
    {
        var scenarios = new List<AdmittedScenario>(RuntimeConstants.ScenarioCount);
        var ports = new HashSet<int>();

        try
        {
            for (var scenarioIndex = 0; scenarioIndex < SemanticEvaluator.ScenarioCount; scenarioIndex++)
            {
                var scenario = SemanticEvaluator.ScenarioAt(scenarioIndex);
                if (cancellationToken.IsCancellationRequested)
                {
                    return Failed(scenarios, null, SuiteFailureCode.SuiteUnavailable);
                }

                var endpoints = new List<AdmittedEndpoint>(
                    scenario == ScenarioId.CrossOriginRedirectCredentials ? 2 : 1);
                var endpointCount = scenario == ScenarioId.CrossOriginRedirectCredentials ? 2 : 1;
                for (var index = 0; index < endpointCount; index++)
                {
                    if (cancellationToken.IsCancellationRequested)
                    {
                        return Failed(scenarios, endpoints, SuiteFailureCode.SuiteUnavailable);
                    }

                    TcpListener? listener = null;
                    try
                    {
                        listener = new TcpListener(new IPAddress(new byte[] { 127, 0, 0, 1 }), 0);
                        listener.Start(maximumObservedAttempts + 1);
                        if (listener.LocalEndpoint is not IPEndPoint address ||
                            !address.Address.Equals(new IPAddress(new byte[] { 127, 0, 0, 1 })) ||
                            address.Port == 0 ||
                            !ports.Add(address.Port))
                        {
                            return FailedListener(
                                scenarios,
                                endpoints,
                                listener,
                                SuiteFailureCode.SuiteUnavailable);
                        }

                        var role = index == 0 ? EndpointRole.Source : EndpointRole.RedirectTarget;
                        endpoints.Add(new AdmittedEndpoint(listener, address, role));
                        listener = null;
                        observer?.EndpointOpened(ports.Count, address.Port);
                    }
                    catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
                    {
                        var currentClosed = true;
                        if (listener is not null)
                        {
                            try
                            {
                                listener.Stop();
                            }
                            catch (Exception closeException) when (RuntimeFailure.IsRecoverable(closeException))
                            {
                                currentClosed = false;
                            }
                        }

                        var failure = Failed(scenarios, endpoints, SuiteFailureCode.SuiteUnavailable);
                        return currentClosed
                            ? failure
                            : new AdmissionResult(null, SuiteFailureCode.InternalFailure);
                    }
                }

                scenarios.Add(new AdmittedScenario(scenario, endpoints.AsReadOnly()));
            }

            if (cancellationToken.IsCancellationRequested)
            {
                return Failed(scenarios, null, SuiteFailureCode.SuiteUnavailable);
            }

            return new AdmissionResult(new AdmittedSuite(scenarios.AsReadOnly()), 0);
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            return Failed(scenarios, null, SuiteFailureCode.SuiteUnavailable);
        }
    }

    private static AdmissionResult FailedListener(
        List<AdmittedScenario> scenarios,
        List<AdmittedEndpoint> endpoints,
        TcpListener listener,
        SuiteFailureCode preferredFailure)
    {
        var listenerClosed = true;
        try
        {
            listener.Stop();
        }
        catch (Exception exception) when (RuntimeFailure.IsRecoverable(exception))
        {
            listenerClosed = false;
        }

        var result = Failed(scenarios, endpoints, preferredFailure);
        return listenerClosed
            ? result
            : new AdmissionResult(null, SuiteFailureCode.InternalFailure);
    }

    private static AdmissionResult Failed(
        List<AdmittedScenario> scenarios,
        List<AdmittedEndpoint>? pending,
        SuiteFailureCode preferredFailure)
    {
        var closed = true;
        if (pending is not null)
        {
            foreach (var endpoint in pending)
            {
                closed = endpoint.Close() && closed;
            }
        }

        foreach (var scenario in scenarios)
        {
            closed = scenario.Close() && closed;
        }

        return new AdmissionResult(
            null,
            closed ? preferredFailure : SuiteFailureCode.InternalFailure);
    }
}
