// check-package is the MyGo resource redistribution gate. The build runs it
// over the source resources tree; -merged checks an assembled app instead.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

func main() {
	if err := check(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(args []string) error {
	flags := flag.NewFlagSet("check-package", flag.ContinueOnError)
	merged := flags.Bool("merged", false, "check an assembled app's Contents/Resources or platform bundle instead of the source resources tree")
	platform := flags.String("platform", "", "target platform (default: darwin-arm64 when -merged is set, or all platforms for source resources)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root := "resources"
	switch {
	case flags.NArg() == 1:
		// An explicit root checks staged resources without consulting build config.
		root = flags.Arg(0)
	case flags.NArg() > 1:
		return fmt.Errorf("usage: check-package [-merged] [-platform <target>] [resource-root]")
	case *merged:
		return fmt.Errorf("check-package -merged needs the assembled Contents/Resources path")
	default:
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
	target := *platform
	if *merged {
		if target == "" {
			if runtime.GOOS == "linux" {
				target = runtime.GOOS + "-" + runtime.GOARCH
			} else {
				target = "darwin-arm64"
			}
		}
		return m.CheckPackageMergedForPlatform(root, target)
	}
	if target == "" && runtime.GOOS == "linux" {
		target = runtime.GOOS + "-" + runtime.GOARCH
	}
	if target != "" {
		return m.CheckPackageForPlatform(root, target)
	}
	return m.CheckPackage(root)
}
