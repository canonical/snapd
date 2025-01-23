package notify

import (
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/testutil"
)

var (
	Versions                        = versions
	VersionLikelySupportedCallbacks = versionLikelySupportedCallbacks

	LikelySupported                = ProtocolVersion.likelySupported
	LikelySupportedProtocolVersion = likelySupportedProtocolVersion
)

func MockSyscall(syscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno)) (restore func()) {
	return testutil.Mock(&doSyscall, syscall)
}

// VersionAndCallback couples protocol version with a callback function which
// returns true if the version is supported. This type is used so that
// `versions` and `versionLikelySupportedCallbacks` can be mocked to avoid
// calling the actual callback functions (which generally probe the host
// system), and so that the logic around handling of unsupported and supported
// versions can be tested.
type VersionAndCallback struct {
	Version  ProtocolVersion
	Callback func() bool
}

func MockVersionLikelySupportedCallbacks(pairs []VersionAndCallback) (restore func()) {
	restoreVersions := testutil.Backup(&versions)
	restoreCallbacks := testutil.Backup(&versionLikelySupportedCallbacks)
	restore = func() {
		restoreCallbacks()
		restoreVersions()
	}
	versions = make([]ProtocolVersion, 0, len(pairs))
	versionLikelySupportedCallbacks = make(map[ProtocolVersion]func() bool, len(pairs))
	for _, pair := range pairs {
		versions = append(versions, pair.Version)
		versionLikelySupportedCallbacks[pair.Version] = pair.Callback
	}
	return restore
}

func MockIoctl(f func(fd uintptr, req IoctlRequest, buf IoctlRequestBuffer) ([]byte, error)) (restore func()) {
	return testutil.Mock(&doIoctl, f)
}
