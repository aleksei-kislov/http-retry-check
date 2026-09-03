package localfs

import (
	"go/build"
	"testing"
)

func TestOtherPlatformFallbackSelection(t *testing.T) {
	unsupportedArtifactTargets := []struct {
		goos   string
		goarch string
	}{
		{goos: "freebsd", goarch: "amd64"},
		{goos: "openbsd", goarch: "amd64"},
		{goos: "netbsd", goarch: "amd64"},
		{goos: "dragonfly", goarch: "amd64"},
		{goos: "solaris", goarch: "amd64"},
		{goos: "illumos", goarch: "amd64"},
		{goos: "aix", goarch: "ppc64"},
		{goos: "plan9", goarch: "amd64"},
		{goos: "js", goarch: "wasm"},
		{goos: "wasip1", goarch: "wasm"},
	}
	for _, target := range unsupportedArtifactTargets {
		t.Run(target.goos+"_"+target.goarch, func(t *testing.T) {
			context := build.Default
			context.GOOS = target.goos
			context.GOARCH = target.goarch
			matches, err := context.MatchFile(".", "rename_other.go")
			if err != nil || !matches {
				t.Fatalf("rename_other.go selection = %t/%v", matches, err)
			}
		})
	}

	for _, target := range []struct {
		goos   string
		goarch string
	}{
		{goos: "freebsd", goarch: "amd64"},
		{goos: "openbsd", goarch: "amd64"},
		{goos: "netbsd", goarch: "amd64"},
		{goos: "dragonfly", goarch: "amd64"},
		{goos: "solaris", goarch: "amd64"},
		{goos: "illumos", goarch: "amd64"},
		{goos: "aix", goarch: "ppc64"},
		{goos: "android", goarch: "arm64"},
		{goos: "ios", goarch: "arm64"},
	} {
		t.Run("nonblocking_"+target.goos, func(t *testing.T) {
			context := build.Default
			context.GOOS = target.goos
			context.GOARCH = target.goarch
			matches, err := context.MatchFile(".", "open_unix.go")
			if err != nil || !matches {
				t.Fatalf("open_unix.go selection = %t/%v", matches, err)
			}
		})
	}

	for _, target := range []struct {
		goos   string
		goarch string
	}{
		{goos: "plan9", goarch: "amd64"},
		{goos: "js", goarch: "wasm"},
		{goos: "wasip1", goarch: "wasm"},
	} {
		t.Run("generic_open_"+target.goos, func(t *testing.T) {
			context := build.Default
			context.GOOS = target.goos
			context.GOARCH = target.goarch
			matches, err := context.MatchFile(".", "open_other.go")
			if err != nil || !matches {
				t.Fatalf("open_other.go selection = %t/%v", matches, err)
			}
		})
	}

	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run("excluded_"+goos, func(t *testing.T) {
			context := build.Default
			context.GOOS = goos
			context.GOARCH = "amd64"
			for _, name := range []string{"open_other.go", "rename_other.go"} {
				matches, err := context.MatchFile(".", name)
				if err != nil || matches {
					t.Fatalf("%s exclusion = %t/%v", name, matches, err)
				}
			}
		})
	}
}
