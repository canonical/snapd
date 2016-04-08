// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/systemd"
)

type ServiceActorSuite struct {
	i      int
	argses [][]string
	outs   [][]byte
	errors []error
	j      int
	jsvcs  [][]string
	jouts  [][]byte
	jerrs  []error
	pb     progress.Meter
}

var _ = Suite(&ServiceActorSuite{})

// borrowed from systemd_test
func (s *ServiceActorSuite) myRun(args ...string) (out []byte, err error) {
	s.argses = append(s.argses, args)
	if s.i < len(s.outs) {
		out = s.outs[s.i]
	}
	if s.i < len(s.errors) {
		err = s.errors[s.i]
	}
	s.i++
	return out, err
}
func (s *ServiceActorSuite) myJctl(svcs []string) (out []byte, err error) {
	s.jsvcs = append(s.jsvcs, svcs)

	if s.j < len(s.jouts) {
		out = s.jouts[s.j]
	}
	if s.j < len(s.jerrs) {
		err = s.jerrs[s.j]
	}
	s.j++

	return out, err
}

func (s *ServiceActorSuite) SetUpTest(c *C) {
	// force UTC timezone, for reproducible timestamps
	os.Setenv("TZ", "")

	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapSnapsDir, 0755)

	// TODO: this mkdir hack is so enable doesn't fail; remove when enable is the same as the rest
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/multi-user.target.wants"), 0755), IsNil)
	systemd.SystemctlCmd = s.myRun
	systemd.JournalctlCmd = s.myJctl
	_, err := makeInstalledMockSnap(`name: hello-snap
version: 1.09
apps:
 svc1:
   command: bin/hello
   daemon: forking
 non-svc2:
   command: something
`)
	c.Assert(err, IsNil)
	f, err := makeInstalledMockSnap(`name: hello-snap
version: 1.10
apps:
 svc1:
   command: bin/hello
   daemon: forking
 non-svc2:
   command: something
`)
	c.Assert(err, IsNil)
	c.Assert(makeSnapActive(f), IsNil)
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil
	s.j = 0
	s.jsvcs = nil
	s.jouts = nil
	s.jerrs = nil
	s.pb = &MockProgressMeter{}
}

func (s *ServiceActorSuite) TestFindServicesNoPackages(c *C) {
	_, err := FindServices("notfound", "", s.pb)
	c.Check(err, Equals, ErrPackageNotFound)
}

func (s *ServiceActorSuite) TestFindServicesNoPackagesNoPattern(c *C) {
	// tricky way of hiding the installed package ;)
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapSnapsDir, 0755)

	actor, err := FindServices("", "", s.pb)
	c.Check(err, IsNil)
	c.Assert(actor, NotNil)
	c.Check(actor.(*serviceActor).svcs, HasLen, 0)
}

func (s *ServiceActorSuite) TestFindServicesNoServices(c *C) {
	_, err := FindServices("hello-snap", "notfound", s.pb)
	c.Check(err, Equals, ErrServiceNotFound)
}

func (s *ServiceActorSuite) TestFindServicesFindsServices(c *C) {
	actor, err := FindServices("", "", s.pb)
	c.Assert(err, IsNil)
	c.Assert(actor, NotNil)
	c.Check(actor.(*serviceActor).svcs, HasLen, 1)

	s.outs = [][]byte{
		nil, // for the "stop"
		[]byte("ActiveState=inactive\n"), // for stop's check
		nil, // for the "start"
		nil, // for restart's stop
		[]byte("ActiveState=inactive\n"), // for restart's stop's check
		nil, // for restart's start
		// nil, // for the "enable" TODO: enable is different for now
		nil, // for enable's reload
		nil, // for the "disable"
		nil, // for disable's reload
		[]byte("Id=x\nLoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\n"), // status
		[]byte("Id=x\nLoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\n"), // status obj
	}
	s.errors = []error{
		nil, nil, // stop & check
		nil,           // start
		nil, nil, nil, // restart (== stop & start)
		// nil,                // enable  TODO: enable is different for now
		nil,                // for enable's reload
		nil,                // disable
		nil,                // for disable's reload
		nil,                // status
		nil,                // status obj
		&systemd.Timeout{}, // flag
	}
	s.jerrs = nil
	s.jouts = [][]byte{
		[]byte(`{"foo": "bar", "baz": 42}`),                    // for the Logs call
		[]byte(`{"__REALTIME_TIMESTAMP":"42","MESSAGE":"hi"}`), // for the Loglines call
	}

	c.Check(actor.Stop(), IsNil)
	c.Check(actor.Start(), IsNil)
	c.Check(actor.Restart(), IsNil)
	c.Check(actor.Enable(), IsNil)
	c.Check(actor.Disable(), IsNil)

	status, err := actor.Status()
	c.Check(err, IsNil)
	c.Assert(status, HasLen, 1)
	c.Check(status[0], Equals, "hello-snap\tsvc1\tenabled; loaded; active (running)")

	stobj, err := actor.ServiceStatus()
	c.Check(err, IsNil)
	c.Assert(stobj, HasLen, 1)
	c.Check(stobj[0], DeepEquals, &PackageServiceStatus{
		ServiceStatus: systemd.ServiceStatus{
			ServiceFileName: "hello-snap_svc1_1.10.service",
			LoadState:       "loaded",
			ActiveState:     "active",
			SubState:        "running",
			UnitFileState:   "enabled",
		},
		PackageName: "hello-snap",
		AppName:     "svc1",
	})

	logs, err := actor.Logs()
	c.Check(err, IsNil)
	c.Check(logs, DeepEquals, []systemd.Log{{"foo": "bar", "baz": 42.}})
	lines, err := actor.Loglines()
	c.Check(err, IsNil)
	c.Check(lines, DeepEquals, []string{"1970-01-01T00:00:00.000042Z - hi"})
}

func (s *ServiceActorSuite) TestFindServicesReportsErrors(c *C) {
	actor, err := FindServices("", "", s.pb)
	c.Assert(err, IsNil)
	c.Assert(actor, NotNil)
	c.Check(actor.(*serviceActor).svcs, HasLen, 1)

	anError := errors.New("error")

	s.errors = []error{
		anError, // stop
		anError, // start
		anError, // restart
		// anError, // enable  TODO: enable is different for now
		anError, // disable
		anError, // status
	}
	s.jerrs = []error{anError, anError}

	c.Check(actor.Stop(), NotNil)
	c.Check(actor.Start(), NotNil)
	c.Check(actor.Restart(), NotNil)
	// c.Check(actor.Enable(), NotNil) TODO: enable is different for now
	c.Check(actor.Disable(), NotNil)
	_, err = actor.Status()
	c.Check(err, NotNil)
	_, err = actor.Logs()
	c.Check(err, NotNil)
	_, err = actor.Loglines()
	c.Check(err, NotNil)
}

func (s *ServiceActorSuite) TestFindServicesIgnoresForegroundApps(c *C) {
	_, err := FindServices("hello-snap", "non-svc2", s.pb)
	c.Check(err, Equals, ErrServiceNotFound)
}
