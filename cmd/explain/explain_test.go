// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package explain_test

import (
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/explain"
)

func Test(t *testing.T) { TestingT(t) }

type explainSuite struct {
	stdout *os.File
}

var _ = Suite(&explainSuite{})

func (s *explainSuite) SetUpTest(c *C) {
	f, err := ioutil.TempFile("", "stdout-*.txt")
	c.Assert(err, IsNil)
	s.stdout = f
	explain.MockStdout(f)
}

func (s *explainSuite) TearDownTest(c *C) {
	if s.stdout != nil {
		defer s.stdout.Close()
		os.Remove(s.stdout.Name())
	}
}

func (s *explainSuite) TestSay(c *C) {
	explain.Say("hello")
	c.Check(explain.StdoutText(), Equals, "")

	explain.Enable()
	defer explain.Disable()
	explain.Say("hello again")
	explain.Say("\t- list item")
	// Tabs get expanded to two spaces.
	c.Check(explain.StdoutText(), Equals, "hello again\n  - list item\n")
}

func (s *explainSuite) TestHeader(c *C) {
	explain.Header("snap-foo")
	c.Check(explain.StdoutText(), Equals, "")

	explain.Header("snap-bar")
	defer explain.Disable()
	explain.Say("\n<< snap-bar >>\n\n")
}

func (s *explainSuite) TestDo(c *C) {
	var called bool
	explain.Do(func() { called = true })
	c.Check(called, Equals, false)

	explain.Enable()
	defer explain.Disable()
	explain.Do(func() { called = true })
	c.Check(called, Equals, true)
}

func (s *explainSuite) TestEnableDisable(c *C) {
	explain.Enable()
	c.Check(os.Getenv("SNAP_EXPLAIN"), Equals, "1")
	explain.Disable()
	c.Check(os.Getenv("SNAP_EXPLAIN"), Equals, "")
}
