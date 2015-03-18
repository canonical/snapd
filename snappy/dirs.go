package snappy

import (
	"path/filepath"
)

// the various file paths
var (
	globalRootDir string

	snapAppsDir      string
	snapOemDir       string
	snapDataDir      string
	snapDataHomeGlob string
	snapAppArmorDir  string

	snapBinariesDir string
	snapServicesDir string

	clickSystemHooksDir string
	cloudMetaDataFile   string
)

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	globalRootDir = rootdir

	snapAppsDir = filepath.Join(rootdir, "/apps")
	snapOemDir = filepath.Join(rootdir, "/oem")
	snapDataDir = filepath.Join(rootdir, "/var/lib/apps")
	snapDataHomeGlob = filepath.Join(rootdir, "/home/*/apps/")
	snapAppArmorDir = filepath.Join(rootdir, "/var/lib/apparmor/clicks")

	snapBinariesDir = filepath.Join(rootdir, snapAppsDir, "bin")
	snapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")

	clickSystemHooksDir = filepath.Join(rootdir, "/usr/share/click/hooks")

	cloudMetaDataFile = filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net/meta-data")
}

func init() {
	// init the global directories at startup
	SetRootDir("/")
}
