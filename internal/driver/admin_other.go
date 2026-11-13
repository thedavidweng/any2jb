//go:build !darwin

package driver

import (
	"context"

	"github.com/thedavidweng/any2jb/locales"
)

func administrator(context.Context, string) error {
	return locales.Key("dynDrvRequiresAppleSilicon")
}
