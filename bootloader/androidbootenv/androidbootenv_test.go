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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/androidbootenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type androidbootenvTestSuite struct {
	envPath string
	env     *androidbootenv.Env
}

var _ = Suite(&androidbootenvTestSuite{})

func (a *androidbootenvTestSuite) SetUpTest(c *C) {
	a.envPath = filepath.Join(c.MkDir(), "androidbootenv")
	a.env = androidbootenv.NewEnv(a.envPath)
	c.Assert(a.env, NotNil)
}

func (a *androidbootenvTestSuite) TestSet(c *C) {
	a.env.Set("key", "value")
	c.Check(a.env.Get("key"), Equals, "value")
}

func (a *androidbootenvTestSuite) TestSaveAndLoad(c *C) {
	a.env.Set("key1", "value1")
	a.env.Set("key2", "")
	a.env.Set("key3", "value3")
	mylog.Check(a.env.Save())


	env2 := androidbootenv.NewEnv(a.envPath)
	c.Check(env2, NotNil)
	mylog.Check(env2.Load())


	c.Assert(env2.Get("key1"), Equals, "value1")
	c.Assert(env2.Get("key2"), Equals, "")
	c.Assert(env2.Get("key3"), Equals, "value3")
}
