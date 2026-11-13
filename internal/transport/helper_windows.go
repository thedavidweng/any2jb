package transport

import (
	"errors"
	"os"
)

func lockHelperLease(*os.File) error {
	return errors.New("Darwin USB leases are unsupported on Windows")
}
