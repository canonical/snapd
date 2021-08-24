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

func (s *sysconfigSuite) makeCloudCfgSrcDirFiles(c *C) string {
	cloudCfgSrcDir := c.MkDir()
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err := ioutil.WriteFile(filepath.Join(cloudCfgSrcDir, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}
	return cloudCfgSrcDir
}

func (s *sysconfigSuite) makeGadgetCloudConfFile(c *C) string {
	gadgetDir := c.MkDir()
	gadgetCloudConf := filepath.Join(gadgetDir, "cloud.conf")
	err := ioutil.WriteFile(gadgetCloudConf, []byte("#cloud-config gadget cloud config"), 0644)
	c.Assert(err, IsNil)

	return gadgetDir
}

func (s *sysconfigSuite) TestHasGadgetCloudConf(c *C) {
	// no cloud.conf is false
	c.Assert(sysconfig.HasGadgetCloudConf("non-existent-dir-place"), Equals, false)

	// the dir is not enough
	gadgetDir := c.MkDir()
	c.Assert(sysconfig.HasGadgetCloudConf(gadgetDir), Equals, false)

	// creating one now is true
	gadgetCloudConf := filepath.Join(gadgetDir, "cloud.conf")
	err := ioutil.WriteFile(gadgetCloudConf, []byte("gadget cloud config"), 0644)
	c.Assert(err, IsNil)

	c.Assert(sysconfig.HasGadgetCloudConf(gadgetDir), Equals, true)
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
	err := sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitDisallowedIgnoresOtherOptions(c *C) {
	cloudCfgSrcDir := s.makeCloudCfgSrcDirFiles(c)
	gadgetDir := s.makeGadgetCloudConfFile(c)

	err := sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		AllowCloudInit:  false,
		CloudInitSrcDir: cloudCfgSrcDir,
		GadgetDir:       gadgetDir,
		TargetRootDir:   boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)

	// did not copy ubuntu-seed src files
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileAbsent)
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileAbsent)

	// also did not copy gadget cloud.conf
	c.Check(filepath.Join(ubuntuDataCloudCfg, "80_device_gadget.cfg"), testutil.FileAbsent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitAllowedGradeSignedDoesNotDisable(c *C) {
	err := sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		AllowCloudInit: true,
		TargetRootDir:  boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FileAbsent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitAllowedGradeSignedDoesNotInstallUbuntuSeedConfig(c *C) {
	cloudCfgSrcDir := s.makeCloudCfgSrcDirFiles(c)

	err := sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		AllowCloudInit:  true,
		TargetRootDir:   boot.InstallHostWritableDir,
		CloudInitSrcDir: cloudCfgSrcDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileAbsent)
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileAbsent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitAllowedGradeDangerousDoesNotDisable(c *C) {
	err := sysconfig.ConfigureTargetSystem(fake20Model("dangerous"), &sysconfig.Options{
		AllowCloudInit: true,
		TargetRootDir:  boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FileAbsent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitDisallowedGradeSecuredDoesDisable(c *C) {
	err := sysconfig.ConfigureTargetSystem(fake20Model("secured"), &sysconfig.Options{
		AllowCloudInit: false,
		TargetRootDir:  boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitAllowedGradeSecuredIgnoresSrcButDoesNotDisable(c *C) {
	cloudCfgSrcDir := s.makeCloudCfgSrcDirFiles(c)

	err := sysconfig.ConfigureTargetSystem(fake20Model("secured"), &sysconfig.Options{
		AllowCloudInit:  true,
		CloudInitSrcDir: cloudCfgSrcDir,
		TargetRootDir:   boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	// the disable file is not present
	ubuntuDataCloudDisabled := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FileAbsent)

	// but we did not copy the config files from ubuntu-seed, even though they
	// are there and cloud-init is not disabled
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileAbsent)
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileAbsent)
}

// this test is the same as the logic from install mode devicestate, where we
// want to install cloud-init configuration not onto the running, ephemeral
// writable, but rather the host writable partition that will be used upon
// reboot into run mode
func (s *sysconfigSuite) TestInstallModeCloudInitInstallsOntoHostRunMode(c *C) {
	cloudCfgSrcDir := s.makeCloudCfgSrcDirFiles(c)

	err := sysconfig.ConfigureTargetSystem(fake20Model("dangerous"), &sysconfig.Options{
		AllowCloudInit:  true,
		CloudInitSrcDir: cloudCfgSrcDir,
		TargetRootDir:   boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	// and did copy the cloud-init files
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "90_foo.cfg"), testutil.FileEquals, "foo.cfg config")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "90_bar.cfg"), testutil.FileEquals, "bar.cfg config")
}

func (s *sysconfigSuite) TestInstallModeCloudInitInstallsOntoHostRunModeWithGadgetCloudConf(c *C) {
	gadgetDir := s.makeGadgetCloudConfFile(c)
	err := sysconfig.ConfigureTargetSystem(fake20Model("secured"), &sysconfig.Options{
		AllowCloudInit: true,
		GadgetDir:      gadgetDir,
		TargetRootDir:  boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	// and did copy the gadget cloud-init file
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "80_device_gadget.cfg"), testutil.FileEquals, "#cloud-config gadget cloud config")
}

func (s *sysconfigSuite) TestInstallModeCloudInitInstallsOntoHostRunModeWithGadgetCloudConfAlsoInstallsUbuntuSeedConfig(c *C) {
	cloudCfgSrcDir := s.makeCloudCfgSrcDirFiles(c)
	gadgetDir := s.makeGadgetCloudConfFile(c)

	err := sysconfig.ConfigureTargetSystem(fake20Model("dangerous"), &sysconfig.Options{
		AllowCloudInit:  true,
		CloudInitSrcDir: cloudCfgSrcDir,
		GadgetDir:       gadgetDir,
		TargetRootDir:   boot.InstallHostWritableDir,
	})
	c.Assert(err, IsNil)

	// we did copy the gadget cloud-init file
	ubuntuDataCloudCfg := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "80_device_gadget.cfg"), testutil.FileEquals, "#cloud-config gadget cloud config")

	// and we also copied the ubuntu-seed files with a new prefix such that they
	// take precedence over the gadget file by being ordered lexically after the
	// gadget file
	c.Check(filepath.Join(ubuntuDataCloudCfg, "90_foo.cfg"), testutil.FileEquals, "foo.cfg config")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "90_bar.cfg"), testutil.FileEquals, "bar.cfg config")
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
		exitCode        int
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
			comment:         "errored w/ exit code 0",
			cloudInitOutput: "status: error",
			exp:             sysconfig.CloudInitErrored,
			exitCode:        0,
		},
		{
			comment:         "errored w/ exit code 1",
			cloudInitOutput: "status: error",
			exp:             sysconfig.CloudInitErrored,
			exitCode:        1,
		},
		{
			comment:         "broken cloud-init output w/ exit code 0",
			cloudInitOutput: "broken cloud-init output",
			expError:        "invalid cloud-init output: broken cloud-init output",
		},
		{
			comment:         "broken cloud-init output w/ exit code 1",
			cloudInitOutput: "broken cloud-init output",
			exitCode:        1,
			expError:        "broken cloud-init output",
		},
	}

	for _, t := range tt {
		old := dirs.GlobalRootDir
		dirs.SetRootDir(c.MkDir())
		defer func() { dirs.SetRootDir(old) }()
		cmd := testutil.MockCommand(c, "cloud-init", fmt.Sprintf(`
if [ "$1" = "status" ]; then
	echo '%s'
	exit %d
else 
	echo "unexpected args, $"
	exit 1
fi
		`, t.cloudInitOutput, t.exitCode))

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

func (s *sysconfigSuite) TestCloudInitNotFoundStatus(c *C) {
	emptyDir := c.MkDir()
	oldPath := os.Getenv("PATH")
	defer func() {
		c.Assert(os.Setenv("PATH", oldPath), IsNil)
	}()
	os.Setenv("PATH", emptyDir)

	status, err := sysconfig.CloudInitStatus()
	c.Assert(err, IsNil)
	c.Check(status, Equals, sysconfig.CloudInitNotFound)
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

var localNoneCloudInitStatusJSON = `{
 "v1": {
  "datasource": "DataSourceNone",
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
    fs_label: null
manual_cache_clean: true
`

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
			comment:             "gce done",
			state:               sysconfig.CloudInitDone,
			cloudInitStatusJSON: gceCloudInitStatusJSON,
			expDatasource:       "GCE",
			expAction:           "restrict",
			expRestrictYamlWritten: `datasource_list: [GCE]
`,
		},
		{
			comment:                "nocloud done",
			state:                  sysconfig.CloudInitDone,
			cloudInitStatusJSON:    multipassNoCloudCloudInitStatusJSON,
			expDatasource:          "NoCloud",
			expAction:              "restrict",
			expRestrictYamlWritten: restrictNoCloudYaml,
		},
		{
			comment:             "nocloud uc20 done",
			state:               sysconfig.CloudInitDone,
			cloudInitStatusJSON: multipassNoCloudCloudInitStatusJSON,
			sysconfOpts: &sysconfig.CloudInitRestrictOptions{
				DisableAfterLocalDatasourcesRun: true,
			},
			expDatasource:  "NoCloud",
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment:             "none uc20 done",
			state:               sysconfig.CloudInitDone,
			cloudInitStatusJSON: localNoneCloudInitStatusJSON,
			sysconfOpts: &sysconfig.CloudInitRestrictOptions{
				DisableAfterLocalDatasourcesRun: true,
			},
			expDatasource:  "None",
			expAction:      "disable",
			expDisableFile: true,
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
		{
			comment:             "nocloud uc20 multipass done",
			state:               sysconfig.CloudInitDone,
			cloudInitStatusJSON: multipassNoCloudCloudInitStatusJSON,
			sysconfOpts: &sysconfig.CloudInitRestrictOptions{
				DisableAfterLocalDatasourcesRun: true,
			},
			expDatasource:  "NoCloud",
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment:             "nocloud uc20 seed lxd done",
			state:               sysconfig.CloudInitDone,
			cloudInitStatusJSON: lxdNoCloudCloudInitStatusJSON,
			sysconfOpts: &sysconfig.CloudInitRestrictOptions{
				DisableAfterLocalDatasourcesRun: true,
			},
			expDatasource:  "NoCloud",
			expAction:      "disable",
			expDisableFile: true,
		},
		{
			comment:        "no cloud-init in $PATH",
			state:          sysconfig.CloudInitNotFound,
			expAction:      "disable",
			expDisableFile: true,
		},
	}

	for _, t := range tt {
		comment := Commentf("%s", t.comment)
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

const maasGadgetCloudInitImplictYAML = `
datasource:
  MAAS:
    foo: bar
`

const maasGadgetCloudInitImplictLowerCaseYAML = `
datasource:
  maas:
    foo: bar
`

const explicitlyNoDatasourceYAML = `datasource_list: []`

const explicitlyNoDatasourceButAlsoImplicitlyAnotherYAML = `
datasource_list: []
reporting:
  NoCloud:
    foo: bar
`

const explicitlyMultipleMixedCaseMentioned = `
reporting:
  NoCloud:
    foo: bar
  maas:
    foo: bar
datasource:
  MAAS:
    foo: bar
  NOCLOUD:
    foo: bar
`

func (s *sysconfigSuite) TestCloudDatasourcesInUse(c *C) {
	tt := []struct {
		configFileContent string
		expError          string
		expRes            *sysconfig.CloudDatasourcesInUseResult
		comment           string
	}{
		{
			configFileContent: `datasource_list: [MAAS]`,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"MAAS"},
			},
			comment: "explicitly allowed via datasource_list in upper case",
		},
		{
			configFileContent: `datasource_list: [maas]`,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"MAAS"},
			},
			comment: "explicitly allowed via datasource_list in lower case",
		},
		{
			configFileContent: `datasource_list: [mAaS]`,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"MAAS"},
			},
			comment: "explicitly allowed via datasource_list in random case",
		},
		{
			configFileContent: `datasource_list: [maas, maas]`,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"MAAS"},
			},
			comment: "duplicated datasource in datasource_list",
		},
		{
			configFileContent: `datasource_list: [maas, MAAS]`,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"MAAS"},
			},
			comment: "duplicated datasource in datasource_list with different cases",
		},
		{
			configFileContent: `datasource_list: [maas, GCE]`,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"GCE", "MAAS"},
				Mentioned:         []string{"GCE", "MAAS"},
			},
			comment: "multiple datasources in datasource list",
		},
		{
			configFileContent: maasGadgetCloudInitImplictYAML,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"MAAS"},
			},
			comment: "implicitly mentioned datasource",
		},
		{
			configFileContent: maasGadgetCloudInitImplictLowerCaseYAML,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"MAAS"},
			},
			comment: "implicitly mentioned datasource in lower case",
		},
		{
			configFileContent: explicitlyNoDatasourceYAML,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyNoneAllowed: true,
			},
			comment: "no datasources allowed at all",
		},
		{
			configFileContent: explicitlyNoDatasourceButAlsoImplicitlyAnotherYAML,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyNoneAllowed: true,
				Mentioned:             []string{"NOCLOUD"},
			},
			comment: "explicitly no datasources allowed, but still some mentioned",
		},
		{
			configFileContent: explicitlyMultipleMixedCaseMentioned,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"MAAS", "NOCLOUD"},
			},
			comment: "multiple of same datasources mentioned in different cases",
		},
		{
			configFileContent: "i'm not yaml",
			expError:          "yaml: unmarshal errors.*\n.*cannot unmarshal.*",
			comment:           "invalid yaml",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		configFile := filepath.Join(c.MkDir(), "cloud.conf")
		err := ioutil.WriteFile(configFile, []byte(t.configFileContent), 0644)
		c.Assert(err, IsNil, comment)
		res, err := sysconfig.CloudDatasourcesInUse(configFile)
		if t.expError != "" {
			c.Assert(err, ErrorMatches, t.expError, comment)
			continue
		}

		c.Assert(res, DeepEquals, t.expRes, comment)
	}
}

const maasCfg1 = `#cloud-config
reporting:
  maas:
    type: webhook
    endpoint: http://172-16-99-0--24.maas-internal:5248/MAAS/metadata/status/foo
    consumer_key: foothefoo
    token_key: foothefoothesecond
    token_secret: foothesecretfoo
`

const maasCfg2 = `datasource_list: [ MAAS ]
`

const maasCfg3 = `#cloud-config
snappy:
  email: foo@foothewebsite.com
`

const maasCfg4 = `#cloud-config
network:
  config:
  - id: enp3s0
    mac_address: 52:54:00:b4:9e:25
    mtu: 1500
    name: enp3s0
    subnets:
    - address: 172.16.99.7/24
      dns_nameservers:
      - 172.16.99.1
      dns_search:
      - maas
      type: static
    type: physical
  - address: 172.16.99.1
    search:
    - maas
    type: nameserver
  version: 1
`

const maasCfg5 = `#cloud-config
datasource:
  MAAS:
    consumer_key: foothefoo
    metadata_url: http://172-16-99-0--24.maas-internal:5248/MAAS/metadata/
    token_key: foothefoothesecond
    token_secret: foothesecretfoo
`

func (s *sysconfigSuite) TestFilterCloudCfgFile(c *C) {
	tt := []struct {
		comment string
		inStr   string
		outStr  string
		err     string
	}{
		{
			comment: "maas reporting cloud-init config",
			inStr:   maasCfg1,
			outStr:  maasCfg1,
		},
		{
			comment: "maas datasource list cloud-init config",
			inStr:   maasCfg2,
			outStr: `#cloud-config
datasource_list:
- MAAS
`,
		},
		{
			comment: "maas snappy user cloud-init config",
			inStr:   maasCfg3,
			// we don't support using the snappy key
			outStr: "",
		},
		{
			comment: "maas networking cloud-init config",
			inStr:   maasCfg4,
			outStr:  maasCfg4,
		},
		{
			comment: "maas datasource cloud-init config",
			inStr:   maasCfg5,
			outStr:  maasCfg5,
		},
		{
			comment: "unsupported datasource in datasource section cloud-init config",
			inStr: `#cloud-config
datasource:
  NoCloud:
    consumer_key: fooooooo
`,
			outStr: "",
		},
		{
			comment: "unsupported datasource in reporting section cloud-init config",
			inStr: `#cloud-config
reporting:
  NoCloud:
    consumer_key: fooooooo
`,
			outStr: "",
		},
		{
			comment: "unsupported datasource in datasource_list with supported one",
			inStr: `#cloud-config
datasource_list: [MAAS, NoCloud]
`,
			outStr: `#cloud-config
datasource_list:
- MAAS
`,
		},
		{
			comment: "unsupported datasources in multiple keys with supported ones",
			inStr: `#cloud-config
datasource:
  MAAS:
    consumer_key: fooooooo
  NoCloud:
    consumer_key: fooooooo

reporting:
  MAAS:
    type: webhook
  NoCloud:
    type: webhook

datasource_list: [MAAS, NoCloud]
`,
			outStr: `#cloud-config
datasource:
  MAAS:
    consumer_key: fooooooo
datasource_list:
- MAAS
reporting:
  MAAS:
    type: webhook
`,
		},
		{
			comment: "unrelated keys",
			inStr: `#cloud-config
datasource:
  MAAS:
    consumer_key: fooooooo
    foo: bar

reporting:
  MAAS:
    type: webhook
    new_foo: new_bar

extra_foo: extra_bar
`,
			outStr: `#cloud-config
datasource:
  MAAS:
    consumer_key: fooooooo
reporting:
  MAAS:
    type: webhook
`,
		},
	}

	dir := c.MkDir()
	for i, t := range tt {
		comment := Commentf(t.comment)
		inFile := filepath.Join(dir, fmt.Sprintf("%d.cfg", i))
		err := ioutil.WriteFile(inFile, []byte(t.inStr), 0755)
		c.Assert(err, IsNil, comment)

		out, err := sysconfig.FilterCloudCfgFile(inFile, []string{"MAAS"})
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			continue
		}
		c.Assert(err, IsNil, comment)

		// no expected output means that everything was filtered out
		if t.outStr == "" {
			c.Assert(out, Equals, "", comment)
			continue
		}

		// otherwise we have expected output in the file
		b, err := ioutil.ReadFile(out)
		c.Assert(err, IsNil, comment)
		c.Assert(string(b), Equals, t.outStr, comment)
	}
}
