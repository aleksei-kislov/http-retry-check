module github.com/aleksei-kislov/http-retry-check/examples/adapters/go-retryablehttp

go 1.25

require (
	github.com/aleksei-kislov/http-retry-check v0.0.0
	github.com/hashicorp/go-retryablehttp v0.7.8
)

require github.com/hashicorp/go-cleanhttp v0.5.2 // indirect

replace github.com/aleksei-kislov/http-retry-check => ../../..
