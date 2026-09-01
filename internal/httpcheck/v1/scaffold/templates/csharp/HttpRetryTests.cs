using System.Net;
using System.Net.Http;
using System.Threading.Tasks;
using HttpRetryCheck.V1.Testing;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Scaffold;

[TestClass]
public sealed class HttpRetryTests
{
    public TestContext TestContext { get; set; } = null!;

    [TestMethod]
    public async Task TestHttpRetryScenarios()
    {
        using var handler = new SocketsHttpHandler
        {
            AllowAutoRedirect = true,
            MaxAutomaticRedirections = 2,
            UseCookies = false,
            UseProxy = false,
        };
        using var client = new HttpClient(handler)
        {
            DefaultRequestVersion = HttpVersion.Version11,
            DefaultVersionPolicy = HttpVersionPolicy.RequestVersionExact,
        };

        await ScenarioTest.CheckAsync(client, line => TestContext.WriteLine(line));
    }
}
