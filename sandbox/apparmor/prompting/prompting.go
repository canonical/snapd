// Package prompting implements high-level prompting interface to a subset of AppArmor features
package prompting

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var NotifyPath string

// PromptingSupportAvailable returns true if NotifyPath exists, indicating that
// apparmor prompting messages may be received from NotifyPath.
func PromptingSupportAvailable() bool {
	return osutil.FileExists(NotifyPath)
}

func setupNotifyPath(newrootdir string) {
	NotifyPath = filepath.Join(newrootdir, "/sys/kernel/security/apparmor/.notify")
}

func init() {
	dirs.AddRootDirCallback(setupNotifyPath)
	setupNotifyPath(dirs.GlobalRootDir)
}
