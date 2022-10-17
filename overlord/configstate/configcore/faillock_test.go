// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type faillockSuite struct {
	configcoreSuite
}

var _ = Suite(&faillockSuite{})

func (s *faillockSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/writable"), 0755)
	c.Assert(err, IsNil)
}

func (s *faillockSuite) TestFaillockSetTrue(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"system.faillock": "true"},
	})
	c.Assert(err, IsNil)

	marker := filepath.Join(dirs.GlobalRootDir, "/etc/writable/faillock.enabled")
	c.Assert(osutil.FileExists(marker), Equals, true)
}

func (s *faillockSuite) TestFaillockSetFalse(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"system.faillock": "false"},
	})
	c.Assert(err, IsNil)

	marker := filepath.Join(dirs.GlobalRootDir, "/etc/writable/faillock.enabled")
	c.Assert(osutil.FileExists(marker), Equals, false)
}

func (s *faillockSuite) TestFaillockSetFalseReset(c *C) {
	marker := filepath.Join(dirs.GlobalRootDir, "/etc/writable/faillock.enabled")
	markerFd, err := os.Create(marker)
	c.Assert(err, IsNil)
	markerFd.Close()
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"system.faillock": "false"},
	})

	c.Assert(osutil.FileExists(marker), Equals, false)
}
