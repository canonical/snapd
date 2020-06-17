// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package sysconfig_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type sysconfigSuite struct {
	testutil.BaseTest

	tmpdir string
}

var _ = Suite(&sysconfigSuite{})

func (s *sysconfigSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *sysconfigSuite) TestCloudInitDisablesByDefault(c *C) {
	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled/")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestCloudInitInstalls(c *C) {
	cloudCfgSrcDir := c.MkDir()
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err := ioutil.WriteFile(filepath.Join(cloudCfgSrcDir, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		CloudInitSrcDir: cloudCfgSrcDir,
		TargetRootDir:   boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	// and did copy the cloud-init files
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileEquals, "foo.cfg config")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileEquals, "bar.cfg config")
}

func (s *sysconfigSuite) TestCloudInitStatusUnhappy(c *C) {
	cmd := testutil.MockCommand(c, "cloud-init", `
echo cloud-init borken
exit 1
`)

	status, err := sysconfig.CloudInitStatus()
	c.Assert(err, ErrorMatches, "cloud-init borken")
	c.Assert(status, Equals, sysconfig.CloudInitErrored)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"cloud-init", "status"},
	})
}

func (s *sysconfigSuite) TestCloudInitStatus(c *C) {
	tt := []struct {
		comment         string
		cloudInitOutput string
		exp             sysconfig.CloudInitState
		restrictedFile  bool
		disabledFile    bool
		expError        string
	}{
		{
			comment:         "done",
			cloudInitOutput: "status: done",
			exp:             sysconfig.CloudInitDone,
		},
		{
			comment:         "running",
			cloudInitOutput: "status: running",
			exp:             sysconfig.CloudInitEnabled,
		},
		{
			comment:         "not run",
			cloudInitOutput: "status: not run",
			exp:             sysconfig.CloudInitEnabled,
		},
		{
			comment:         "new unrecognized state",
			cloudInitOutput: "status: newfangledstatus",
			exp:             sysconfig.CloudInitEnabled,
		},
		{
			comment:        "restricted by snapd",
			restrictedFile: true,
			exp:            sysconfig.CloudInitRestrictedBySnapd,
		},
		{
			comment:         "disabled temporarily",
			cloudInitOutput: "status: disabled",
			exp:             sysconfig.CloudInitUntriggered,
		},
		{
			comment:      "disabled permanently via file",
			disabledFile: true,
			exp:          sysconfig.CloudInitDisabledPermanently,
		},
		{
			comment:         "errored",
			cloudInitOutput: "status: error",
			exp:             sysconfig.CloudInitErrored,
		},
		{
			comment:         "broken cloud-init output",
			cloudInitOutput: "broken cloud-init output",
			expError:        "invalid cloud-init output: broken cloud-init output",
		},
	}

	for _, t := range tt {
		old := dirs.GlobalRootDir
		dirs.SetRootDir(c.MkDir())
		defer func() { dirs.SetRootDir(old) }()
		cmd := testutil.MockCommand(c, "cloud-init", fmt.Sprintf(`
if [ "$1" = "status" ]; then
	echo '%s'
else 
	echo "unexpected args, $"
	exit 1
fi
		`, t.cloudInitOutput))

		if t.disabledFile {
			cloudDir := filepath.Join(dirs.GlobalRootDir, "etc/cloud")
			err := os.MkdirAll(cloudDir, 0755)
			c.Assert(err, IsNil)
			err = ioutil.WriteFile(filepath.Join(cloudDir, "cloud-init.disabled"), nil, 0644)
			c.Assert(err, IsNil)
		}

		if t.restrictedFile {
			cloudDir := filepath.Join(dirs.GlobalRootDir, "etc/cloud/cloud.cfg.d")
			err := os.MkdirAll(cloudDir, 0755)
			c.Assert(err, IsNil)
			err = ioutil.WriteFile(filepath.Join(cloudDir, "zzzz_snapd.cfg"), nil, 0644)
			c.Assert(err, IsNil)
		}

		status, err := sysconfig.CloudInitStatus()
		if t.expError != "" {
			c.Assert(err, ErrorMatches, t.expError, Commentf(t.comment))
		} else {
			c.Assert(err, IsNil)
			c.Assert(status, Equals, t.exp, Commentf(t.comment))
		}

		// if the restricted file was there we don't call cloud-init status
		var expCalls [][]string
		if !t.restrictedFile && !t.disabledFile {
			expCalls = [][]string{
				{"cloud-init", "status"},
			}
		}

		c.Assert(cmd.Calls(), DeepEquals, expCalls, Commentf(t.comment))
		cmd.Restore()
	}
}
