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
	"crypto"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// systemKey describes the environment for which security profiles
// have been generated. It is useful to compare if the current
// running system is similar enough to the generated profiles or
// if the profiles need to be re-generated to match the new system.
type systemKey struct {
	BuildID          string   `yaml:"build-id"`
	AppArmorFeatures []string `yaml:"apparmor-features"`
	NFSHome          bool     `yaml:"nfs-home"`
	OverlayRoot      string   `yaml:"overlay-root"`
	Core             string   `yaml:"core,omitempty"`
	SecCompActions   []string `yaml:"seccomp-features"`

	CoreSnapConfineProfileID string `yaml:"core-snap-confine-profile-id,omitempty"`
}

var mockedSystemKey *systemKey

func generateSystemKey() *systemKey {
	// for testing only
	if mockedSystemKey != nil {
		return mockedSystemKey
	}

	var sk systemKey
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
	sk.NFSHome, err = osutil.IsHomeUsingNFS()
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

	// Take the apparmor profile of the snap-confine on the core into
	// account - it should only change if the core changes. However
	// in tests we do change this without changing core (and vice
	// versa).
	sk.CoreSnapConfineProfileID = coreSnapConfineProfileID(sk.Core)

	// Add seccomp-features
	sk.SecCompActions = release.SecCompActions

	return &sk
}

func coreSnapConfineProfileID(core string) string {
	snapConfineInCore := filepath.Join(dirs.SnapMountDir, "core", core, "usr/lib/snapd/snap-confine")
	snapConfineInCoreProfileName := strings.Replace(snapConfineInCore[1:], "/", ".", -1)
	hash, _, err := osutil.FileDigest(filepath.Join(dirs.SystemApparmorDir, snapConfineInCoreProfileName), crypto.SHA1)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", hash)
}

// SystemKey outputs a string that identifies what security profiles
// environment this snapd is using. Security profiles that were generated
// with a different Systemkey should be re-generated.
func SystemKey() string {
	sk := generateSystemKey()

	// special case: unknown build-ids always trigger a rebuild
	if sk.BuildID == "" {
		return ""
	}
	sks, err := yaml.Marshal(sk)
	if err != nil {
		panic(err)
	}
	return string(sks)
}

func MockSystemKey(s string) func() {
	var sk systemKey
	err := yaml.Unmarshal([]byte(s), &sk)
	if err != nil {
		panic(err)
	}
	mockedSystemKey = &sk
	return func() { mockedSystemKey = nil }
}
