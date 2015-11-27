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

package classic

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/testutil"
)

type EnabledTestSuite struct {
	testutil.BaseTest

	runInChroot [][]string
}

var _ = Suite(&EnabledTestSuite{})

func makeMockClassicEnv(c *C) {
	canary := filepath.Join(dirs.ClassicDir, "etc/apt/sources.list")
	err := os.MkdirAll(filepath.Dir(canary), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)
}

func (t *EnabledTestSuite) SetUpTest(c *C) {
	t.BaseTest.SetUpTest(c)

	dirs.ClassicDir = c.MkDir()
}

func (t *EnabledTestSuite) TestEnabledNotEnabled(c *C) {
	c.Assert(Enabled(), Equals, false)
}

func (t *EnabledTestSuite) TestEnabledEnabled(c *C) {
	makeMockClassicEnv(c)

	c.Assert(Enabled(), Equals, true)
}
