// Package prompting implements high-level prompting interface to a subset of AppArmor features
package prompting

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

const notifyPath = "/sys/kernel/security/apparmor/.notify"

func PromptingAvailable() bool {
	return osutil.FileExists(notifyPath)
}

func NotifyPath() string {
	return filepath.Join(dirs.GlobalRootDir, notifyPath)
}
