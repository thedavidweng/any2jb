// prepare-runtime fetches and verifies the pinned, redistributable Node sidecar.
package main

import (
	"log"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
)

func main() {
	m, err := manifest.Load()
	if err != nil {
		log.Fatal(err)
	}
	if err := engine.PrepareNode(".cache", "resources", m); err != nil {
		log.Fatal(err)
	}
}
