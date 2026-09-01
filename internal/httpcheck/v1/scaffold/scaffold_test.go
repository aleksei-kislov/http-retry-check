package scaffold

import (
	"bytes"
	"strings"
	"testing"
)

const expectedGoReadme = `# HTTP Retry Check test scaffold

Copy this scaffold into a Go module that depends on github.com/aleksei-kislov/http-retry-check. Run:

    go test -count=1 -run '^TestHTTPRetryScenarios$' -v ./...

The test fails on unsafe or inconclusive results. A green result covers only these six local scenarios; it does not certify the client or run it in a sandbox.
`

const expectedGoTest = `package httpcheck_test

import (
	"net/http"
	"testing"

	httpchecktest "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/testing"
)

func TestHTTPRetryScenarios(t *testing.T) {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy:             nil,
		Protocols:         protocols,
		DisableKeepAlives: true,
	}
	t.Cleanup(transport.CloseIdleConnections)

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}

	httpchecktest.Check(t, client)
}
`

const expectedCSharpReadme = `# HTTP Retry Check test scaffold

Copy this scaffold into a .NET 10 MSTest project that references HttpRetryCheck, Microsoft.NET.Test.Sdk 18.9.0, MSTest.TestFramework 4.3.3, and MSTest.TestAdapter 4.3.3. Run:

    dotnet test

The test fails on unsafe or inconclusive results. A green result covers only these six local scenarios; it does not certify the client or run it in a sandbox.
`

const expectedCSharpTest = `using System.Net;
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
`

func TestScaffoldAssetsMatchEmbeddedUTF8Files(t *testing.T) {
	tests := []struct {
		language string
		names    []string
		bytes    []string
	}{
		{language: "go", names: []string{"README.md", "http_retry_test.go"}, bytes: []string{expectedGoReadme, expectedGoTest}},
		{language: "csharp", names: []string{"README.md", "HttpRetryTests.cs"}, bytes: []string{expectedCSharpReadme, expectedCSharpTest}},
	}
	for _, test := range tests {
		files, err := Files(test.language)
		if err != nil || len(files) != 2 {
			t.Fatalf("%s files = %d/%v", test.language, len(files), err)
		}
		for index, file := range files {
			if file.Name != test.names[index] || string(file.Contents) != test.bytes[index] ||
				bytes.HasPrefix(file.Contents, []byte{0xef, 0xbb, 0xbf}) || bytes.Contains(file.Contents, []byte("\r")) ||
				!bytes.HasSuffix(file.Contents, []byte("\n")) || bytes.HasSuffix(file.Contents, []byte("\n\n")) {
				t.Fatalf("%s[%d] drifted: %q/%q", test.language, index, file.Name, file.Contents)
			}
			for _, forbidden := range []string{"http://", "https://", "DefaultClient", "DefaultTransport", "UseProxy = true", "exec.Command", "Process.Start"} {
				if strings.Contains(string(file.Contents), forbidden) {
					t.Fatalf("%s[%d] contains forbidden marker %q", test.language, index, forbidden)
				}
			}
		}
		files[0].Contents[0] ^= 0xff
		fresh, err := Files(test.language)
		if err != nil || string(fresh[0].Contents) != test.bytes[0] {
			t.Fatal("returned scaffold bytes alias embedded storage")
		}
	}
	for _, language := range []string{"", "Go", "dotnet", "PRIVATE"} {
		files, err := Files(language)
		if err != ErrInvalidLanguage || files != nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("invalid %q = %#v/%v", language, files, err)
		}
	}
}
