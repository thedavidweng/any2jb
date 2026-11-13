package savekit

import "errors"

// LauncherRequest identifies a pinned package and the user's compatibility
// declarations. It can run only as a confirmed SaveKit write.
type LauncherRequest struct {
	ID                    string
	Version               string
	PhysicalCompatibility bool
	BackupFolder          string
}

func (r *Request) ValidateToolSave(tool string) error {
	if r == nil {
		return nil
	}
	if r.Launcher != nil {
		if tool != "savekit" || r.Action != "write" {
			return errors.New("launcher saves require the SaveKit write gate")
		}
		return r.ValidateLauncherWrite()
	}
	return r.Validate()
}
