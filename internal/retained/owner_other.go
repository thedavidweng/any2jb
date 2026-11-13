//go:build !unix

package retained

import (
	"errors"
	"io/fs"
)

func ownedByMe(fs.FileInfo) error {
	return errors.New("checking folder ownership is not supported on this platform yet")
}
