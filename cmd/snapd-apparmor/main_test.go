// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package main_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snapd_apparmor "github.com/snapcore/snapd/cmd/snapd-apparmor"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type mainSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mainSuite{})

func (s *mainSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *mainSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

// Mocks WSL check. Values:
// - 0 to mock not being on WSL.
// - 1 to mock being on WSL 1.
// - 2 to mock being on WSL 2.
func mockWSL(version int) (restore func()) {
	restoreOnWSL := testutil.Backup(&release.OnWSL)
	restoreWSLVersion := testutil.Backup(&release.WSLVersion)

	release.OnWSL = version != 0
	release.WSLVersion = version

	return func() {
		restoreOnWSL()
		restoreWSLVersion()
	}
}

func (s *mainSuite) TestIsContainerWithInternalPolicy(c *C) {
	// since "apparmorfs" is not present within our test root dir setup
	// we expect this to return false
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	appArmorSecurityFSPath := filepath.Join(dirs.GlobalRootDir, "/sys/kernel/security/apparmor/")
	mylog.Check(os.MkdirAll(appArmorSecurityFSPath, 0755))


	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	// simulate being inside WSL
	restore := mockWSL(1)
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, true)
	restore()

	restore = mockWSL(2)
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, true)
	restore()

	for _, prefix := range []string{"lxc", "lxd", "incus"} {
		// simulate being inside a container environment
		restore := testutil.MockCommand(c, "systemd-detect-virt", "echo "+prefix)
		c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)
		mylog.Check(os.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_stacked"), []byte("yes"), 0644))

		c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)
		mylog.Check(os.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_name"), nil, 0644))

		c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)
		mylog.Check(os.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_name"), []byte("foo"), 0644))

		c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)
		mylog.
			// lxc/lxd name should result in a container with internal policy
			Check(os.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_name"), []byte(prefix+"-foo"), 0644))

		c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, true)

		os.Remove(filepath.Join(appArmorSecurityFSPath, ".ns_name"))
		os.Remove(filepath.Join(appArmorSecurityFSPath, ".ns_stacked"))
		restore.Restore()
	}
}

func (s *mainSuite) TestLoadAppArmorProfiles(c *C) {
	parserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer parserCmd.Restore()
	restore := snapd_apparmor.MockParserSearchPath(parserCmd.BinDir())
	defer restore()
	mylog.Check(snapd_apparmor.LoadAppArmorProfiles())

	// since no profiles to load the parser should not have been called
	c.Assert(parserCmd.Calls(), HasLen, 0)
	mylog.

		// mock a profile
		Check(os.MkdirAll(dirs.SnapAppArmorDir, 0755))


	profile := filepath.Join(dirs.SnapAppArmorDir, "foo")
	mylog.Check(os.WriteFile(profile, nil, 0644))


	// pretend that the host apparmor has a 3.0 abi file.
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/apparmor.d/abi"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/etc/apparmor.d/abi/3.0"), nil, 0644), IsNil)

	// ensure SNAPD_DEBUG is set in the environment so then --quiet
	// will *not* be included in the apparmor_parser arguments (since
	// when these test are run in via CI SNAPD_DEBUG is set)
	os.Setenv("SNAPD_DEBUG", "1")
	mylog.Check(snapd_apparmor.LoadAppArmorProfiles())


	// check arguments to the parser are as expected
	c.Assert(parserCmd.Calls(), DeepEquals, [][]string{
		{
			"apparmor_parser", "--policy-features", filepath.Join(dirs.GlobalRootDir, "/etc/apparmor.d/abi/3.0"),
			"--replace", "--write-cache",
			fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir), profile,
		},
	})

	// test error case
	parserCmd = testutil.MockCommand(c, "apparmor_parser", "echo mocked parser failed > /dev/stderr; exit 1")
	defer parserCmd.Restore()
	restore = snapd_apparmor.MockParserSearchPath(parserCmd.BinDir())
	defer restore()
	mylog.Check(snapd_apparmor.LoadAppArmorProfiles())
	c.Check(err.Error(), Equals, "cannot load apparmor profiles: exit status 1\napparmor_parser output:\nmocked parser failed\n")
	mylog.

		// rename so file is ignored
		Check(os.Rename(profile, profile+"~"))

	// forget previous calls so we can check below that as a result of
	// having no profiles again that no invocation of the parser occurs
	parserCmd.ForgetCalls()
	mylog.Check(snapd_apparmor.LoadAppArmorProfiles())

	c.Assert(parserCmd.Calls(), HasLen, 0)
}

func (s *mainSuite) TestIsContainer(c *C) {
	detectCmd := testutil.MockCommand(c, "systemd-detect-virt", "exit 1")
	defer detectCmd.Restore()
	c.Check(snapd_apparmor.IsContainer(), Equals, false)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"},
	})

	detectCmd = testutil.MockCommand(c, "systemd-detect-virt", "")
	c.Check(snapd_apparmor.IsContainer(), Equals, true)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"},
	})

	// test error cases too
	detectCmd = testutil.MockCommand(c, "systemd-detect-virt", "echo failed > /dev/stderr; exit 1")
	c.Check(snapd_apparmor.IsContainer(), Equals, false)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"},
	})

	// Test WSL2 with custom kernel
	// systemd-detect-virt may return a non-zero exit code as it fails to recognize it as WSL
	// This will happen when the kernel name includes neither "WSL" not "Microsoft"
	detectCmd = testutil.MockCommand(c, "systemd-detect-virt", "echo none; exit 1")
	defer mockWSL(2)()
	c.Check(snapd_apparmor.IsContainer(), Equals, true)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string(nil))
}

func (s *mainSuite) TestValidateArgs(c *C) {
	testCases := []struct {
		args   []string
		errMsg string
	}{
		{
			args:   []string{"start"},
			errMsg: "",
		},
		{
			args:   []string{"foo"},
			errMsg: "Expected to be called with a single 'start' argument.",
		},
		{
			args:   []string{"start", "foo"},
			errMsg: "Expected to be called with a single 'start' argument.",
		},
	}
	for _, tc := range testCases {
		mylog.Check(snapd_apparmor.ValidateArgs(tc.args))
	}
}

type integrationSuite struct {
	testutil.BaseTest

	logBuf    *bytes.Buffer
	parserCmd *testutil.MockCmd
}

var _ = Suite(&integrationSuite{})

func (s *integrationSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	logBuf, r := logger.MockLogger()
	s.AddCleanup(r)
	s.logBuf = logBuf

	// simulate a single profile to load
	s.parserCmd = testutil.MockCommand(c, "apparmor_parser", "")
	s.AddCleanup(s.parserCmd.Restore)
	restore := snapd_apparmor.MockParserSearchPath(s.parserCmd.BinDir())
	s.AddCleanup(restore)
	mylog.Check(os.MkdirAll(dirs.SnapAppArmorDir, 0755))

	profile := filepath.Join(dirs.SnapAppArmorDir, "foo")
	mylog.Check(os.WriteFile(profile, nil, 0644))


	os.Args = []string{"snapd-apparmor", "start"}
}

func (s *integrationSuite) TestRunInContainerSkipsLoading(c *C) {
	testutil.MockCommand(c, "systemd-detect-virt", "exit 0")
	mylog.Check(snapd_apparmor.Run())

	c.Check(s.logBuf.String(), testutil.Contains, "DEBUG: inside container environment")
	c.Check(s.logBuf.String(), testutil.Contains, "Inside container environment without internal policy")
	c.Assert(s.parserCmd.Calls(), HasLen, 0)
}

func (s *integrationSuite) TestRunInContainerWithInternalPolicyLoadsProfiles(c *C) {
	defer mockWSL(1)()
	mylog.Check(snapd_apparmor.Run())

	c.Check(s.logBuf.String(), testutil.Contains, "DEBUG: inside container environment")
	c.Check(s.logBuf.String(), Not(testutil.Contains), "Inside container environment without internal policy")
	c.Assert(s.parserCmd.Calls(), HasLen, 1)
}

func (s *integrationSuite) TestRunNormalLoadsProfiles(c *C) {
	// simulate a normal system (not a container)
	testutil.MockCommand(c, "systemd-detect-virt", "exit 1")

	detectCmd := testutil.MockCommand(c, "systemd-detect-virt", "exit 1")
	defer detectCmd.Restore()
	mylog.Check(snapd_apparmor.Run())

	c.Assert(s.parserCmd.Calls(), HasLen, 1)
	c.Check(s.logBuf.String(), Matches, `(?s).* main.go:[0-9]+: Loading profiles \[.*/var/lib/snapd/apparmor/profiles/foo\].*`)
}
