# HTTP Retry Check test scaffold

Copy this scaffold into a .NET 10 MSTest project with HttpRetryCheck installed.
Replace the example client with the configuration your application uses, then
run:

    dotnet test

The test fails on unsafe or inconclusive results. Defaults are a 5-second
scenario timeout, 2-second connection timeout, 300-millisecond quiet window,
and 2-attempt limit. The C# guide documents bounded options and an optional
proxy-free HTTP/1 baseline. A green result covers only these six local
scenarios; it does not certify or sandbox the client.
