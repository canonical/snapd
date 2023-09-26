// Package notify implements high-level notify interface to a subset of AppArmor features
package notify

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var SysPath string

// SupportAvailable returns true if SysPath exists, indicating that apparmor
// prompting messages may be received from SysPath.
func SupportAvailable() bool {
	return osutil.FileExists(SysPath)
}

func setupSysPath(newrootdir string) {
	SysPath = filepath.Join(newrootdir, "/sys/kernel/security/apparmor/.notify")
}

func init() {
	dirs.AddRootDirCallback(setupSysPath)
	setupSysPath(dirs.GlobalRootDir)
}
