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

func MockApparmorMetadataTagsSupported(f func() bool) (restore func()) {
	return testutil.Mock(&apparmorMetadataTagsSupported, f)
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

// TODO: remove this once v5 is no longer manually disabled
func OverrideV5ManuallyDisabled() (restore func()) {
	v5ManuallyDisabled = false
	return func() {
		v5ManuallyDisabled = true
	}
}

func MockIoctl(f func(fd uintptr, req IoctlRequest, buf IoctlRequestBuffer) ([]byte, error)) (restore func()) {
	return testutil.Mock(&doIoctl, f)
}
