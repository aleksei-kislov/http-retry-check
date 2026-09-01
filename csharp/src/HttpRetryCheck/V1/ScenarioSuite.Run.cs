using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;
using HttpRetryCheck.V1.Runtime;

namespace HttpRetryCheck.V1;

public static partial class ScenarioSuite
{
    public static Task<SuiteResult> RunAsync(
        HttpMessageInvoker client,
        CancellationToken cancellationToken = default)
    {
        return ScenarioRuntime.RunAsync(client, cancellationToken);
    }
}
