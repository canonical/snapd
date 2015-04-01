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

package systemctl

import (
	"testing"

	. "launchpad.net/gocheck"
	"time"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// systemctl's testsuite
type SystemctlTestSuite struct {
	i      int
	argses [][]string
	errors []error
	outs   [][]byte
}

var _ = Suite(&SystemctlTestSuite{})

func (s *SystemctlTestSuite) SetUpTest(c *C) {
	Systemctl = s.myRun
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil
}

func (s *SystemctlTestSuite) TearDownTest(c *C) {
	Systemctl = run
}

func (s *SystemctlTestSuite) myRun(args ...string) (out []byte, err error) {
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

func (s *SystemctlTestSuite) TestDaemonReload(c *C) {
	err := DaemonReload()
	c.Assert(err, IsNil)
	c.Assert(s.argses, DeepEquals, [][]string{{"daemon-reload"}})
}

func (s *SystemctlTestSuite) TestStart(c *C) {
	err := Start("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"start", "foo"}})
}

func (s *SystemctlTestSuite) TestStop(c *C) {
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=active\n"),
		[]byte("ActiveState=inactive\n"),
	}
	s.errors = []error{nil, nil, nil, nil, &Timeout{}}
	err := Stop("foo")
	c.Assert(err, IsNil)
	c.Assert(s.argses, HasLen, 4)
	c.Check(s.argses[0], DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], DeepEquals, s.argses[2])
	c.Check(s.argses[1], DeepEquals, s.argses[3])
}

func (s *SystemctlTestSuite) TestStopTimeout(c *C) {
	oldSteps := stopSteps
	oldDelay := stopDelay
	stopSteps = 2
	stopDelay = time.Millisecond
	defer func() {
		stopSteps = oldSteps
		stopDelay = oldDelay
	}()

	err := Stop("foo")
	c.Assert(err, FitsTypeOf, &Timeout{})
}

func (s *SystemctlTestSuite) TestDisable(c *C) {
	err := Disable("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", RootDir, "disable", "foo"}})
}

func (s *SystemctlTestSuite) TestEnable(c *C) {
	err := Enable("foo")
	c.Assert(err, IsNil)
	c.Check(s.argses, DeepEquals, [][]string{{"--root", RootDir, "enable", "foo"}})
}
