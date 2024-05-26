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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"

	// to set ApplyFilesystemOnlyDefaults hook
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

var gadgetYaml = `
volumes:
  pc:
    bootloader: grub
`

var fakeModel = assertstest.FakeAssertion(map[string]interface{}{
	"type":         "model",
	"authority-id": "my-brand",
	"series":       "16",
	"brand-id":     "my-brand",
	"model":        "my-model",
	"architecture": "amd64",
	"base":         "core18",
	"gadget":       "pc",
	"kernel":       "pc-kernel",
}).(*asserts.Model)

func fake20Model(grade string) *asserts.Model {
	return assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        grade,
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "kernel",
				"id":              "kerneldididididididididididididi",
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              "gadgetididididididididididididid",
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	}).(*asserts.Model)
}

func (s *sysconfigSuite) TestConfigureTargetSystemNonUC20(c *C) {
	mylog.Check(sysconfig.ConfigureTargetSystem(fakeModel, nil))
	c.Assert(err, ErrorMatches, "internal error: ConfigureTargetSystem can only be used with a model with a grade")
}

func (s *sysconfigSuite) TestGadgetDefaults(c *C) {
	restore := osutil.MockMountInfo("")
	defer restore()

	const gadgetDefaultsYaml = `
defaults:
  system:
    service:
      rsyslog.disable: true
      ssh.disable: true
    journal.persistent: true
`
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   "idid",
	}
	snapInfo := snaptest.MockSnapWithFiles(c, "name: pc\ntype: gadget", si, [][]string{
		{"meta/gadget.yaml", gadgetYaml + gadgetDefaultsYaml},
	})

	var sysctlArgs [][]string
	systemctlRestorer := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		sysctlArgs = append(sysctlArgs, args)
		return nil, nil
	})
	defer systemctlRestorer()

	journalPath := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/var/log/journal")
	sshDontRunFile := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/ssh/sshd_not_to_be_run")

	// validity
	c.Check(osutil.FileExists(sshDontRunFile), Equals, false)
	exists, _, _ := osutil.DirExists(journalPath)
	c.Check(exists, Equals, false)
	mylog.Check(sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		TargetRootDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
		GadgetDir:     snapInfo.MountDir(),
	}))


	c.Check(osutil.FileExists(sshDontRunFile), Equals, true)
	exists, _, _ = osutil.DirExists(journalPath)
	c.Check(exists, Equals, true)

	c.Check(sysctlArgs, DeepEquals, [][]string{{"--root", filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults"), "mask", "rsyslog.service"}})
}

func (s *sysconfigSuite) TestInstallModeEarlyDefaultsFromGadgetInvalid(c *C) {
	const gadgetDefaultsYaml = `
defaults:
  system:
    service:
      rsyslog:
        disable: foo
`
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   "idid",
	}
	snapInfo := snaptest.MockSnapWithFiles(c, "name: pc\ntype: gadget", si, [][]string{
		{"meta/gadget.yaml", gadgetYaml + gadgetDefaultsYaml},
	})
	mylog.Check(sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		TargetRootDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
		GadgetDir:     snapInfo.MountDir(),
	}))
	c.Check(err, ErrorMatches, `option "service.rsyslog.disable" has invalid value "foo"`)
}

func (s *sysconfigSuite) TestInstallModeEarlyDefaultsFromGadgetSeedSnap(c *C) {
	restore := osutil.MockMountInfo("")
	defer restore()

	const gadgetDefaultsYaml = `
defaults:
  system:
    service:
      rsyslog.disable: true
      ssh.disable: true
    journal.persistent: true
`
	snapFile := snaptest.MakeTestSnapWithFiles(c, "name: pc\ntype: gadget\nversion: 1", [][]string{
		{"meta/gadget.yaml", gadgetYaml + gadgetDefaultsYaml},
	})

	snapContainer := squashfs.New(snapFile)

	var sysctlArgs [][]string
	systemctlRestorer := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		sysctlArgs = append(sysctlArgs, args)
		return nil, nil
	})
	defer systemctlRestorer()

	journalPath := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/var/log/journal")
	sshDontRunFile := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults/etc/ssh/sshd_not_to_be_run")

	// validity
	c.Check(osutil.FileExists(sshDontRunFile), Equals, false)
	exists, _, _ := osutil.DirExists(journalPath)
	c.Check(exists, Equals, false)
	mylog.Check(sysconfig.ConfigureTargetSystem(fake20Model("signed"), &sysconfig.Options{
		TargetRootDir: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
		GadgetSnap:    snapContainer,
	}))


	c.Check(osutil.FileExists(sshDontRunFile), Equals, true)
	exists, _, _ = osutil.DirExists(journalPath)
	c.Check(exists, Equals, true)

	c.Check(sysctlArgs, DeepEquals, [][]string{{"--root", filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "_writable_defaults"), "mask", "rsyslog.service"}})
}
