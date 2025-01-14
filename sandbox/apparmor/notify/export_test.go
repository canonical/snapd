package notify

import (
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/testutil"
)

var (
	Versions                  = versions
	VersionSupportedCallbacks = versionSupportedCallbacks

	Supported                = Version.supported
	SupportedProtocolVersion = supportedProtocolVersion
)

func MockSyscall(syscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno)) (restore func()) {
	return testutil.Mock(&doSyscall, syscall)
}

type VersionAndCallback struct {
	Version  Version
	Callback func() bool
}

func MockVersionSupportedCallbacks(pairs []VersionAndCallback) (restore func()) {
	restoreVersions := testutil.Backup(&versions)
	restoreCallbacks := testutil.Backup(&versionSupportedCallbacks)
	restore = func() {
		restoreCallbacks()
		restoreVersions()
	}
	versions = make([]Version, 0, len(pairs))
	versionSupportedCallbacks = make(map[Version]func() bool, len(pairs))
	for _, pair := range pairs {
		versions = append(versions, pair.Version)
		versionSupportedCallbacks[pair.Version] = pair.Callback
	}
	return restore
}
