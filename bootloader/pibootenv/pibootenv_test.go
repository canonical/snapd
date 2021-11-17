// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package pibootenv_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/pibootenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type pienvTestSuite struct {
	envFile string
}

var _ = Suite(&pienvTestSuite{})

func (p *pienvTestSuite) SetUpTest(c *C) {
	p.envFile = filepath.Join(c.MkDir(), "piboot.conf")
}

func (p *pienvTestSuite) TestSetNoDuplicate(c *C) {
	env := pibootenv.NewEnv(p.envFile)
	env.Set("foo", "bar")
	env.Set("foo", "bar")
	env.Save()

	buf, err := ioutil.ReadFile(p.envFile)
	c.Assert(err, IsNil)
	c.Assert(string(buf), Equals, "foo=bar\n")
}

func (p *pienvTestSuite) TestOpenEnv(c *C) {
	env := pibootenv.NewEnv(p.envFile)
	env.Set("foo", "bar")
	err := env.Save()
	c.Assert(err, IsNil)

	env2 := pibootenv.NewEnv(p.envFile)
	err = env2.Load()
	c.Assert(err, IsNil)
	c.Assert(env2.Get("foo"), Equals, "bar")
}

func (p *pienvTestSuite) TestGetSimple(c *C) {
	env := pibootenv.NewEnv(p.envFile)
	env.Set("foo", "bar")
	c.Assert(env.Get("foo"), Equals, "bar")
}

func (p *pienvTestSuite) TestGetNoSuchEntry(c *C) {
	env := pibootenv.NewEnv(p.envFile)
	c.Assert(env.Get("no-such-entry"), Equals, "")
}

func (p *pienvTestSuite) TestSetEmptyUnsets(c *C) {
	env := pibootenv.NewEnv(p.envFile)

	env.Set("foo", "bar")
	c.Assert(env.Get("foo"), Equals, "bar")
	env.Set("foo", "")
	c.Assert(env.Get("foo"), Equals, "")
}

func (p *pienvTestSuite) TestMerge(c *C) {
	env := pibootenv.NewEnv(p.envFile)
	env.Set("foo", "bar1")

	env2 := pibootenv.NewEnv(filepath.Join(c.MkDir(), "piboot2.conf"))
	env2.Set("other", "hello")
	env2.Set("foo", "bar2")

	env.Merge(env2)
	c.Assert(env.Get("foo"), Equals, "bar2")
	c.Assert(env.Get("other"), Equals, "hello")
}
