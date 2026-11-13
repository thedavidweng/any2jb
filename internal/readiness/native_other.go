//go:build !darwin && !linux

package readiness

import "context"

func observePlatform(context.Context, *Observation) {}
