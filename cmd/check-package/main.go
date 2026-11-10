// check-package is the MyGo pre-build resource redistribution gate.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/thedavidweng/any2jb/manifest"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	root := "resources"
	if len(os.Args) == 2 {
		// An explicit root checks staged resources without consulting build config.
		root = os.Args[1]
	} else if len(os.Args) != 1 {
		return fmt.Errorf("usage: check-package [staged-resource-root]")
	} else {
		data, err := os.ReadFile("mygo.json")
		if err != nil {
			return err
		}
		var config struct {
			Resources    []string `json:"resources"`
			FrontendDist string   `json:"frontendDist"`
			Icon         string   `json:"icon"`
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return err
		}
		if len(config.Resources) != 0 || config.FrontendDist != "" || config.Icon != "" {
			return fmt.Errorf("package gate supports only the resources directory; extra resources, frontendDist and custom icon need an audited staging step")
		}
	}
	m, err := manifest.Load()
	if err != nil {
		return err
	}
	return m.CheckPackage(root)
}
