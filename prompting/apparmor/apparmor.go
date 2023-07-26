// Package apparmor implements high-level interface to a subset of AppArmor features
package apparmor

import (
	"github.com/snapcore/snapd/osutil"
)

const notifyPath = "/sys/kernel/security/apparmor/.notify"

func PromptingAvailable() bool {
	return osutil.FileExists(notifyPath)
}

func NotifyPath() string {
	return notifyPath
}
