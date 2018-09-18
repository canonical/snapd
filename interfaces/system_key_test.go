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

package interfaces_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type systemKeySuite struct {
	testutil.BaseTest

	tmp              string
	apparmorFeatures string
	buildID          string
}

var _ = Suite(&systemKeySuite{})

func (s *systemKeySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmp = c.MkDir()
	dirs.SetRootDir(s.tmp)
	err := os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.DistroLibExecDir, 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("/proc/self/exe", filepath.Join(dirs.DistroLibExecDir, "snapd"))
	c.Assert(err, IsNil)

	s.apparmorFeatures = filepath.Join(s.tmp, "/sys/kernel/security/apparmor/features")
	id, err := osutil.MyBuildID()
	c.Assert(err, IsNil)
	s.buildID = id

	s.AddCleanup(interfaces.MockIsHomeUsingNFS(func() (bool, error) { return false, nil }))
	s.AddCleanup(release.MockSecCompActions([]string{"allow", "errno", "kill", "log", "trace", "trap"}))
}

func (s *systemKeySuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	dirs.SetRootDir("/")
}

func (s *systemKeySuite) TestInterfaceWriteSystemKey(c *C) {
	err := interfaces.WriteSystemKey()
	c.Assert(err, IsNil)

	systemKey, err := ioutil.ReadFile(dirs.SnapSystemKeyFile)
	c.Assert(err, IsNil)

	apparmorFeaturesStr, err := json.Marshal(release.AppArmorFeatures())
	c.Assert(err, IsNil)

	seccompActionsStr, err := json.Marshal(release.SecCompActions())
	c.Assert(err, IsNil)

	nfsHome, err := osutil.IsHomeUsingNFS()
	c.Assert(err, IsNil)

	buildID, err := osutil.ReadBuildID("/proc/self/exe")
	c.Assert(err, IsNil)

	overlayRoot, err := osutil.IsRootWritableOverlay()
	c.Assert(err, IsNil)
	c.Check(string(systemKey), Equals, fmt.Sprintf(`{"version":1,"build-id":"%s","apparmor-features":%s,"nfs-home":%v,"overlay-root":%q,"seccomp-features":%s}`, buildID, apparmorFeaturesStr, nfsHome, overlayRoot, seccompActionsStr))
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchHappy(c *C) {
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}
`))

	// no system-key yet -> Error
	c.Assert(osutil.FileExists(dirs.SnapSystemKeyFile), Equals, false)
	_, err := interfaces.SystemKeyMismatch()
	c.Assert(err, Equals, interfaces.ErrSystemKeyMissing)

	// create a system-key -> no mismatch anymore
	err = interfaces.WriteSystemKey()
	c.Assert(err, IsNil)
	mismatch, err := interfaces.SystemKeyMismatch()
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, false)

	// change our system-key to have more apparmor features
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor_features": ["caps", "dbus", "more", "and", "more"]
}
`))
	mismatch, err = interfaces.SystemKeyMismatch()
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, true)
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchVersions(c *C) {
	// we calculcate v1
	s.AddCleanup(interfaces.MockSystemKey(`
{
"version":1,
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0"
}`))
	// and the on-disk version is v2
	err := ioutil.WriteFile(dirs.SnapSystemKeyFile, []byte(`
{
"version":2,
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0"
}`), 0644)
	c.Assert(err, IsNil)

	// when we encounter different versions we get the right error
	_, err = interfaces.SystemKeyMismatch()
	c.Assert(err, Equals, interfaces.ErrSystemKeyVersion)
}

func (s *systemKeySuite) TestInterfaceSystemKeyFindSnapdPathNormal(c *C) {
	p, err := interfaces.FindSnapdPath()
	c.Assert(err, IsNil)
	c.Check(p, Equals, filepath.Join(dirs.DistroLibExecDir, "snapd"))
}

func (s *systemKeySuite) TestInterfaceSystemKeyFindSnapdPathReexec(c *C) {
	s.AddCleanup(interfaces.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(dirs.SnapMountDir, "core/111/usr/bin/snap"), nil
	}))
	p, err := interfaces.FindSnapdPath()
	c.Assert(err, IsNil)
	c.Check(p, Equals, filepath.Join(dirs.SnapMountDir, "/core/111/usr/lib/snapd/snapd"))
}

func (s *systemKeySuite) TestInterfaceSystemKeyFindSnapdPathSnapdSnap(c *C) {
	s.AddCleanup(interfaces.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(dirs.SnapMountDir, "snapd/22/usr/bin/snap"), nil
	}))
	p, err := interfaces.FindSnapdPath()
	c.Assert(err, IsNil)
	c.Check(p, Equals, filepath.Join(dirs.SnapMountDir, "/snapd/22/usr/lib/snapd/snapd"))
}
