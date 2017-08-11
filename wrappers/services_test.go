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
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", filepath.Base(svcFile)},
		{"daemon-reload"},
	})

	content, err := ioutil.ReadFile(svcFile)
	c.Assert(err, IsNil)

	verbs := []string{"Start", "Stop", "StopPost"}
	cmds := []string{"", " --command=stop", " --command=post-stop"}
	for i := range verbs {
		expected := fmt.Sprintf("Exec%s=/usr/bin/snap run%s hello-snap.svc1", verbs[i], cmds[i])
		c.Check(string(content), Matches, "(?ms).*^"+regexp.QuoteMeta(expected)) // check.v1 adds ^ and $ around the regexp provided
	}

	sysdLog = nil
	err = wrappers.StopServices(info.Services(), &progress.NullProgress{})
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
		// StopServices generates
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

	err = wrappers.StopServices(info.Services(), &progress.NullProgress{})
	c.Assert(err, IsNil)

	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", svcFName},
		// check kill invocations
		{"kill", svcFName, "-s", "TERM"},
		{"kill", svcFName, "-s", "KILL"},
	})
}

func (s *servicesTestSuite) TestStartServices(c *C) {
	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	info := snaptest.MockSnap(c, packageHello, contentsHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.StartServices(info.Services(), nil)
	c.Assert(err, IsNil)

	c.Assert(sysdLog, DeepEquals, [][]string{{"start", filepath.Base(svcFile)}})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesFailCreateCleanup(c *C) {
	var sysdLog [][]string

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return nil, nil
	}

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  daemon: potato
`, contentsHello, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, ErrorMatches, ".*potato.*")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	// *either* the first service failed validation, and nothing
	// was done, *or* the second one failed, and the first one was
	// enabled before the second failed, and disabled after.
	if len(sysdLog) > 0 {
		// the second service failed validation
		c.Check(sysdLog, DeepEquals, [][]string{
			{"--root", dirs.GlobalRootDir, "enable", "snap.hello-snap.svc1.service"},
			{"--root", dirs.GlobalRootDir, "disable", "snap.hello-snap.svc1.service"},
			{"daemon-reload"},
		})
	}
}

func (s *servicesTestSuite) TestAddSnapMultiServicesFailEnableCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	numEnables := 0

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		sdcmd := cmd[0]
		if len(cmd) >= 2 {
			sdcmd = cmd[len(cmd)-2]
		}
		switch sdcmd {
		case "enable":
			numEnables++
			switch numEnables {
			case 1:
				if cmd[len(cmd)-1] == svc2Name {
					// the services are being iterated in the "wrong" order
					svc1Name, svc2Name = svc2Name, svc1Name
				}
				return nil, nil
			case 2:
				return nil, fmt.Errorf("failed")
			default:
				panic("expected no more than 2 enables")
			}
		case "disable", "daemon-reload":
			return nil, nil
		default:
			panic("unexpected systemctl command " + sdcmd)
		}
	}

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, contentsHello, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, ErrorMatches, "failed")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", svc1Name},
		{"--root", dirs.GlobalRootDir, "enable", svc2Name}, // this one fails
		{"--root", dirs.GlobalRootDir, "disable", svc1Name},
		{"daemon-reload"},
	})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesStartFailOnSystemdReloadCleanup(c *C) {
	// this test might be overdoing it (it's mostly covering the same ground as the previous one), but ... :-)
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	first := true
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) < 2 {
			return nil, fmt.Errorf("failed")
		}
		if first {
			first = false
			if cmd[len(cmd)-1] == svc2Name {
				// the services are being iterated in the "wrong" order
				svc1Name, svc2Name = svc2Name, svc1Name
			}
		}
		return nil, nil

	}

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, contentsHello, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, &progress.NullProgress{})
	c.Assert(err, ErrorMatches, "failed")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", svc1Name},
		{"--root", dirs.GlobalRootDir, "enable", svc2Name},
		{"daemon-reload"}, // this one fails
		{"--root", dirs.GlobalRootDir, "disable", svc1Name},
		{"--root", dirs.GlobalRootDir, "disable", svc2Name},
		{"daemon-reload"}, // so does this one :-)
	})
}

func (s *servicesTestSuite) TestStartSnapMultiServicesFailStartCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	numStarts := 0

	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 2 && cmd[len(cmd)-2] == "start" {
			numStarts++
			if numStarts == 2 {
				name := cmd[len(cmd)-1]
				if name == svc1Name {
					// the services are being iterated in the "wrong" order
					svc1Name, svc2Name = svc2Name, svc1Name
				}
				return nil, fmt.Errorf("failed")
			}
		}
		return []byte("ActiveState=inactive\n"), nil
	}

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, contentsHello, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.StartServices(info.Services(), nil)
	c.Assert(err, ErrorMatches, "failed")

	c.Assert(sysdLog, HasLen, 4)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"start", svc1Name},
		{"start", svc2Name}, // this one fails
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
	})
}
