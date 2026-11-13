// prepare-runtime fetches and verifies the pinned, redistributable Node sidecars.
package main

import (
	"flag"
	"log"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
)

func main() {
	platform := flag.String("platform", "", "prepare only one platform target (default: all pinned targets)")
	flag.Parse()
	m, err := manifest.Load()
	if err != nil {
		log.Fatal(err)
	}
	if *platform != "" {
		err = engine.PrepareNodeForPlatform(".cache", "resources", *platform, m)
	} else {
		err = engine.PrepareNode(".cache", "resources", m)
	}
	if err != nil {
		log.Fatal(err)
	}
}
