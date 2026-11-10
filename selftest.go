package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
)

func packagedSelfTest() error {
	assets, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return err
	}
	m, err := manifest.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := engine.SelfTest(ctx, assets, mygo.App.Version(), m); err != nil {
		return err
	}
	fmt.Println("any2jb self-test passed: integrity, Node and bridge; no console or USB access")
	return nil
}
