// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// ErrSystemKeyIncomparableVersions indicates that the system-key
// on disk and the system-key calculated from generateSystemKey
// have different inputs and are therefor incomparable.
//
// This means:
// - "snapd" needs to re-generate security profiles
// - "snap run" cannot wait for those security profiles
var (
	ErrSystemKeyIncomparableVersions = errors.New("system-key version mismatch")
	ErrSystemKeyMissing              = errors.New("system-key missing on disk")
)

// systemKey describes the environment for which security profiles
// have been generated. It is useful to compare if the current
// running system is similar enough to the generated profiles or
// if the profiles need to be re-generated to match the new system.
//
// Note that this key gets generated on *each* `snap run` - so it
// *must* be cheap to calculate it (no hashes of big binaries etc).
type systemKey struct {
	// IMPORTANT: when adding new inputs bump this version
	Version string `json:"version"`

	BuildID          string   `json:"build_id"`
	AppArmorFeatures []string `json:"apparmor_features"`
	NFSHome          bool     `json:"nfs_home"`
	OverlayRoot      string   `json:"overlay_root"`
	Core             string   `json:"core,omitempty"`
	SecCompActions   []string `json:"seccomp_features"`
}

var (
	isHomeUsingNFS  = osutil.IsHomeUsingNFS
	mockedSystemKey *systemKey
)

func generateSystemKey() *systemKey {
	// for testing only
	if mockedSystemKey != nil {
		return mockedSystemKey
	}

	var sk systemKey
	sk.Version = "1"
	buildID, err := osutil.MyBuildID()
	if err != nil {
		buildID = ""
	}
	sk.BuildID = buildID

	// Add apparmor-features (which is already sorted)
	sk.AppArmorFeatures = release.AppArmorFeatures()

	// Add if home is using NFS, if so we need to have a different
	// security profile and if this changes we need to change our
	// profile.
	sk.NFSHome, err = isHomeUsingNFS()
	if err != nil {
		logger.Noticef("cannot determine nfs usage in generateSystemKey: %v", err)
	}

	// Add if '/' is on overlayfs so we can add AppArmor rules for
	// upperdir such that if this changes, we change our profile.
	sk.OverlayRoot, err = osutil.IsRootWritableOverlay()
	if err != nil {
		logger.Noticef("cannot determine root filesystem on overlay in generateSystemKey: %v", err)
	}

	// Add the current Core path, we need this because we call helpers
	// like snap-confine from core that will need an updated profile
	// if it changes
	//
	// FIXME: what about core18? the snapd snap?
	sk.Core, _ = os.Readlink(filepath.Join(dirs.SnapMountDir, "core/current"))

	// Add seccomp-features
	sk.SecCompActions = release.SecCompActions

	return &sk
}

// WriteSystemKey will write the current system-key to disk
func WriteSystemKey() error {
	sk := generateSystemKey()

	// special case: unknown build-ids always trigger a rebuild
	if sk.BuildID == "" {
		return nil
	}
	sks, err := json.Marshal(sk)
	if err != nil {
		panic(err)
	}
	return osutil.AtomicWriteFile(dirs.SnapSystemKeyFile, sks, 0644, 0)
}

// SystemKeyMismatch checks if the running binary expects a different
// system-key than what is on disk.
//
// This is used in two places:
// - snap run: when there is a mismatch it will wait for snapd
//             to re-generate the security profiles
// - snapd: on startup it checks if the system-key has changed and
//          if so re-generate the security profiles
//
// This ensures that "snap run" and "snapd" have a consistent set
// of security profiles. Without it we may have the following
// scenario:
// 1. snapd gets refreshed and snaps need updated security profiles
//    to work (e.g. because snap-exec needs a new permission)
// 2. The system reboots to start the new snapd. At this point
//    the old security profiles are on disk (because the new
//    snapd did not run yet)
// 3. Snaps that run as daemon get started during boot by systemd
//    (e.g. network-manager). This may happen before snapd had a
//    chance to refresh the security profiles.
// 4. Because the security profiles are for the old version of
//    the snaps that run before snapd fail to start. For e.g.
//    network-manager this is of course catastrophic.
// To prevent this, in step(4) we have this wait-for-snapd
// step to ensure the expected profiles are on disk.
func SystemKeyMismatch() (bool, error) {
	mySystemKey := generateSystemKey()

	raw, err := ioutil.ReadFile(dirs.SnapSystemKeyFile)
	if err != nil && os.IsNotExist(err) {
		return false, ErrSystemKeyMissing
	}
	if err != nil {
		return false, err
	}
	var diskSystemKey systemKey
	if err := json.Unmarshal(raw, &diskSystemKey); err != nil {
		return false, err
	}
	// deal with the race that "snap run" may start, then snapd
	// is upgraded and generates a new system-key with different
	// inputs than the "snap run" in memory. In this case we
	// should be fine because new security profiles will also
	// have been written to disk.
	if mySystemKey.Version != diskSystemKey.Version {
		return false, ErrSystemKeyIncomparableVersions
	}
	mySystemKeyJSON, err := json.Marshal(mySystemKey)
	if err != nil {
		return false, err
	}

	return string(mySystemKeyJSON) != string(raw), nil
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
