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

	. "gopkg.in/check.v1"

	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/systemd"
	"os"
	"path/filepath"
)

type ServiceActorSuite struct {
	i      int
	argses [][]string
	outs   [][]byte
	errors []error
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

func (s *ServiceActorSuite) SetUpTest(c *C) {
	SetRootDir(c.MkDir())
	// TODO: this mkdir hack is so enable doesn't fail; remove when enable is the same as the rest
	c.Assert(os.MkdirAll(filepath.Join(globalRootDir, "/etc/systemd/system/multi-user.target.wants"), 0755), IsNil)
	systemd.SystemctlCmd = s.myRun
	makeInstalledMockSnap(globalRootDir, "")
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil
	s.pb = &MockProgressMeter{}
}

func (s *ServiceActorSuite) TestFindServicesNoPackages(c *C) {
	_, err := FindServices("notfound", "", s.pb)
	c.Check(err, Equals, ErrPackageNotFound)
}

func (s *ServiceActorSuite) TestFindServicesNoPackagesNoPattern(c *C) {
	// tricky way of hiding the installed package ;)
	SetRootDir(c.MkDir())
	actor, err := FindServices("", "", s.pb)
	c.Check(err, IsNil)
	c.Assert(actor, NotNil)
	c.Check(actor.svcs, HasLen, 0)
}

func (s *ServiceActorSuite) TestFindServicesNoServices(c *C) {
	_, err := FindServices("hello-app", "notfound", s.pb)
	c.Check(err, Equals, ErrServiceNotFound)
}

func (s *ServiceActorSuite) TestFindServicesFindsServices(c *C) {
	actor, err := FindServices("", "", s.pb)
	c.Assert(err, IsNil)
	c.Assert(actor, NotNil)
	c.Check(actor.svcs, HasLen, 1)

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
		&systemd.Timeout{}, // flag
	}

	c.Check(actor.Stop(), IsNil)
	c.Check(actor.Start(), IsNil)
	c.Check(actor.Restart(), IsNil)
	c.Check(actor.Enable(), IsNil)
	c.Check(actor.Disable(), IsNil)
	status, err := actor.Status()
	c.Check(err, IsNil)
	c.Assert(status, HasLen, 1)
	c.Check(status[0], Equals, "hello-app\tsvc1\tenabled; loaded; active (running)")
}

func (s *ServiceActorSuite) TestFindServicesReportsErrors(c *C) {
	actor, err := FindServices("", "", s.pb)
	c.Assert(err, IsNil)
	c.Assert(actor, NotNil)
	c.Check(actor.svcs, HasLen, 1)

	anError := errors.New("error")

	s.outs = [][]byte{
		nil,
		nil,
		nil,
		nil,
	}
	s.errors = []error{
		anError, // stop
		anError, // start
		anError, // restart
		// anError, // enable  TODO: enable is different for now
		anError, // disable
		anError, // status
	}

	c.Check(actor.Stop(), NotNil)
	c.Check(actor.Start(), NotNil)
	c.Check(actor.Restart(), NotNil)
	// c.Check(actor.Enable(), NotNil) TODO: enable is different for now
	c.Check(actor.Disable(), NotNil)
	_, err = actor.Status()
	c.Check(err, NotNil)
}
