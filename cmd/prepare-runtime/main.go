// prepare-runtime fetches, builds, and verifies all pinned runtime sidecars and bundled assets.
package main

import (
	"context"
	"flag"
	"log"
	"runtime"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
)

func main() {
	platform := flag.String("platform", "", "prepare only one platform target (default: all pinned targets on Apple Silicon macOS, or host target on Linux)")
	flag.Parse()
	target := *platform
	if target == "" && runtime.GOOS == "linux" {
		target = runtime.GOOS + "-" + runtime.GOARCH
	}
	m, err := manifest.Load()
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.PrepareRuntime(context.Background(), ".cache", "resources", target, m); err != nil {
		log.Fatal(err)
	}
}
