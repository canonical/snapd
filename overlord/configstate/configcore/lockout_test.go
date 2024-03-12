// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type faillockSuite struct {
	configcoreSuite

	markerPath string
}

var _ = Suite(&faillockSuite{})

func (s *faillockSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	s.markerPath = filepath.Join(dirs.GlobalRootDir, "/etc/writable/account-lockout.enabled")
	err := os.MkdirAll(filepath.Dir(s.markerPath), 0755)
	c.Assert(err, IsNil)
}

func (s *faillockSuite) TestFaillockSetTrue(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"users.lockout": "true"},
	})
	c.Assert(err, IsNil)
	c.Check(s.markerPath, testutil.FilePresent)
}

func (s *faillockSuite) TestFaillockSetFalse(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"users.lockout": "false"},
	})
	c.Assert(err, IsNil)
	c.Check(s.markerPath, testutil.FileAbsent)
}

func (s *faillockSuite) TestFaillockSetFalseReset(c *C) {
	err := os.WriteFile(s.markerPath, nil, 0644)
	c.Assert(err, IsNil)

	err = configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"users.lockout": "false"},
	})
	c.Assert(err, IsNil)
	c.Check(s.markerPath, testutil.FileAbsent)
}

func (s *faillockSuite) TestFaillockHandlesErrors(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"users.lockout": "invalid-value"},
	})
	c.Assert(err, ErrorMatches, "users.lockout can only be set to 'true' or 'false'")
}

func (s *faillockSuite) TestFaillockUnsetChangeNothing(c *C) {
	err := os.WriteFile(s.markerPath, nil, 0644)
	c.Assert(err, IsNil)

	err = configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"users.lockout": ""},
	})
	c.Assert(err, IsNil)
	c.Check(s.markerPath, testutil.FilePresent)
}
