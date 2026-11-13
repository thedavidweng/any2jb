// This isolated probe is built only by update_test.go into a temporary .app.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/egoist/mygo"
)

type localSource struct{ manifest, archive string }

func (s localSource) Manifest(context.Context, bool) (io.ReadCloser, error) {
	return os.Open(s.manifest)
}

func (s localSource) Open(context.Context, string) (io.ReadCloser, error) {
	return os.Open(s.archive)
}

func main() {
	mygo.Updater.SetSource(localSource{os.Args[1], os.Args[2]})
	up, err := mygo.Updater.Check(context.Background())
	if err == nil && up != nil {
		err = up.Install(context.Background(), nil)
	}
	fmt.Println(err)
}
