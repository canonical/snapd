// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package interfaces

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snapdtool"
)

// ErrSystemKeyIncomparableVersions indicates that the system-key
// on disk and the system-key calculated from generateSystemKey
// have different inputs and are therefore incomparable.
//
// This means:
// - "snapd" needs to re-generate security profiles
// - "snap run" cannot wait for those security profiles
var (
	ErrSystemKeyVersion = errors.New("system-key versions not comparable")
	ErrSystemKeyMissing = errors.New("system-key missing on disk")
)

// systemKey describes the environment for which security profiles
// have been generated. It is useful to compare if the current
// running system is similar enough to the generated profiles or
// if the profiles need to be re-generated to match the new system.
//
// Note that this key gets generated on *each* `snap run` - so it
// *must* be cheap to calculate it (no hashes of big binaries etc).
type systemKey struct {
	// IMPORTANT: when adding/removing/changing inputs bump this version (see below)
	Version int `json:"version"`

	// This is the build-id of the snapd that generated the profiles.
	BuildID string `json:"build-id"`

	// These inputs come from the host environment via e.g.
	// kernel version or similar settings. If those change we may
	// need to change the generated profiles (e.g. when the user
	// boots into a more featureful seccomp).
	//
	// As an exception, the NFSHome is not renamed to RemoteFSHome
	// to avoid needless re-computation.
	AppArmorFeatures       []string `json:"apparmor-features"`
	AppArmorParserMtime    int64    `json:"apparmor-parser-mtime"`
	AppArmorParserFeatures []string `json:"apparmor-parser-features"`
	AppArmorPrompting      bool     `json:"apparmor-prompting"`
	NFSHome                bool     `json:"nfs-home"`
	OverlayRoot            string   `json:"overlay-root"`
	SecCompActions         []string `json:"seccomp-features"`
	SeccompCompilerVersion string   `json:"seccomp-compiler-version"`
	CgroupVersion          string   `json:"cgroup-version"`
}

// IMPORTANT: when adding/removing/changing inputs bump this
const systemKeyVersion = 11

var (
	isHomeUsingRemoteFS   = osutil.IsHomeUsingRemoteFS
	isRootWritableOverlay = osutil.IsRootWritableOverlay
	mockedSystemKey       *systemKey

	readBuildID = osutil.ReadBuildID
)

func seccompCompilerVersionInfo(path string) (seccomp.VersionInfo, error) {
	return seccomp.CompilerVersionInfo(func(name string) (string, error) { return filepath.Join(path, name), nil })
}

func generateSystemKey() (*systemKey, error) {
	// for testing only
	if mockedSystemKey != nil {
		return mockedSystemKey, nil
	}

	sk := &systemKey{
		Version: systemKeyVersion,
	}
	snapdPath, err := snapdtool.InternalToolPath("snapd")
	if err != nil {
		return nil, err
	}
	buildID, err := readBuildID(snapdPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sk.BuildID = buildID

	// Add apparmor-features (which is already sorted)
	sk.AppArmorFeatures, _ = apparmor.KernelFeatures()

	// Add apparmor-parser-mtime
	sk.AppArmorParserMtime = apparmor.ParserMtime()

	if features.AppArmorPrompting.IsEnabled() {
		// Add AppArmor prompting status
		sk.AppArmorPrompting = features.AppArmorPrompting.IsSupported()
	}

	// Add if home is using a remote file system, if so we need to have a
	// different security profile and if this changes we need to change our
	// profile.
	sk.NFSHome, err = isHomeUsingRemoteFS()
	if err != nil {
		// just log the error here
		logger.Noticef("cannot determine nfs usage in generateSystemKey: %v", err)
		return nil, err
	}

	// Add if '/' is on overlayfs so we can add AppArmor rules for
	// upperdir such that if this changes, we change our profile.
	sk.OverlayRoot, err = isRootWritableOverlay()
	if err != nil {
		// just log the error here
		logger.Noticef("cannot determine root filesystem on overlay in generateSystemKey: %v", err)
		return nil, err
	}

	// Add seccomp-features
	sk.SecCompActions = seccomp.Actions()

	versionInfo, err := seccompCompilerVersionInfo(filepath.Dir(snapdPath))
	if err != nil {
		logger.Noticef("cannot determine seccomp compiler version in generateSystemKey: %v", err)
		return nil, err
	}
	sk.SeccompCompilerVersion = string(versionInfo)

	cgv, err := cgroup.Version()
	if err != nil {
		logger.Noticef("cannot determine cgroup version: %v", err)
		return nil, err
	}
	sk.CgroupVersion = strconv.FormatInt(int64(cgv), 10)

	return sk, nil
}

// UnmarshalJSONSystemKey unmarshalls the data from the reader as JSON into a
// system key usable with SystemKeysMatch.
func UnmarshalJSONSystemKey(r io.Reader) (interface{}, error) {
	sk := &systemKey{}
	err := json.NewDecoder(r).Decode(sk)
	if err != nil {
		return nil, err
	}
	return sk, nil
}

// WriteSystemKey will write the current system-key to disk
func WriteSystemKey() error {
	sk, err := generateSystemKey()
	if err != nil {
		return err
	}

	// only fix AppArmorParserFeatures if we didn't already mock a system-key
	// if we mocked a system-key we are running a test and don't want to use
	// the real host system's parser features
	if mockedSystemKey == nil {
		// We only want to calculate this when the mtime of the parser changes.
		// Since we calculate the mtime() as part of generateSystemKey, we can
		// simply unconditionally write this out here.
		sk.AppArmorParserFeatures, _ = apparmor.ParserFeatures()
	}

	sks, err := json.Marshal(sk)
	if err != nil {
		return err
	}
	return osutil.AtomicWriteFile(dirs.SnapSystemKeyFile, sks, 0644, 0)
}

// SystemKeyMismatch checks if the running binary expects a different
// system-key than what is on disk.
//
// This is used in two places:
//   - snap run: when there is a mismatch it will wait for snapd
//     to re-generate the security profiles
//   - snapd: on startup it checks if the system-key has changed and
//     if so re-generate the security profiles
//
// This ensures that "snap run" and "snapd" have a consistent set
// of security profiles. Without it we may have the following
// scenario:
//  1. snapd gets refreshed and snaps need updated security profiles
//     to work (e.g. because snap-exec needs a new permission)
//  2. The system reboots to start the new snapd. At this point
//     the old security profiles are on disk (because the new
//     snapd did not run yet)
//  3. Snaps that run as daemon get started during boot by systemd
//     (e.g. network-manager). This may happen before snapd had a
//     chance to refresh the security profiles.
//  4. Because the security profiles are for the old version of
//     the snaps that run before snapd fail to start. For e.g.
//     network-manager this is of course catastrophic.
//
// To prevent this, in step(4) we have this wait-for-snapd
// step to ensure the expected profiles are on disk.
//
// The apparmor-parser-features system-key is handled specially
// and not included in this comparison because it is written out
// to disk whenever apparmor-parser-mtime changes (in this manner
// snap run only has to obtain the mtime of apparmor_parser and
// doesn't have to invoke it)
func SystemKeyMismatch() (bool, error) {
	mySystemKey, err := generateSystemKey()
	if err != nil {
		return false, err
	}

	diskSystemKey, err := readSystemKey()
	if err != nil {
		return false, err
	}

	// deal with the race that "snap run" may start, then snapd
	// is upgraded and generates a new system-key with different
	// inputs than the "snap run" in memory. In this case we
	// should be fine because new security profiles will also
	// have been written to disk.
	if mySystemKey.Version != diskSystemKey.Version {
		return false, ErrSystemKeyVersion
	}

	// special case to detect local runs
	if mockedSystemKey == nil {
		if exe, err := os.Readlink("/proc/self/exe"); err == nil {
			// detect running local local builds
			if !strings.HasPrefix(exe, "/usr") && !strings.HasPrefix(exe, "/snap") {
				logger.Noticef("running from non-installed location %s: ignoring system-key", exe)
				return false, ErrSystemKeyVersion
			}
		}
	}

	// since we always write out apparmor-parser-feature when
	// apparmor-parser-mtime changes, we don't need to compare it here
	// (allowing snap run to only need to check the mtime of the parser)
	// so just set both to nil to make the DeepEqual happy
	diskSystemKey.AppArmorParserFeatures = nil
	mySystemKey.AppArmorParserFeatures = nil

	ok, err := SystemKeysMatch(mySystemKey, diskSystemKey)
	return !ok, err
}

func readSystemKey() (*systemKey, error) {
	raw, err := os.ReadFile(dirs.SnapSystemKeyFile)
	if err != nil && os.IsNotExist(err) {
		return nil, ErrSystemKeyMissing
	}
	if err != nil {
		return nil, err
	}
	var diskSystemKey systemKey
	if err := json.Unmarshal(raw, &diskSystemKey); err != nil {
		return nil, err
	}
	return &diskSystemKey, nil
}

// RecordedSystemKey returns the system key read from the disk as opaque interface{}.
func RecordedSystemKey() (interface{}, error) {
	diskSystemKey, err := readSystemKey()
	if err != nil {
		return nil, err
	}
	return diskSystemKey, nil
}

// CurrentSystemKey calculates and returns the current system key as opaque interface{}.
func CurrentSystemKey() (interface{}, error) {
	currentSystemKey, err := generateSystemKey()
	return currentSystemKey, err
}

// SystemKeysMatch returns whether the given system keys match.
func SystemKeysMatch(systemKey1, systemKey2 interface{}) (bool, error) {
	// precondition check
	_, ok1 := systemKey1.(*systemKey)
	_, ok2 := systemKey2.(*systemKey)
	if !(ok1 && ok2) {
		return false, fmt.Errorf("SystemKeysMatch: arguments are not system keys")
	}

	// TODO: write custom struct compare
	return reflect.DeepEqual(systemKey1, systemKey2), nil
}

// RemoveSystemKey removes the system key from the disk.
func RemoveSystemKey() error {
	err := os.Remove(dirs.SnapSystemKeyFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func MockSystemKey(s string) func() {
	var sk systemKey
	err := json.Unmarshal([]byte(s), &sk)
	if err != nil {
		panic(err)
	}
	mockedSystemKey = &sk
	return func() { mockedSystemKey = nil }
}
