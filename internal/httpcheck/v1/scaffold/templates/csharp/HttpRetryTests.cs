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
        // Replace this example with the client configuration used by your application.
        using var client = new HttpClient();

        await ScenarioTest.CheckAsync(client, line => TestContext.WriteLine(line));
    }
}
