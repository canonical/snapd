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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/osutil"

	// to set ConfigcoreFilesystemOnlyApply hook
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/sysconfig"
)

var gadgetYaml = `
volumes:
  pc:
    bootloader: grub
`

func (s *sysconfigSuite) TestGadgetDefaults(c *C) {
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

	rsyslogServiceFile := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/systemd/system/rsyslog.service")
	journalPath := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/var/log/journal")
	sshDontRunFile := filepath.Join(boot.InstallHostWritableDir, "_writable_defaults/etc/ssh/sshd_not_to_be_run")

	// sanity
	c.Check(osutil.FileExists(rsyslogServiceFile), Equals, false)
	c.Check(osutil.FileExists(sshDontRunFile), Equals, false)
	exists, _, _ := osutil.DirExists(journalPath)
	c.Check(exists, Equals, false)

	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
		GadgetDir:     snapInfo.MountDir(),
	})
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(rsyslogServiceFile), Equals, true)
	c.Check(osutil.IsSymlink(rsyslogServiceFile), Equals, true)
	c.Check(osutil.FileExists(sshDontRunFile), Equals, true)
	exists, _, _ = osutil.DirExists(journalPath)
	c.Check(exists, Equals, true)
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

	err := sysconfig.ConfigureRunSystem(&sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
		GadgetDir:     snapInfo.MountDir(),
	})
	c.Check(err, ErrorMatches, `option "rsyslog.service" has invalid value "foo"`)
}
