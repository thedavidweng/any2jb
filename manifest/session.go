package manifest

import (
	"strings"

	"github.com/thedavidweng/any2jb/session"
)

// SessionProfile projects the canonical host contract for orchestration.
// Capabilities permit tools, not execution without a verified session or consent.
func (p Profile) SessionProfile() session.Profile {
	s := session.Profile{ID: p.ID, Revision: p.Revision, SourceSHA256: p.SourceSHA256,
		BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName,
		StartArgumentCount: p.StartArgumentCount, StartPort: p.StartPort,
		RetainsRPC: p.SupportsHeldRPC}
	for _, edition := range p.SupportedSmileBASIC {
		_, name, _ := strings.Cut(edition, " ")
		s.Editions = append(s.Editions, name)
	}
	// Read-only tools come with the retained lease itself; the write tools below
	// additionally need their own host capability. Neither is consent or a
	// redistribution grant: the adapter still consults DistributionFor.
	if p.SupportsHeldRPC {
		s.Tools = append(s.Tools, "rpc-status", "system-code-dump",
			"ouncecake-webdav", "ouncecake-ftp", "sd-browser")
	}
	for _, tool := range []struct {
		name      string
		supported bool
	}{
		{"savekit-read", p.SupportsSaveKit},
		{"savekit", p.SupportsSaveKit}, {"native-hblauncher", p.SupportsNativeHBLauncher}, {"modloader", p.SupportsModloader},
	} {
		if tool.supported {
			s.Tools = append(s.Tools, tool.name)
		}
	}
	return s
}
