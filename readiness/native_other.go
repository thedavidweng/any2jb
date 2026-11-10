//go:build !darwin

package readiness

import "context"

func observePlatform(context.Context, *Observation) {}
