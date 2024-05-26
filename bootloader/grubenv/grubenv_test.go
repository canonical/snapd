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
	"fmt"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/testutil"
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

func (g *grubenvTestSuite) TestSet(c *C) {
	env := grubenv.NewEnv(g.envPath)
	c.Check(env, NotNil)

	env.Set("key", "value")
	c.Check(env.Get("key"), Equals, "value")
}

func (g *grubenvTestSuite) TestSave(c *C) {
	env := grubenv.NewEnv(g.envPath)
	c.Check(env, NotNil)

	env.Set("key1", "value1")
	env.Set("key2", "value2")
	env.Set("key3", "value3")
	env.Set("key4", "value4")
	env.Set("key5", "value5")
	env.Set("key6", "value6")
	env.Set("key7", "value7")
	// set "key1" again, ordering (position) does not change
	env.Set("key1", "value1")
	mylog.Check(env.Save())


	c.Assert(g.envPath, testutil.FileEquals, `# GRUB Environment Block
key1=value1
key2=value2
key3=value3
key4=value4
key5=value5
key6=value6
key7=value7
###################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################`)
}

func (g *grubenvTestSuite) TestSaveOverflow(c *C) {
	env := grubenv.NewEnv(g.envPath)
	c.Check(env, NotNil)

	for i := 0; i < 101; i++ {
		env.Set(fmt.Sprintf("key%d", i), "foo")
	}
	mylog.Check(env.Save())
	c.Assert(err, ErrorMatches, `cannot write grubenv .*: bigger than 1024 bytes \(1026\)`)
}
