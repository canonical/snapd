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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	snapd_apparmor "github.com/snapcore/snapd/cmd/snapd-apparmor"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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

func (s *mainSuite) TestIsContainerWithInternalPolicy(c *C) {
	// since "apparmorfs" is not present within our test root dir setup
	// we expect this to return false
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	appArmorSecurityFSPath := filepath.Join(dirs.GlobalRootDir, "/sys/kernel/security/apparmor/")
	err := os.MkdirAll(appArmorSecurityFSPath, 0755)
	c.Assert(err, IsNil)

	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	// simulate being inside WSL
	testutil.MockCommand(c, "systemd-detect-virt", "echo wsl")
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, true)

	// simulate being inside a container environment
	testutil.MockCommand(c, "systemd-detect-virt", "echo lxc")
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	err = ioutil.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_stacked"), []byte("yes"), 0644)
	c.Assert(err, IsNil)
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	err = ioutil.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_name"), nil, 0644)
	c.Assert(err, IsNil)
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	err = ioutil.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_name"), []byte("foo"), 0644)
	c.Assert(err, IsNil)
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)
	// lxc/lxd name should result in a container with internal policy
	err = ioutil.WriteFile(filepath.Join(appArmorSecurityFSPath, ".ns_name"), []byte("lxc-foo"), 0644)
	c.Assert(err, IsNil)
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, true)
}

func (s *mainSuite) TestLoadAppArmorProfiles(c *C) {
	parserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer parserCmd.Restore()
	err := snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)
	// since no profiles to load the parser should not have been called
	c.Assert(parserCmd.Calls(), HasLen, 0)

	// mock a profile
	err = os.MkdirAll(dirs.SnapAppArmorDir, 0755)
	c.Assert(err, IsNil)

	profile := filepath.Join(dirs.SnapAppArmorDir, "foo")
	err = ioutil.WriteFile(profile, nil, 0644)
	c.Assert(err, IsNil)

	// ensure SNAPD_DEBUG is set in the environment so then --quiet
	// will *not* be included in the apparmor_parser arguments (since
	// when these test are run in via CI SNAPD_DEBUG is set)
	os.Setenv("SNAPD_DEBUG", "1")
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)

	// check arguments to the parser are as expected
	c.Assert(parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache",
			"-O", "no-expr-simplify",
			fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir),
			profile}})

	// test error case
	testutil.MockCommand(c, "apparmor_parser", "echo mocked parser failed > /dev/stderr; exit 1")
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Check(err.Error(), Equals, fmt.Sprintf("cannot load apparmor profiles: exit status 1\napparmor_parser output:\nmocked parser failed\n"))

	// rename so file is ignored
	err = os.Rename(profile, profile+"~")
	c.Assert(err, IsNil)
	// forget previous calls so we can check below that as a result of
	// having no profiles again that no invocation of the parser occurs
	parserCmd.ForgetCalls()
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)
	c.Assert(parserCmd.Calls(), HasLen, 0)
}

func (s *mainSuite) TestIsContainer(c *C) {
	detectCmd := testutil.MockCommand(c, "systemd-detect-virt", "exit 1")
	defer detectCmd.Restore()
	c.Check(snapd_apparmor.IsContainer(), Equals, false)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"}})

	detectCmd = testutil.MockCommand(c, "systemd-detect-virt", "")
	c.Check(snapd_apparmor.IsContainer(), Equals, true)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"}})

	// test error cases too
	detectCmd = testutil.MockCommand(c, "systemd-detect-virt", "echo failed > /dev/stderr; exit 1")
	c.Check(snapd_apparmor.IsContainer(), Equals, false)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"}})
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
		err := snapd_apparmor.ValidateArgs(tc.args)
		if err != nil {
			c.Check(err.Error(), Equals, tc.errMsg)
		} else {
			c.Check(tc.errMsg, Equals, "")
		}
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
	err := os.MkdirAll(dirs.SnapAppArmorDir, 0755)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapAppArmorDir, "foo")
	err = ioutil.WriteFile(profile, nil, 0644)
	c.Assert(err, IsNil)

	os.Args = []string{"snapd-apparmor", "start"}
}

func (s *integrationSuite) TestRunInContainerSkipsLoading(c *C) {
	testutil.MockCommand(c, "systemd-detect-virt", "exit 0")

	err := snapd_apparmor.Run()
	c.Assert(err, IsNil)
	c.Check(s.logBuf.String(), testutil.Contains, "DEBUG: inside container environment")
	c.Check(s.logBuf.String(), testutil.Contains, "Inside container environment without internal policy")
	c.Assert(s.parserCmd.Calls(), HasLen, 0)
}

func (s *integrationSuite) TestRunInContainerWithInternalPolicyLoadsProfiles(c *C) {
	testutil.MockCommand(c, "systemd-detect-virt", "echo wsl")

	err := snapd_apparmor.Run()
	c.Assert(err, IsNil)
	c.Check(s.logBuf.String(), testutil.Contains, "DEBUG: inside container environment")
	c.Check(s.logBuf.String(), Not(testutil.Contains), "Inside container environment without internal policy")
	c.Assert(s.parserCmd.Calls(), HasLen, 1)
}

func (s *integrationSuite) TestRunNormalLoadsProfiles(c *C) {
	// simulate a normal system (not a container)
	testutil.MockCommand(c, "systemd-detect-virt", "exit 1")

	detectCmd := testutil.MockCommand(c, "systemd-detect-virt", "exit 1")
	defer detectCmd.Restore()

	err := snapd_apparmor.Run()
	c.Assert(err, IsNil)
	c.Assert(s.parserCmd.Calls(), HasLen, 1)
}
