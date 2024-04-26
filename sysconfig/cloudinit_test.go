// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020, 2024 Canonical Ltd
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
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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

	oldTmpdir := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", s.tmpdir)
	s.AddCleanup(func() { os.Unsetenv(oldTmpdir) })
}

func (s *sysconfigSuite) makeCloudCfgSrcDirFiles(c *C, cfgs ...string) (string, []string) {
	cloudCfgSrcDir := c.MkDir()
	names := make([]string, 0, len(cfgs))
	for i, mockCfg := range cfgs {
		configFileName := fmt.Sprintf("seed-config-%d.cfg", i)
		err := os.WriteFile(filepath.Join(cloudCfgSrcDir, configFileName), []byte(mockCfg), 0644)
		c.Assert(err, IsNil)
		names = append(names, configFileName)
	}
	return cloudCfgSrcDir, names
}

func (s *sysconfigSuite) makeGadgetCloudConfFile(c *C, content string) string {
	gadgetDir := c.MkDir()
	gadgetCloudConf := filepath.Join(gadgetDir, "cloud.conf")
	if content == "" {
		content = "#cloud-config some gadget cloud config"
	}
	err := os.WriteFile(gadgetCloudConf, []byte(content), 0644)
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
	err := os.WriteFile(gadgetCloudConf, []byte("gadget cloud config"), 0644)
	c.Assert(err, IsNil)

	c.Assert(sysconfig.HasGadgetCloudConf(gadgetDir), Equals, true)
}

// this test is for initramfs calls that disable cloud-init for the ephemeral
// writable partition that is used while running during install or recover mode
func (s *sysconfigSuite) TestEphemeralModeInitramfsCloudInitDisables(c *C) {
	writableDefaultsDir := sysconfig.WritableDefaultsDir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	err := sysconfig.DisableCloudInit(writableDefaultsDir)
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitDisablesByDefaultRunMode(c *C) {
	err := sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		TargetRootDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitDisallowedIgnoresOtherOptions(c *C) {
	cloudCfgSrcDir, _ := s.makeCloudCfgSrcDirFiles(c)
	gadgetDir := s.makeGadgetCloudConfFile(c, "")

	err := sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		AllowCloudInit:  false,
		CloudInitSrcDir: cloudCfgSrcDir,
		GadgetDir:       gadgetDir,
		TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)

	// did not copy ubuntu-seed src files
	ubuntuDataCloudCfg := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileAbsent)
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileAbsent)

	// also did not copy gadget cloud.conf
	c.Check(filepath.Join(ubuntuDataCloudCfg, "80_device_gadget.cfg"), testutil.FileAbsent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitAllowedPermutations(c *C) {

	// common inputs in the test cases below
	defaultMAASCfgs := []string{
		maasCfg1, // MAAS specific config
		maasCfg2, // MAAS specific config - this sets the datasource_list
		maasCfg3, // generic config that's filtered out
		maasCfg4, // generic config that passes filtering
		maasCfg5, // MAAS specific config
	}
	defaultMAASHappyInstalled := []bool{
		true,
		true,
		false,
		true,
		true,
	}
	defaultMAASNoneInstalled := []bool{false, false, false, false, false}

	explicitNonSupportedDatasource := `datasource_list: [GCE]`
	explicitSupportedDatasource := maasCfg2
	explicitNoDatasourceAllowed := `datasource_list: []`
	explicitSupportedAndNonSupportedDatasources := `datasource_list: [GCE, MAAS]`

	implictMentionNonSupportedDatasource := `#cloud-config
datasource:
  gce:
    metadata_url: foo
`
	implicitMentionSupportedDatasource := maasCfg1

	restrictDatasourceMAASFile := `datasource_list: [MAAS]
`
	// restrictDatasourceNoneFile := `datasource_list: []
	// `

	tt := []struct {
		grade     string
		gadgetCfg string
		// use lists here to install the files in the order that the file
		// content appears
		seedCfgs []string
		// whether each file in seedCfgs gets installed
		resultCfgCopied           []bool
		expDsRestrictFileContents string
		comment                   string
	}{
		{
			comment: "no config anywhere, but cloud-init not disabled",
		},
		{
			grade:   "dangerous",
			comment: "grade dangerous, no config anywhere, but cloud-init not disabled",
		},
		{
			comment:                   "no gadget config but MAAS allowed",
			seedCfgs:                  defaultMAASCfgs,
			resultCfgCopied:           defaultMAASHappyInstalled,
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment: "grade signed NoCloud not allowed",
			seedCfgs: []string{
				`#cloud-config
datasource:
  NoCloud:
    something-no-cloudy: foo
`,
			},
			resultCfgCopied: []bool{false},
		},

		{
			comment:                   "MAAS in seed and gadget explicit gets installed",
			seedCfgs:                  defaultMAASCfgs,
			resultCfgCopied:           defaultMAASHappyInstalled,
			gadgetCfg:                 explicitSupportedDatasource,
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment:                   "MAAS in seed and gadget implicit gets installed",
			seedCfgs:                  defaultMAASCfgs,
			resultCfgCopied:           defaultMAASHappyInstalled,
			gadgetCfg:                 implicitMentionSupportedDatasource,
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment:         "MAAS in seed not installed due to implicit unsupported datasource in gadget",
			seedCfgs:        defaultMAASCfgs,
			resultCfgCopied: defaultMAASNoneInstalled,
			gadgetCfg:       implictMentionNonSupportedDatasource,
		},
		{
			comment:         "MAAS in seed not installed due to gadget disallowing any datasource",
			seedCfgs:        defaultMAASCfgs,
			resultCfgCopied: defaultMAASNoneInstalled,
			gadgetCfg:       explicitNoDatasourceAllowed,
		},
		{
			comment:         "MAAS in seed not installed due to gadget explicitly allowing other datasource",
			seedCfgs:        defaultMAASCfgs,
			resultCfgCopied: defaultMAASNoneInstalled,
			gadgetCfg:       explicitNonSupportedDatasource,
		},
		{
			comment: "MAAS in seed not installed due to seed config disallowing itself with gadget",
			seedCfgs: []string{
				maasCfg1,                    // MAAS specific config
				maasCfg2,                    // MAAS specific config - this sets the datasource_list
				maasCfg3,                    // generic config that's filtered out
				maasCfg4,                    // generic config that passes filtering
				maasCfg5,                    // MAAS specific config
				explicitNoDatasourceAllowed, // extra that disables all datasources
			},
			resultCfgCopied: []bool{false, false, false, false, false, false},
			gadgetCfg:       explicitNonSupportedDatasource,
		},
		{
			comment: "MAAS in seed not installed due to seed config disallowing itself without gadget",
			seedCfgs: []string{
				maasCfg1,                    // MAAS specific config
				maasCfg2,                    // MAAS specific config - this sets the datasource_list
				maasCfg3,                    // generic config that's filtered out
				maasCfg4,                    // generic config that passes filtering
				maasCfg5,                    // MAAS specific config
				explicitNoDatasourceAllowed, // extra that disables all datasources
			},
			resultCfgCopied: []bool{false, false, false, false, false, false},
		},
		{
			comment: "implictly mentioned datasource in seed not installed because it's unsupported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg2,                             // MAAS specific config - this sets the datasource_list
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},
			resultCfgCopied: []bool{
				true,
				true,
				true,
				true,
				false, // the implicit GCE one
			},
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment: "implictly mentioned datasource in seed not installed because it's unsupported + gadget with explicit supported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg2,                             // MAAS specific config - this sets the datasource_list
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},
			resultCfgCopied: []bool{
				true,
				true,
				true,
				true,
				false, // the implicit GCE one
			},
			gadgetCfg:                 explicitSupportedDatasource,
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment: "implicit mentioned datasource in seed not installed because it's unsupported + gadget with explicit supported and non-supported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg2,                             // MAAS specific config - this sets the datasource_list
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},

			resultCfgCopied: []bool{
				true,
				true,
				true,
				true,
				false, // the implicit GCE one
			},
			gadgetCfg:                 explicitSupportedAndNonSupportedDatasources,
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment: "implicit mentioned datasource and supported datasource in seed all not installed because no supported datasource in intersection with gadget with implicit non-supported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg2,                             // MAAS specific config - this sets the datasource_list
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},

			resultCfgCopied: []bool{
				false,
				false,
				false,
				false,
				false, // the implicit GCE one
			},
			gadgetCfg: implictMentionNonSupportedDatasource,
		},
		{
			comment: "implicit mentioned datasource and supported datasource in seed all not installed because no supported datasource in intersection with gadget with explicit non-supported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg2,                             // MAAS specific config - this sets the datasource_list
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},

			resultCfgCopied: []bool{
				false,
				false,
				false,
				false,
				false, // the implicit GCE one
			},
			gadgetCfg: explicitNonSupportedDatasource,
		},
		{
			comment: "implicit mentioned datasource and supported datasource in seed all not installed because no supported datasource in intersection with gadget with implicit non-supported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},

			resultCfgCopied: []bool{
				false,
				false,
				false, // the implicit GCE one
				false,
			},
			gadgetCfg: implictMentionNonSupportedDatasource,
		},
		{
			comment: "implicit mentioned datasource in seed not installed and supported datasource in seed installed due to intersection with gadget with implicit supported",
			seedCfgs: []string{
				maasCfg1,                             // MAAS specific config
				maasCfg4,                             // generic config that passes filtering
				maasCfg5,                             // MAAS specific config
				implictMentionNonSupportedDatasource, // extra that implicitly mentions GCE
			},
			resultCfgCopied: []bool{
				true,
				true,
				true,
				false, // the implicit GCE one
			},
			gadgetCfg:                 explicitSupportedAndNonSupportedDatasources,
			expDsRestrictFileContents: restrictDatasourceMAASFile,
		},
		{
			comment:         "entirely filtered out seed config not installed",
			seedCfgs:        []string{maasCfg3},
			resultCfgCopied: []bool{false},
		},
		{
			comment:         "entirely filtered out seed config not installed + gadget with explicit supported datasource",
			seedCfgs:        []string{maasCfg3},
			resultCfgCopied: []bool{false},
			gadgetCfg:       explicitSupportedDatasource,
		},
		{
			comment: "implicitly mentioned supported datasource in gadget + explicit no datasource in seed",
			seedCfgs: []string{
				explicitNoDatasourceAllowed,
			},
			resultCfgCopied: []bool{false},
			gadgetCfg:       implicitMentionSupportedDatasource,
		},
		{
			comment: "MAAS and GCE in seed gets installed in dangerous",
			grade:   "dangerous",
			seedCfgs: []string{
				maasCfg1,
				maasCfg2,
				maasCfg3,
				maasCfg4,
				maasCfg5,
				implictMentionNonSupportedDatasource,
			},
			resultCfgCopied: []bool{true, true, true, true, true, true},
		},
		{
			comment: "MAAS and GCE in seed gets installed in dangerous with gadget MAAS",
			grade:   "dangerous",
			seedCfgs: []string{
				maasCfg1,
				maasCfg2,
				maasCfg3,
				maasCfg4,
				maasCfg5,
				implictMentionNonSupportedDatasource,
			},
			gadgetCfg:       implicitMentionSupportedDatasource,
			resultCfgCopied: []bool{true, true, true, true, true, true},
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		var seedSrcNames []string
		var cloudCfgSrcDir string
		c.Assert(t.seedCfgs, HasLen, len(t.resultCfgCopied), comment)
		if len(t.seedCfgs) != 0 {
			cloudCfgSrcDir, seedSrcNames = s.makeCloudCfgSrcDirFiles(c, t.seedCfgs...)
		}

		var gadgetDir string
		if t.gadgetCfg != "" {
			gadgetDir = s.makeGadgetCloudConfFile(c, t.gadgetCfg)
		}

		if t.grade == "" {
			t.grade = "signed"
		}
		err := sysconfig.ConfigureTargetSystem(fake20Model(t.grade), &sysconfig.Options{
			AllowCloudInit:  true,
			TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			CloudInitSrcDir: cloudCfgSrcDir,
			GadgetDir:       gadgetDir,
		})
		c.Assert(err, IsNil, comment)

		ubuntuDataCloudCfg := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud.cfg.d/")
		for i, name := range seedSrcNames {
			if t.resultCfgCopied[i] {
				c.Check(filepath.Join(ubuntuDataCloudCfg, "90_"+name), testutil.FilePresent, comment)
			} else {
				c.Check(filepath.Join(ubuntuDataCloudCfg, "90_"+name), testutil.FileAbsent, comment)
			}
		}

		if t.gadgetCfg != "" {
			ubuntuDataCloudCfg := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud.cfg.d/")
			c.Check(filepath.Join(ubuntuDataCloudCfg, "80_device_gadget.cfg"), testutil.FileEquals, t.gadgetCfg)

		}

		// check the restrict file we should have installed in some cases too
		restrictFile := filepath.Join(ubuntuDataCloudCfg, "99_snapd_datasource.cfg")
		if t.expDsRestrictFileContents != "" {
			c.Check(restrictFile, testutil.FileEquals, t.expDsRestrictFileContents, comment)
		} else {
			c.Check(restrictFile, testutil.FileAbsent, comment)
		}

		// make sure the disabled file is absent
		ubuntuDataCloudDisabled := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
		c.Check(ubuntuDataCloudDisabled, testutil.FileAbsent)

		// need to clear this dir each time as it doesn't change for each
		// iteration
		c.Assert(os.RemoveAll(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), IsNil)
	}
}

func (s *sysconfigSuite) TestInstallModeCloudInitDisallowedGradeSecuredDoesDisable(c *C) {
	err := sysconfig.ConfigureTargetSystem(fake20Model("secured"), &sysconfig.Options{
		AllowCloudInit: false,
		TargetRootDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
	})
	c.Assert(err, IsNil)

	ubuntuDataCloudDisabled := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FilePresent)
}

func (s *sysconfigSuite) TestInstallModeCloudInitAllowedGradeSecuredIgnoresSrcButDoesNotDisable(c *C) {
	cloudCfgSrcDir, _ := s.makeCloudCfgSrcDirFiles(c)

	err := sysconfig.ConfigureTargetSystem(fake20Model("secured"), &sysconfig.Options{
		AllowCloudInit:  true,
		CloudInitSrcDir: cloudCfgSrcDir,
		TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
	})
	c.Assert(err, IsNil)

	// the disable file is not present
	ubuntuDataCloudDisabled := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(ubuntuDataCloudDisabled, testutil.FileAbsent)

	// but we did not copy the config files from ubuntu-seed, even though they
	// are there and cloud-init is not disabled
	ubuntuDataCloudCfg := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "foo.cfg"), testutil.FileAbsent)
	c.Check(filepath.Join(ubuntuDataCloudCfg, "bar.cfg"), testutil.FileAbsent)
}

// this test is the same as the logic from install mode devicestate, where we
// want to install cloud-init configuration not onto the running, ephemeral
// writable, but rather the host writable partition that will be used upon
// reboot into run mode
func (s *sysconfigSuite) TestInstallModeCloudInitInstallsOntoHostRunMode(c *C) {
	cloudCfgSrcDir, cfgFileNames := s.makeCloudCfgSrcDirFiles(c, "#cloud-config foo", "#cloud-config bar")

	err := sysconfig.ConfigureTargetSystem(fake20Model("dangerous"), &sysconfig.Options{
		AllowCloudInit:  true,
		CloudInitSrcDir: cloudCfgSrcDir,
		TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
	})
	c.Assert(err, IsNil)

	// and did copy the cloud-init files
	ubuntuDataCloudCfg := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/cloud/cloud.cfg.d/")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "90_"+cfgFileNames[0]), testutil.FileEquals, "#cloud-config foo")
	c.Check(filepath.Join(ubuntuDataCloudCfg, "90_"+cfgFileNames[1]), testutil.FileEquals, "#cloud-config bar")
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
		expectedLog     string
	}{
		{
			comment:         "done",
			cloudInitOutput: "status: done",
			exp:             sysconfig.CloudInitDone,
		},
		{
			comment:         "done",
			cloudInitOutput: "status: done",
			exp:             sysconfig.CloudInitDone,
			exitCode:        2,
			expectedLog:     `.*cloud-init status returned 'recoverable error' status: cloud-init completed but experienced errors\n`,
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
		{
			comment:         "normal cloud-init output w/ exit code 1",
			cloudInitOutput: "status: foobar",
			exitCode:        1,
			expError:        "cloud-init errored: status: foobar",
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
			err = os.WriteFile(filepath.Join(cloudDir, "cloud-init.disabled"), nil, 0644)
			c.Assert(err, IsNil)
		}

		if t.restrictedFile {
			cloudDir := filepath.Join(dirs.GlobalRootDir, "etc/cloud/cloud.cfg.d")
			err := os.MkdirAll(cloudDir, 0755)
			c.Assert(err, IsNil)
			err = os.WriteFile(filepath.Join(cloudDir, "zzzz_snapd.cfg"), nil, 0644)
			c.Assert(err, IsNil)
		}

		logBuf, restore := logger.MockLogger()
		defer restore()

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

		if t.expectedLog != "" {
			c.Check(logBuf.String(), Matches, t.expectedLog)
		}
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
			err = os.WriteFile(statusJSONFile, []byte(t.cloudInitStatusJSON), 0644)
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

const maasCloudInitImplicitYAML = `
datasource:
  MAAS:
    foo: bar
`

const gceCloudInitImplicitYAML = `
datasource:
  GCE:
    foo: bar
`

const maasGadgetCloudInitImplicitLowerCaseYAML = `
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
			configFileContent: maasCloudInitImplicitYAML,
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"MAAS"},
			},
			comment: "implicitly mentioned datasource",
		},
		{
			configFileContent: maasGadgetCloudInitImplicitLowerCaseYAML,
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
		err := os.WriteFile(configFile, []byte(t.configFileContent), 0644)
		c.Assert(err, IsNil, comment)
		res, err := sysconfig.CloudDatasourcesInUse(configFile)
		if t.expError != "" {
			c.Assert(err, ErrorMatches, t.expError, comment)
			continue
		}

		c.Assert(res, DeepEquals, t.expRes, comment)
	}
}

func (s *sysconfigSuite) TestCloudDatasourcesInUseForDirInUse(c *C) {
	tt := []struct {
		configFilesContents map[string]string
		expError            string
		expRes              *sysconfig.CloudDatasourcesInUseResult
		comment             string
	}{
		{
			configFilesContents: map[string]string{
				"maas.cfg": `datasource_list: [MAAS]`,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"MAAS"},
			},
			comment: "explicitly allowed via datasource_list",
		},
		{
			configFilesContents: map[string]string{
				"1_maas.cfg": `datasource_list: [MAAS]`,
				"2_none.cfg": `datasource_list: []`,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyNoneAllowed: true,
				Mentioned:             []string{"MAAS"},
			},
			comment: "explicit none overwriting explicit allowing",
		},
		{
			configFilesContents: map[string]string{
				"1_none.cfg": `datasource_list: []`,
				"2_maas.cfg": `datasource_list: [MAAS]`,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyNoneAllowed: false,
				ExplicitlyAllowed:     []string{"MAAS"},
				Mentioned:             []string{"MAAS"},
			},
			comment: "explicit allowing overwriting explicit none",
		},
		{
			configFilesContents: map[string]string{
				"maas.cfg": maasCloudInitImplicitYAML,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"MAAS"},
			},
			comment: "implicit datasource",
		},
		{
			configFilesContents: map[string]string{
				"1_gce.cfg":  gceCloudInitImplicitYAML,
				"2_maas.cfg": maasCloudInitImplicitYAML,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"GCE", "MAAS"},
			},
			comment: "multiple implicit datasources",
		},
		{
			configFilesContents: map[string]string{
				"1_maas.cfg": maasCloudInitImplicitYAML,
				"2_gce.cfg":  gceCloudInitImplicitYAML,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"GCE", "MAAS"},
			},
			comment: "multiple implicit datasources in different lexical order",
		},
		{
			configFilesContents: map[string]string{
				"maas.cfg": `datasource_list: [MAAS]`,
				"gce.cfg":  gceCloudInitImplicitYAML,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed: []string{"MAAS"},
				Mentioned:         []string{"GCE", "MAAS"},
			},
			comment: "implicit datasources and explicit datasource",
		},
		{
			configFilesContents: map[string]string{},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				ExplicitlyAllowed:     nil,
				ExplicitlyNoneAllowed: false,
				Mentioned:             nil,
			},
			comment: "no files means empty result",
		},
		{
			configFilesContents: map[string]string{
				"maas.conf": `datasource_list: [MAAS]`,
			},
			expRes:  &sysconfig.CloudDatasourcesInUseResult{},
			comment: "only .cfg files are allowed",
		},
		{
			configFilesContents: map[string]string{
				"maas":    `datasource_list: [MAAS]`,
				"gce.cfg": gceCloudInitImplicitYAML,
			},
			expRes: &sysconfig.CloudDatasourcesInUseResult{
				Mentioned: []string{"GCE"},
			},
			comment: "with .cfg and non-.cfg files, only .cfg files are allowed",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)

		dir := c.MkDir()
		for basename, content := range t.configFilesContents {
			configFile := filepath.Join(dir, basename)
			err := os.WriteFile(configFile, []byte(content), 0644)
			c.Assert(err, IsNil, comment)
		}

		res, err := sysconfig.CloudDatasourcesInUseForDir(dir)
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
		err := os.WriteFile(inFile, []byte(t.inStr), 0755)
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
		b, err := os.ReadFile(out)
		c.Assert(err, IsNil, comment)
		c.Assert(string(b), Equals, t.outStr, comment)
	}
}
