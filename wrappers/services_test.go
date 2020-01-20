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
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

type servicesTestSuite struct {
	tempdir string

	sysdLog [][]string

	systemctlRestorer, delaysRestorer func()

	perfTimings timings.Measurer
}

var _ = Suite(&servicesTestSuite{})

func (s *servicesTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	s.sysdLog = nil
	dirs.SetRootDir(s.tempdir)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.delaysRestorer = systemd.MockStopDelays(time.Millisecond, 25*time.Second)
	s.perfTimings = timings.New(nil)

}

func (s *servicesTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.systemctlRestorer()
	s.delaysRestorer()
}

func (s *servicesTestSuite) TestAddSnapServicesAndRemove(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
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

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 2)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(svcFile)},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
	})

	s.sysdLog = nil
	err = wrappers.RemoveSnapServices(info, progress.Null)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(svcFile), Equals, false)
	c.Assert(s.sysdLog, HasLen, 2)
	c.Check(s.sysdLog[0], DeepEquals, []string{"--root", dirs.GlobalRootDir, "disable", filepath.Base(svcFile)})
	c.Check(s.sysdLog[1], DeepEquals, []string{"daemon-reload"})
}

var snapdYaml = `name: snapd
version: 1.0
type: snapd
`

func (s *servicesTestSuite) TestRemoveSnapWithSocketsRemovesSocketsService(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc1:
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_DATA/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_COMMON/sock2.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	err = wrappers.StopServices(info.Services(), "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapServices(info, &progress.Null)
	c.Assert(err, IsNil)

	app := info.Apps["svc1"]
	c.Assert(app.Sockets, HasLen, 2)
	for _, socket := range app.Sockets {
		c.Check(osutil.FileExists(socket.File()), Equals, false)
	}
}

func (s *servicesTestSuite) TestRemoveSnapPackageFallbackToKill(c *C) {
	restore := wrappers.MockKillWait(time.Millisecond)
	defer restore()

	var sysdLog [][]string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// filter out the "systemctl show" that
		// StopServices generates
		if cmd[0] != "show" {
			sysdLog = append(sysdLog, cmd)
		}
		return []byte("ActiveState=active\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, `name: wat
version: 42
apps:
 wat:
   command: wat
   stop-timeout: 20ms
   daemon: forking
`, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	sysdLog = nil

	svcFName := "snap.wat.wat.service"

	err = wrappers.StopServices(info.Services(), "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", svcFName},
		// check kill invocations
		{"kill", svcFName, "-s", "TERM", "--kill-who=all"},
		{"kill", svcFName, "-s", "KILL", "--kill-who=all"},
	})
}

func (s *servicesTestSuite) TestServicesEnableState(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := "snap.hello-snap.svc1.service"
	svc2File := "snap.hello-snap.svc2.service"

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
		# shifting by 2 also drops the temp dir arg to --root
	    shift 2
	fi

	case "$1" in
		is-enabled)
			case "$2" in 
			"snap.hello-snap.svc1.service")
				echo "disabled"
				exit 1
				;;
			"snap.hello-snap.svc2.service")
				echo "enabled"
				exit 0
				;;
			*)
				echo "unexpected is-enabled of service $2"
				exit 2
				;;
			esac
	        ;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac

	exit 1
	`)
	defer r.Restore()

	states, err := wrappers.ServicesEnableState(info, progress.Null)
	c.Assert(err, IsNil)

	c.Assert(states, DeepEquals, map[string]bool{
		"svc1": false,
		"svc2": true,
	})

	// the calls could be out of order in the list, since iterating over a map
	// is non-deterministic, so manually check each call
	c.Assert(r.Calls(), HasLen, 2)
	for _, call := range r.Calls() {
		c.Assert(call, HasLen, 5)
		c.Assert(call[:4], DeepEquals, []string{"systemctl", "--root", s.tempdir, "is-enabled"})
		switch call[4] {
		case svc1File, svc2File:
		default:
			c.Errorf("unknown service for systemctl call: %s", call[4])
		}
	}
}

func (s *servicesTestSuite) TestServicesEnableStateFail(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := "snap.hello-snap.svc1.service"

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
		# shifting by 2 also drops the temp dir arg to --root
	    shift 2
	fi

	case "$1" in
		is-enabled)
			case "$2" in
			"snap.hello-snap.svc1.service")
				echo "whoops"
				exit 1
				;;
			*)
				echo "unexpected is-enabled of service $2"
				exit 2
				;;
			esac
	        ;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac

	exit 1
	`)
	defer r.Restore()

	_, err := wrappers.ServicesEnableState(info, progress.Null)
	c.Assert(err, ErrorMatches, ".*is-enabled snap.hello-snap.svc1.service\\] failed with exit status 1: whoops\n.*")

	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", s.tempdir, "is-enabled", svc1File},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithDisabledServices(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
	    shift 2
	fi

	case "$1" in
		enable)
			case "$2" in 
				"snap.hello-snap.svc1.service")
					echo "unexpected enable of disabled service $2"
					exit 1
					;;
				"snap.hello-snap.svc2.service")
					exit 0
					;;
				*)
					echo "unexpected enable of service $2"
					exit 1
					;;
			esac
			;;
		daemon-reload)
			exit 0
			;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac
	exit 2
	`)
	defer r.Restore()

	// svc1 will be disabled
	disabledSvcs := []string{"svc1"}

	err := wrappers.AddSnapServices(info, disabledSvcs, progress.Null)
	c.Assert(err, IsNil)

	// only svc2 should be enabled
	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", s.tempdir, "enable", "snap.hello-snap.svc2.service"},
		{"systemctl", "daemon-reload"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithDisabledServicesNowApp(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	// mock the logger
	buf, loggerRestore := logger.MockLogger()
	defer loggerRestore()

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
	    shift 2
	fi

	case "$1" in
		enable)
			case "$2" in 
				"snap.hello-snap.svc1.service")
					exit 0
					;;
				*)
					echo "unexpected enable of service $2"
					exit 1
					;;
			esac
			;;
		daemon-reload)
			exit 0
			;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac
	exit 2
	`)
	defer r.Restore()

	svcs := []string{"hello"}

	err := wrappers.AddSnapServices(info, svcs, progress.Null)
	c.Assert(err, IsNil)

	// check the log for the notice
	c.Assert(buf.String(), Matches, `.*previously disabled service hello is now an app and not a service\n.*`)

	// the cleanup of AddSnapServices will remove written service files and then
	// call reload, but note that we should catch any non-svc apps before
	// actually enabling them, so we just see a daemon-reload call and not any
	// enable calls
	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", s.tempdir, "enable", "snap.hello-snap.svc1.service"},
		{"systemctl", "daemon-reload"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithDisabledServicesMissing(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	// mock the logger
	buf, loggerRestore := logger.MockLogger()
	defer loggerRestore()

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
	    shift 2
	fi

	case "$1" in
		enable)
			case "$2" in 
				"snap.hello-snap.svc1.service")
					exit 0
					;;
				*)
					echo "unexpected enable of service $2"
					exit 1
					;;
			esac
			;;
		daemon-reload)
			exit 0
			;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac
	exit 2
	`)
	defer r.Restore()

	svcs := []string{"old-disabled-svc"}

	err := wrappers.AddSnapServices(info, svcs, progress.Null)
	c.Assert(err, IsNil)

	// check the log for the notice
	c.Assert(buf.String(), Matches, `.*previously disabled service old-disabled-svc no longer exists\n.*`)

	// the cleanup of AddSnapServices will remove written service files and then
	// call reload, but note that we should catch any non-svc apps before
	// actually enabling them, so we just see a daemon-reload call and not any
	// enable calls
	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", s.tempdir, "enable", "snap.hello-snap.svc1.service"},
		{"systemctl", "daemon-reload"},
	})
}

func (s *servicesTestSuite) TestStopServicesWithSockets(c *C) {
	var sysdLog []string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		if cmd[0] == "stop" {
			sysdLog = append(sysdLog, cmd[1])
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc1:
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_DATA/sock2.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	sysdLog = nil

	err = wrappers.StopServices(info.Services(), "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	sort.Strings(sysdLog)
	c.Check(sysdLog, DeepEquals, []string{
		"snap.hello-snap.svc1.service", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket"})
}

func (s *servicesTestSuite) TestStartServices(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.StartServices(info.Services(), nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"--root", s.tempdir, "is-enabled", filepath.Base(svcFile)},
		{"start", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestEnableServices(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.EnableSnapServices(info, nil)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"--root", s.tempdir, "enable", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestNoStartDisabledServices(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
	    shift 2
	fi

	case "$1" in
	    is-enabled)
	        if [ "$2" = "snap.hello-snap.svc1.service" ]; then
	            echo "disabled"
	            exit 1
	        else
	            echo "unexpected call $*"
	            exit 2
	        fi
	        ;;
	    *)
	        echo "unexpected call $*"
	        exit 2
	esac
	`)
	defer r.Restore()

	err := wrappers.StartServices(info.Services(), nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", s.tempdir, "is-enabled", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesFailCreateCleanup(c *C) {
	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  daemon: potato
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, ErrorMatches, ".*potato.*")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	// *either* the first service failed validation, and nothing
	// was done, *or* the second one failed, and the first one was
	// enabled before the second failed, and disabled after.
	if len(s.sysdLog) > 0 {
		// the second service failed validation
		c.Check(s.sysdLog, DeepEquals, [][]string{
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

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
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
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
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
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
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

	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
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

func (s *servicesTestSuite) TestAddSnapSocketFiles(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc1:
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_DATA/sock2.socket
    sock3:
      listen-stream: $XDG_RUNTIME_DIR/sock3.socket

`, &snap.SideInfo{Revision: snap.R(12)})

	sock1File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.sock1.socket")
	sock2File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.sock2.socket")
	sock3File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.sock3.socket")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	expected := fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock1
ListenStream=%s
SocketMode=0666

`, filepath.Join(s.tempdir, "/var/snap/hello-snap/common/sock1.socket"))
	c.Check(sock1File, testutil.FileContains, expected)

	expected = fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock2
ListenStream=%s

`, filepath.Join(s.tempdir, "/var/snap/hello-snap/12/sock2.socket"))
	c.Check(sock2File, testutil.FileContains, expected)

	expected = fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock3
ListenStream=%s

`, filepath.Join(s.tempdir, "/run/user/0/snap.hello-snap/sock3.socket"))
	c.Check(sock3File, testutil.FileContains, expected)
}

func (s *servicesTestSuite) TestStartSnapMultiServicesFailStartCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 2 && cmd[0] == "start" {
			name := cmd[len(cmd)-1]
			if name == svc2Name {
				return nil, fmt.Errorf("failed")
			}
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	svcs := info.Services()
	c.Assert(svcs, HasLen, 2)
	if svcs[0].Name == "svc2" {
		svcs[0], svcs[1] = svcs[1], svcs[0]
	}
	err := wrappers.StartServices(svcs, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(sysdLog, HasLen, 8, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", s.tempdir, "is-enabled", svc1Name},
		{"--root", s.tempdir, "is-enabled", svc2Name},
		{"start", svc1Name},
		{"start", svc2Name}, // one of the services fails
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestStartSnapMultiServicesFailStartCleanupWithSockets(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	svc2SocketName := "snap.hello-snap.svc2.sock1.socket"
	svc3Name := "snap.hello-snap.svc3.service"
	svc3SocketName := "snap.hello-snap.svc3.sock1.socket"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		c.Logf("call: %v", cmd)
		if len(cmd) >= 2 && cmd[0] == "start" && cmd[1] == svc3SocketName {
			// svc2 socket fails
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
 svc3:
  command: bin/hello
  daemon: simple
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
`, &snap.SideInfo{Revision: snap.R(12)})

	// ensure desired order
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"], info.Apps["svc3"]}

	err := wrappers.StartServices(apps, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Logf("sysdlog: %v", sysdLog)
	c.Assert(sysdLog, HasLen, 17, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", s.tempdir, "is-enabled", svc1Name},
		{"--root", s.tempdir, "enable", svc2SocketName},
		{"start", svc2SocketName},
		{"--root", s.tempdir, "enable", svc3SocketName},
		{"start", svc3SocketName}, // start failed, what follows is the cleanup
		{"stop", svc3SocketName},
		{"show", "--property=ActiveState", svc3SocketName},
		{"stop", svc3Name},
		{"show", "--property=ActiveState", svc3Name},
		{"--root", s.tempdir, "disable", svc3SocketName},
		{"stop", svc2SocketName},
		{"show", "--property=ActiveState", svc2SocketName},
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"--root", s.tempdir, "disable", svc2SocketName},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestStartSnapServicesKeepsOrder(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.services-snap.svc1.service"
	svc2Name := "snap.services-snap.svc2.service"
	svc3Name := "snap.services-snap.svc3.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, `name: services-snap
apps:
  svc1:
    daemon: simple
    before: [svc3]
  svc2:
    daemon: simple
    after: [svc1]
  svc3:
    daemon: simple
    before: [svc2]
`, &snap.SideInfo{Revision: snap.R(12)})

	svcs := info.Services()
	c.Assert(svcs, HasLen, 3)

	sorted, err := snap.SortServices(svcs)
	c.Assert(err, IsNil)

	err = wrappers.StartServices(sorted, nil, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 6, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", s.tempdir, "is-enabled", svc1Name},
		{"--root", s.tempdir, "is-enabled", svc3Name},
		{"--root", s.tempdir, "is-enabled", svc2Name},
		{"start", svc1Name},
		{"start", svc3Name},
		{"start", svc2Name},
	}, Commentf("calls: %v", sysdLog))

	// change the order
	sorted[1], sorted[0] = sorted[0], sorted[1]

	// we should observe the calls done in the same order as services
	err = wrappers.StartServices(sorted, nil, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 12, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog[6:], DeepEquals, [][]string{
		{"--root", s.tempdir, "is-enabled", svc3Name},
		{"--root", s.tempdir, "is-enabled", svc1Name},
		{"--root", s.tempdir, "is-enabled", svc2Name},
		{"start", svc3Name},
		{"start", svc1Name},
		{"start", svc2Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestServiceAfterBefore(c *C) {
	snapYaml := packageHello + `
 svc2:
   daemon: forking
   after: [svc1]
 svc3:
   daemon: forking
   before: [svc4]
   after:  [svc2]
 svc4:
   daemon: forking
   after:
     - svc1
     - svc2
     - svc3
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	checks := []struct {
		file    string
		kind    string
		matches []string
	}{{
		file:    filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service"),
		kind:    "After",
		matches: []string{info.Apps["svc1"].ServiceName()},
	}, {
		file:    filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		kind:    "After",
		matches: []string{info.Apps["svc2"].ServiceName()},
	}, {
		file:    filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		kind:    "Before",
		matches: []string{info.Apps["svc4"].ServiceName()},
	}, {
		file: filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc4.service"),
		kind: "After",
		matches: []string{
			info.Apps["svc1"].ServiceName(),
			info.Apps["svc2"].ServiceName(),
			info.Apps["svc3"].ServiceName(),
		},
	}}

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	for _, check := range checks {
		content, err := ioutil.ReadFile(check.file)
		c.Assert(err, IsNil)

		for _, m := range check.matches {
			c.Check(string(content), Matches,
				// match:
				//   ...
				//   After=other.mount some.target foo.service bar.service
				//   Before=foo.service bar.service
				//   ...
				// but not:
				//   Foo=something After=foo.service Bar=something else
				// or:
				//   After=foo.service
				//   bar.service
				// or:
				//   After=  foo.service    bar.service
				"(?ms).*^(?U)"+check.kind+"=.*\\s?"+regexp.QuoteMeta(m)+"\\s?[^=]*$")
		}
	}

}

func (s *servicesTestSuite) TestServiceWatchdog(c *C) {
	snapYaml := packageHello + `
 svc2:
   daemon: forking
   watchdog-timeout: 12s
 svc3:
   daemon: forking
   watchdog-timeout: 0s
 svc4:
   daemon: forking
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "\nWatchdogSec=12\n"), Equals, true)

	noWatchdog := []string{
		filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc4.service"),
	}
	for _, svcPath := range noWatchdog {
		content, err := ioutil.ReadFile(svcPath)
		c.Assert(err, IsNil)
		c.Check(strings.Contains(string(content), "WatchdogSec="), Equals, false)
	}
}

func (s *servicesTestSuite) TestStopServiceEndure(c *C) {
	const surviveYaml = `name: survive-snap
version: 1.0
apps:
 survivor:
  command: bin/survivor
  refresh-mode: endure
  daemon: simple
`
	info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})
	survivorFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.survive-snap.survivor.service")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", filepath.Base(survivorFile)},
		{"daemon-reload"},
	})

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), snap.StopReasonRefresh, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 0)

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), snap.StopReasonRemove, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(survivorFile)},
		{"show", "--property=ActiveState", "snap.survive-snap.survivor.service"},
	})

}

func (s *servicesTestSuite) TestStopServiceSigs(c *C) {
	r := wrappers.MockKillWait(1 * time.Millisecond)
	defer r()

	survivorFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.survive-snap.srv.service")
	for _, t := range []struct {
		mode        string
		expectedSig string
		expectedWho string
	}{
		{mode: "sigterm", expectedSig: "TERM", expectedWho: "main"},
		{mode: "sigterm-all", expectedSig: "TERM", expectedWho: "all"},
		{mode: "sighup", expectedSig: "HUP", expectedWho: "main"},
		{mode: "sighup-all", expectedSig: "HUP", expectedWho: "all"},
		{mode: "sigusr1", expectedSig: "USR1", expectedWho: "main"},
		{mode: "sigusr1-all", expectedSig: "USR1", expectedWho: "all"},
		{mode: "sigusr2", expectedSig: "USR2", expectedWho: "main"},
		{mode: "sigusr2-all", expectedSig: "USR2", expectedWho: "all"},
	} {
		surviveYaml := fmt.Sprintf(`name: survive-snap
version: 1.0
apps:
 srv:
  command: bin/survivor
  stop-mode: %s
  daemon: simple
`, t.mode)
		info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})

		s.sysdLog = nil
		err := wrappers.AddSnapServices(info, nil, progress.Null)
		c.Assert(err, IsNil)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"--root", dirs.GlobalRootDir, "enable", filepath.Base(survivorFile)},
			{"daemon-reload"},
		})

		s.sysdLog = nil
		err = wrappers.StopServices(info.Services(), snap.StopReasonRefresh, progress.Null, s.perfTimings)
		c.Assert(err, IsNil)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"stop", filepath.Base(survivorFile)},
			{"show", "--property=ActiveState", "snap.survive-snap.srv.service"},
		}, Commentf("failure in %s", t.mode))

		s.sysdLog = nil
		err = wrappers.StopServices(info.Services(), snap.StopReasonRemove, progress.Null, s.perfTimings)
		c.Assert(err, IsNil)
		switch t.expectedWho {
		case "all":
			c.Check(s.sysdLog, DeepEquals, [][]string{
				{"stop", filepath.Base(survivorFile)},
				{"show", "--property=ActiveState", "snap.survive-snap.srv.service"},
			})
		case "main":
			c.Check(s.sysdLog, DeepEquals, [][]string{
				{"stop", filepath.Base(survivorFile)},
				{"show", "--property=ActiveState", "snap.survive-snap.srv.service"},
				{"kill", filepath.Base(survivorFile), "-s", "TERM", "--kill-who=all"},
				{"kill", filepath.Base(survivorFile), "-s", "KILL", "--kill-who=all"},
			})
		default:
			panic("not reached")
		}
	}

}

func (s *servicesTestSuite) TestStartSnapTimerEnableStart(c *C) {
	svc1Name := "snap.hello-snap.svc1.service"
	// svc2Name := "snap.hello-snap.svc2.service"
	svc2Timer := "snap.hello-snap.svc2.timer"

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"]}
	err := wrappers.StartServices(apps, nil, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 4, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "is-enabled", svc1Name},
		{"--root", dirs.GlobalRootDir, "enable", svc2Timer},
		{"start", svc2Timer},
		{"start", svc1Name},
	}, Commentf("calls: %v", s.sysdLog))
}

func (s *servicesTestSuite) TestStartSnapTimerCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	svc2Timer := "snap.hello-snap.svc2.timer"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 2 && cmd[0] == "start" && cmd[1] == svc2Timer {
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"]}
	err := wrappers.StartServices(apps, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(sysdLog, HasLen, 10, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "is-enabled", svc1Name},
		{"--root", dirs.GlobalRootDir, "enable", svc2Timer},
		{"start", svc2Timer}, // this call fails
		{"stop", svc2Timer},
		{"show", "--property=ActiveState", svc2Timer},
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"--root", dirs.GlobalRootDir, "disable", svc2Timer},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestAddRemoveSnapWithTimersAddsRemovesTimerFiles(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	app := info.Apps["svc2"]
	c.Assert(app.Timer, NotNil)

	c.Check(osutil.FileExists(app.Timer.File()), Equals, true)
	c.Check(osutil.FileExists(app.ServiceFile()), Equals, true)

	err = wrappers.StopServices(info.Services(), "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapServices(info, &progress.Null)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(app.Timer.File()), Equals, false)
	c.Check(osutil.FileExists(app.ServiceFile()), Equals, false)
}

func (s *servicesTestSuite) TestFailedAddSnapCleansUp(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
 svc3:
  command: bin/hello
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
`, &snap.SideInfo{Revision: snap.R(12)})

	calls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		if len(cmd) == 1 && cmd[0] == "daemon-reload" && calls == 0 {
			// only fail the first systemd daemon-reload call, the
			// second one is at the end of cleanup
			calls += 1
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, NotNil)

	c.Logf("services dir: %v", dirs.SnapServicesDir)
	matches, err := filepath.Glob(dirs.SnapServicesDir + "/*")
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0, Commentf("the following autogenerated files were left behind: %v", matches))
}

func (s *servicesTestSuite) TestAddServicesDidReload(c *C) {
	const base = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
`
	onlyServices := snaptest.MockSnap(c, base+`
 svc1:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	onlySockets := snaptest.MockSnap(c, base+`
 svc1:
  command: bin/hello
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
`, &snap.SideInfo{Revision: snap.R(12)})

	onlyTimers := snaptest.MockSnap(c, base+`
 svc1:
  command: bin/hello
  daemon: oneshot
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	for i, info := range []*snap.Info{onlyServices, onlySockets, onlyTimers} {
		s.sysdLog = nil
		err := wrappers.AddSnapServices(info, nil, progress.Null)
		c.Assert(err, IsNil)
		reloads := 0
		c.Logf("calls: %v", s.sysdLog)
		for _, call := range s.sysdLog {
			if strutil.ListContains(call, "daemon-reload") {
				reloads += 1
			}
		}
		c.Check(reloads >= 1, Equals, true, Commentf("test-case %v did not reload services as expected", i))
	}
}

func (s *servicesTestSuite) TestSnapServicesActivation(c *C) {
	const snapYaml = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
 svc1:
  command: bin/hello
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
 svc2:
  command: bin/hello
  daemon: oneshot
  timer: 10:00-12:00
 svc3:
  command: bin/hello
  daemon: simple
`

	svc3Name := "snap.hello-snap.svc3.service"

	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 2, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// only svc3 gets started during boot
		{"--root", dirs.GlobalRootDir, "enable", svc3Name},
		{"daemon-reload"},
	}, Commentf("calls: %v", s.sysdLog))
}

func (s *servicesTestSuite) TestServiceRestartDelay(c *C) {
	snapYaml := packageHello + `
 svc2:
   daemon: forking
   restart-delay: 12s
 svc3:
   daemon: forking
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "\nRestartSec=12\n"), Equals, true)

	content, err = ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "RestartSec="), Equals, false)
}
