// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package androidbootenv_test

import (
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/partition/androidbootenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type androidbootenvTestSuite struct {
	envPath string
}

var _ = Suite(&androidbootenvTestSuite{})

func (a *androidbootenvTestSuite) SetUpTest(c *C) {
	a.envPath = filepath.Join(c.MkDir(), "androidbootenv")
	env := androidbootenv.NewEnv(a.envPath)
}

func (a *androidbootenvTestSuite) TestSet(c *C) {
	c.Assert(env, NotNil)

	env.Set("key", "value")
	c.Check(env.Get("key"), Equals, "value")
}

func (a *androidbootenvTestSuite) TestSaveAndLoad(c *C) {
	c.Assert(env, NotNil)

	env.Set("key1", "value1")
	env.Set("key2", "")
	env.Set("key3", "value3")

	err := env.Save()
	c.Assert(err, IsNil)

	env2 := androidbootenv.NewEnv(a.envPath)
	c.Check(env2, NotNil)

	err = env2.Load()
	c.Assert(err, IsNil)

	c.Assert(env2.Get("key1"), Equals, "value1")
	c.Assert(env2.Get("key2"), Equals, "")
	c.Assert(env2.Get("key3"), Equals, "value3")
}
