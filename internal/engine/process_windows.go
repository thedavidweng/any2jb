package engine

import "os"

// Windows is a compile gate, not a supported process supervisor yet. Refuse to
// launch rather than pretending Process.Kill terminates a whole process tree.
func ownedFile(info os.FileInfo) bool { return false }
