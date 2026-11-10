//go:build !darwin

package driver

import (
	"context"
	"errors"
)

func administrator(context.Context, string) error {
	return errors.New("driver installation requires Apple Silicon macOS")
}
