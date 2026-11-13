//go:build windows

package usbgadget

import "errors"

func openHidgEndpoint(string) (Endpoint, error) {
	return nil, errors.New("Linux ConfigFS hidg endpoints are unsupported on Windows")
}

func isTemporaryHIDError(error) bool { return false }

func configFSGadgetWritable() bool { return false }
