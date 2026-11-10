package savekit

// LauncherRequest identifies a pinned package and the user's compatibility
// declarations. It can run only as a confirmed SaveKit write.
type LauncherRequest struct {
	ID                    string
	Version               string
	PhysicalCompatibility bool
	BackupFolder          string
}
