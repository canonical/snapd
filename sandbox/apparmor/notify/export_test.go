package notify

import (
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/testutil"
)

var (
	NativeByteOrder = nativeByteOrder

	Versions                     = versions
	VersionLikelySupportedChecks = versionLikelySupportedChecks

	LikelySupported                = ProtocolVersion.likelySupported
	LikelySupportedProtocolVersion = likelySupportedProtocolVersion
)

func MockSyscall(syscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno)) (restore func()) {
	return testutil.Mock(&doSyscall, syscall)
}

func MockApparmorMetadataTagsSupportedByKernel(f func() bool) (restore func()) {
	return testutil.Mock(&apparmorMetadataTagsSupportedByKernel, f)
}

// VersionAndCheck couples protocol version with a support check function which
// returns true if the version is supported. This type is used so that
// `versions` and `versionLikelySupportedChecks` can be mocked to avoid
// calling the actual check functions (which generally probe the host
// system), and so that the logic around handling of unsupported and supported
// versions can be tested.
type VersionAndCheck struct {
	Version ProtocolVersion
	Check   func() bool
}

func MockVersionLikelySupportedChecks(pairs []VersionAndCheck) (restore func()) {
	restoreVersions := testutil.Backup(&versions)
	restoreChecks := testutil.Backup(&versionLikelySupportedChecks)
	restore = func() {
		restoreChecks()
		restoreVersions()
	}
	versions = make([]ProtocolVersion, 0, len(pairs))
	versionLikelySupportedChecks = make(map[ProtocolVersion]func() bool, len(pairs))
	for _, pair := range pairs {
		versions = append(versions, pair.Version)
		versionLikelySupportedChecks[pair.Version] = pair.Check
	}
	return restore
}

func MockIoctl(f func(fd uintptr, req IoctlRequest, buf IoctlRequestBuffer) ([]byte, error)) (restore func()) {
	return testutil.Mock(&doIoctl, f)
}

func MockOsRemove(f func(path string) error) (restore func()) {
	return testutil.Mock(&osRemove, f)
}
