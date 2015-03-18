package snappy

import (
	"path/filepath"
)

// the various file paths
var (
	snapAppsDir      string
	snapOemDir       string
	snapDataDir      string
	snapDataHomeGlob string
	snapAppArmorDir  string

	snapBinariesDir string
	snapServicesDir string
)

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	snapAppsDir = filepath.Join(rootdir, "/apps")
	snapOemDir = filepath.Join(rootdir, "/oem")
	snapDataDir = filepath.Join(rootdir, "/var/lib/apps")
	snapDataHomeGlob = filepath.Join(rootdir, "/home/*/apps/")
	snapAppArmorDir = filepath.Join(rootdir, "/var/lib/apparmor/clicks")

	snapBinariesDir = filepath.Join(rootdir, snapAppsDir, "bin")
	snapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")
}

func init() {
	// init the global directories at startup
	SetRootDir("/")
}
