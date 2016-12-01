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

package grubenv_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/partition/grubenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type grubenvTestSuite struct {
	envPath string
}

var _ = Suite(&grubenvTestSuite{})

func (g *grubenvTestSuite) SetUpTest(c *C) {
	g.envPath = filepath.Join(c.MkDir(), "grubenv")
}

func (g *grubenvTestSuite) TestSetenv(c *C) {
	env := grubenv.NewGrubenv(g.envPath)
	c.Check(env, NotNil)

	env.Setenv("key", "value")
	c.Check(env.Getenv("key"), Equals, "value")
}

func (g *grubenvTestSuite) TestSave(c *C) {
	env := grubenv.NewGrubenv(g.envPath)
	c.Check(env, NotNil)

	env.Setenv("key7", "value7")
	env.Setenv("key1", "value1")
	env.Setenv("key2", "value2")
	env.Setenv("key3", "value3")
	env.Setenv("key4", "value4")
	env.Setenv("key5", "value5")
	env.Setenv("key6", "value6")
	env.Setenv("key7", "value7")

	err := env.Save()
	c.Assert(err, IsNil)

	buf, err := ioutil.ReadFile(g.envPath)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, []byte(`# GRUB Environment Block
key1=value1
key2=value2
key3=value3
key4=value4
key5=value5
key6=value6
key7=value7
###################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################`))
}
