// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/wrappers"
)

type servicesTestSuite struct {
	tempdir    string
	prevctlCmd func(...string) ([]byte, error)
}

var _ = Suite(&servicesTestSuite{})

func (s *servicesTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)

	s.prevctlCmd = systemd.SystemctlCmd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}
}

func (s *servicesTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	systemd.SystemctlCmd = s.prevctlCmd
}

func (s *servicesTestSuite) TestAddSnapServicesAndRemove(c *C) {
	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	info := snaptest.MockSnap(c, packageHello, contentsHello, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)

	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	content, err := ioutil.ReadFile(svcFile)
	c.Assert(err, IsNil)

	verbs := []string{"Start", "Stop", "StopPost"}
	cmds := []string{"", " --command=stop", " --command=post-stop"}
	for i := range verbs {
		expected := fmt.Sprintf("Exec%s=/usr/bin/snap run%s hello-snap.svc1", verbs[i], cmds[i])
		c.Check(string(content), Matches, "(?ms).*^"+regexp.QuoteMeta(expected)) // check.v1 adds ^ and $ around the regexp provided
	}

	sysdLog = nil
	err = wrappers.StopSnapServices(info, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 2)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(svcFile)},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
	})

	sysdLog = nil
	err = wrappers.RemoveSnapServices(info, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(svcFile), Equals, false)
	c.Assert(sysdLog, HasLen, 2)
	c.Check(sysdLog[0], DeepEquals, []string{"--root", dirs.GlobalRootDir, "disable", filepath.Base(svcFile)})
	c.Check(sysdLog[1], DeepEquals, []string{"daemon-reload"})
}

func (s *servicesTestSuite) TestRemoveSnapPackageFallbackToKill(c *C) {
	restore := wrappers.MockKillWait(200 * time.Millisecond)
	defer restore()

	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		// filter out the "systemctl show" that
		// StopSnapServicesGenerates
		if cmd[0] != "show" {
			sysdLog = append(sysdLog, cmd)
		}
		return []byte("ActiveState=active\n"), nil
	}

	info := snaptest.MockSnap(c, `name: wat
version: 42
apps:
 wat:
   command: wat
   stop-timeout: 250ms
   daemon: forking
`, "", &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)

	sysdLog = nil

	svcFName := "snap.wat.wat.service"

	err = wrappers.StopSnapServices(info, &progress.NullProgress{})
	c.Assert(err, IsNil)

	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", svcFName},
		// check kill invocations
		{"kill", svcFName, "-s", "TERM"},
		{"kill", svcFName, "-s", "KILL"},
	})
}

func (s *servicesTestSuite) TestStartSnapServices(c *C) {
	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	info := snaptest.MockSnap(c, packageHello, contentsHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.StartSnapServices(info, nil)
	c.Assert(err, IsNil)

	c.Assert(sysdLog, HasLen, 3)
	c.Check(sysdLog[0], DeepEquals, []string{"daemon-reload"})
	c.Check(sysdLog[1], DeepEquals, []string{"--root", dirs.GlobalRootDir, "enable", filepath.Base(svcFile)})
	c.Check(sysdLog[2], DeepEquals, []string{"start", filepath.Base(svcFile)})
}
