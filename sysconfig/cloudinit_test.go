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

// this test is for initramfs calls that disable cloud-init for the ephemeral
// writable partition that is used while running during install or recover mode
func (s *sysconfigSuite) TestEphemeralModeInitramfsCloudInitDisables(c *C) {
	writableDefaultsDir := sysconfig.WritableDefaultsDir(boot.InitramfsWritableDir)
	err := sysconfig.DisableCloudInit(writableDefaultsDir)
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitDisablesByDefaultRunMode(c *C) {
	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

// this test is the same as the logic from install mode devicestate, where we
// want to install cloud-init configuration not onto the running, ephemeral
// writable, but rather the host writable partition that will be used upon
// reboot into run mode
func (s *sysconfigSuite) TestInstallModeCloudInitInstallsOntoHostRunMode(c *C) {
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

var gceCloudInitStatusJSON = `{
	"v1": {
	 "datasource": "DataSourceGCE",
	 "init": {
	  "errors": [],
	  "finished": 1591751113.4536479,
	  "start": 1591751112.130069
	 },
	 "stage": null
	}
}
`

var multipassNoCloudCloudInitStatusJSON = `{
 "v1": {
  "datasource": "DataSourceNoCloud [seed=/dev/sr0][dsmode=net]",
  "init": {
   "errors": [],
   "finished": 1591788514.4656117,
   "start": 1591788514.2607572
  },
  "stage": null
 }
}`

var lxdNoCloudCloudInitStatusJSON = `{
 "v1": {
  "datasource": "DataSourceNoCloud [seed=/var/lib/cloud/seed/nocloud-net][dsmode=net]",
  "init": {
   "errors": [],
   "finished": 1591788737.3982718,
   "start": 1591788736.9015596
  },
  "stage": null
 }
}`

var restrictNoCloudYaml = `datasource_list: [NoCloud]
datasource:
  NoCloud:
    fs_label: null`

func (s *sysconfigSuite) TestRestrictCloudInit(c *C) {
	tt := []struct {
		comment                string
		state                  sysconfig.CloudInitState
		sysconfOpts            *sysconfig.CloudInitRestrictOptions
		cloudInitStatusJSON    string
		expError               string
		expRestrictYamlWritten string
		expDatasource          string
		expAction              string
		expDisableFile         bool
	}{
		{
			comment:  "already disabled",
			state:    sysconfig.CloudInitDisabledPermanently,
			expError: "cannot restrict cloud-init: already disabled",
		},
		{
			comment:  "already restricted",
			state:    sysconfig.CloudInitRestrictedBySnapd,
			expError: "cannot restrict cloud-init: already restricted",
		},
		{
			comment:  "errored",
			state:    sysconfig.CloudInitErrored,
			expError: "cannot restrict cloud-init in error or enabled state",
		},
		{
			comment:  "enable (not running)",
			state:    sysconfig.CloudInitEnabled,
			expError: "cannot restrict cloud-init in error or enabled state",
		},
		{
			comment: "errored w/ force disable",
			state:   sysconfig.CloudInitErrored,
			sysconfOpts: &sysconfig.CloudInitRestrictOptions{
				ForceDisable: true,
			},
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment: "enable (not running) w/ force disable",
			state:   sysconfig.CloudInitEnabled,
			sysconfOpts: &sysconfig.CloudInitRestrictOptions{
				ForceDisable: true,
			},
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment:        "untriggered",
			state:          sysconfig.CloudInitUntriggered,
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment:        "unknown status",
			state:          -1,
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment:                "gce done",
			state:                  sysconfig.CloudInitDone,
			cloudInitStatusJSON:    gceCloudInitStatusJSON,
			expDatasource:          "GCE",
			expAction:              "restrict",
			expRestrictYamlWritten: "datasource_list: [GCE]",
		},
		{
			comment:                "nocloud done",
			state:                  sysconfig.CloudInitDone,
			cloudInitStatusJSON:    multipassNoCloudCloudInitStatusJSON,
			expDatasource:          "NoCloud",
			expAction:              "restrict",
			expRestrictYamlWritten: restrictNoCloudYaml,
		},
		// the two cases for lxd and multipass are effectively the same, but as
		// the largest known users of cloud-init w/ UC, we leave them as
		// separate test cases for their different cloud-init status.json
		// content
		{
			comment:                "nocloud multipass done",
			state:                  sysconfig.CloudInitDone,
			cloudInitStatusJSON:    multipassNoCloudCloudInitStatusJSON,
			expDatasource:          "NoCloud",
			expAction:              "restrict",
			expRestrictYamlWritten: restrictNoCloudYaml,
		},
		{
			comment:                "nocloud seed lxd done",
			state:                  sysconfig.CloudInitDone,
			cloudInitStatusJSON:    lxdNoCloudCloudInitStatusJSON,
			expDatasource:          "NoCloud",
			expAction:              "restrict",
			expRestrictYamlWritten: restrictNoCloudYaml,
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		// setup status.json
		old := dirs.GlobalRootDir
		dirs.SetRootDir(c.MkDir())
		defer func() { dirs.SetRootDir(old) }()
		statusJSONFile := filepath.Join(dirs.GlobalRootDir, "/run/cloud-init/status.json")
		if t.cloudInitStatusJSON != "" {
			err := os.MkdirAll(filepath.Dir(statusJSONFile), 0755)
			c.Assert(err, IsNil, comment)
			err = ioutil.WriteFile(statusJSONFile, []byte(t.cloudInitStatusJSON), 0644)
			c.Assert(err, IsNil, comment)
		}

		// if we expect snapd to write a yaml config file for cloud-init, ensure
		// the dir exists before hand
		if t.expRestrictYamlWritten != "" {
			err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/cloud/cloud.cfg.d"), 0755)
			c.Assert(err, IsNil, comment)
		}

		res, err := sysconfig.RestrictCloudInit(t.state, t.sysconfOpts)
		if t.expError == "" {
			c.Assert(err, IsNil, comment)
			c.Assert(res.DataSource, Equals, t.expDatasource, comment)
			c.Assert(res.Action, Equals, t.expAction, comment)
			if t.expRestrictYamlWritten != "" {
				// check the snapd restrict yaml file that should have been written
				c.Assert(
					filepath.Join(dirs.GlobalRootDir, "/etc/cloud/cloud.cfg.d/zzzz_snapd.cfg"),
					testutil.FileEquals,
					t.expRestrictYamlWritten,
					comment,
				)
			}

			// if we expect the disable file to be written then check for it
			// otherwise ensure it was not written accidentally
			var fileCheck Checker
			if t.expDisableFile {
				fileCheck = testutil.FilePresent
			} else {
				fileCheck = testutil.FileAbsent
			}

			c.Assert(
				filepath.Join(dirs.GlobalRootDir, "/etc/cloud/cloud-init.disabled"),
				fileCheck,
				comment,
			)

		} else {
			c.Assert(err, ErrorMatches, t.expError, comment)
		}
	}
}
