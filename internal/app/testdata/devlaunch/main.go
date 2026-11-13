// This isolated probe reports what the developer-launch gate does in the build
// it was built into; developer_launch_test.go builds it both ways.
package main

import (
	"fmt"
	"os"

	"github.com/thedavidweng/any2jb/internal/devlaunch"
)

func main() {
	config, err := devlaunch.Load(os.Getenv("ANY2JB_DEVELOPER_CONFIG"))
	if err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	if config.SourceRoot == "" {
		fmt.Println("not staged")
		return
	}
	fmt.Println("staged " + config.SourceRoot)
}
