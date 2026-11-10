//go:build !unix

package session

import (
	"errors"
	"io/fs"
)

// Folder ownership has no portable check here, so user folders are refused
// rather than accepted unchecked.
func ownedByMe(fs.FileInfo) error {
	return errors.New("checking folder ownership is not supported on this platform yet")
}
