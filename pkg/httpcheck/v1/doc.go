// Package httpcheck runs a caller-provided HTTP client through six HTTP Retry
// Check scenarios, using fresh 127.0.0.1 listeners for every run. Results
// contain only the observations defined by the suite.
package httpcheck
