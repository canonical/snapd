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
	"github.com/snapcore/snapd/sandbox/apparmor"
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/testutil"
)

type systemKeySuite struct {
	testutil.BaseTest

	tmp                    string
	apparmorFeatures       string
	buildID                string
	seccompCompilerVersion seccomp_compiler.VersionInfo
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
	s.buildID = "this-is-my-build-id"

	s.seccompCompilerVersion = seccomp_compiler.VersionInfo("123 2.3.3 abcdef123 -")
	testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"), fmt.Sprintf(`
if [ "$1" = "version-info" ]; then echo "%s"; exit 0; fi
exit 1
`, s.seccompCompilerVersion))

	s.AddCleanup(release.MockSecCompActions([]string{"allow", "errno", "kill", "log", "trace", "trap"}))
}

func (s *systemKeySuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	dirs.SetRootDir("/")
}

func (s *systemKeySuite) testInterfaceWriteSystemKey(c *C, nfsHome bool) {
	restore := interfaces.MockIsHomeUsingNFS(func() (bool, error) { return nfsHome, nil })
	defer restore()

	restore = interfaces.MockReadBuildID(func(p string) (string, error) {
		c.Assert(p, Equals, filepath.Join(dirs.DistroLibExecDir, "snapd"))
		return s.buildID, nil
	})
	defer restore()

	err := interfaces.WriteSystemKey()
	c.Assert(err, IsNil)

	systemKey, err := ioutil.ReadFile(dirs.SnapSystemKeyFile)
	c.Assert(err, IsNil)

	kernelFeatures, _ := apparmor.KernelFeatures()

	apparmorFeaturesStr, err := json.Marshal(kernelFeatures)
	c.Assert(err, IsNil)

	apparmorParserMtime, err := json.Marshal(apparmor.ParserMtime())
	c.Assert(err, IsNil)

	parserFeatures, _ := apparmor.ParserFeatures()
	apparmorParserFeaturesStr, err := json.Marshal(parserFeatures)
	c.Assert(err, IsNil)

	seccompActionsStr, err := json.Marshal(release.SecCompActions())
	c.Assert(err, IsNil)

	compiler, err := seccomp_compiler.New(func(name string) (string, error) {
		return filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"), nil
	})
	c.Assert(err, IsNil)
	seccompCompilerVersion, err := compiler.VersionInfo()
	c.Assert(err, IsNil)
	c.Assert(seccompCompilerVersion, Equals, s.seccompCompilerVersion)

	overlayRoot, err := osutil.IsRootWritableOverlay()
	c.Assert(err, IsNil)
	c.Check(string(systemKey), Equals, fmt.Sprintf(`{"version":1,"build-id":"%s","apparmor-features":%s,"apparmor-parser-mtime":%s,"apparmor-parser-features":%s,"nfs-home":%v,"overlay-root":%q,"seccomp-features":%s,"seccomp-compiler-version":"%s"}`, s.buildID, apparmorFeaturesStr, apparmorParserMtime, apparmorParserFeaturesStr, nfsHome, overlayRoot, seccompActionsStr, seccompCompilerVersion))
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyNoNFS(c *C) {
	s.testInterfaceWriteSystemKey(c, false)
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyWithNFS(c *C) {
	s.testInterfaceWriteSystemKey(c, true)
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyErrorOnBuildID(c *C) {
	restore := interfaces.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	restore = interfaces.MockReadBuildID(func(p string) (string, error) {
		c.Assert(p, Equals, filepath.Join(dirs.DistroLibExecDir, "snapd"))
		return "", fmt.Errorf("no build ID for you")
	})
	defer restore()

	err := interfaces.WriteSystemKey()
	c.Assert(err, ErrorMatches, "no build ID for you")
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
"apparmor-features": ["caps", "dbus", "more", "and", "more"]
}
`))
	mismatch, err = interfaces.SystemKeyMismatch()
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, true)
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchParserMtimeHappy(c *C) {
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-parser-mtime": 1234
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

	// change our system-key to have a different parser mtime
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-parser-mtime": 5678
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
