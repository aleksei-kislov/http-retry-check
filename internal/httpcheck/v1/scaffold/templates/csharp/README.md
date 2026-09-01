# HTTP Retry Check test scaffold

Copy this scaffold into a .NET 10 MSTest project that references HttpRetryCheck, Microsoft.NET.Test.Sdk 18.9.0, MSTest.TestFramework 4.3.3, and MSTest.TestAdapter 4.3.3. Run:

    dotnet test

The test fails on unsafe or inconclusive results. A green result covers only these six local scenarios; it does not certify the client or run it in a sandbox.
