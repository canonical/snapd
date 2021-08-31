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
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type timezoneSuite struct {
	configcoreSuite
}

var _ = Suite(&timezoneSuite{})

func (s *timezoneSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/writable"), 0755)
	c.Assert(err, IsNil)
	localtimePath := filepath.Join(dirs.GlobalRootDir, "/etc/writable/localtime")
	err = os.Symlink("/usr/share/zoneinfo/WET", localtimePath)
	c.Assert(err, IsNil)
}

func (s *timezoneSuite) TestConfigureTimezoneInvalid(c *C) {
	invalidTimezones := []string{
		"no-#", "no-ä", "no/triple/slash/",
	}

	for _, tz := range invalidTimezones {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.timezone": tz,
			},
		})
		c.Assert(err, ErrorMatches, `cannot set timezone.*`)
	}
}

func (s *timezoneSuite) TestConfigureTimezoneIntegration(c *C) {
	mockedTimedatectl := testutil.MockCommand(c, "timedatectl", "")
	defer mockedTimedatectl.Restore()

	validTimezones := []string{
		"UTC", "Europe/Malta", "US/Indiana-Starke", "Africa/Sao_Tome",
		"America/Argentina/Cordoba", "America/Argentina/La_Rioja",
		"Etc/GMT+1", "CST6CDT", "GMT0", "GMT-0", "PST8PDT",
	}

	for _, tz := range validTimezones {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.timezone": tz,
			},
		})
		c.Assert(err, IsNil)
		c.Check(mockedTimedatectl.Calls(), DeepEquals, [][]string{
			{"timedatectl", "set-timezone", tz},
		}, Commentf("tested timezone: %v", tz))
		mockedTimedatectl.ForgetCalls()
	}
}

func (s *timezoneSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.timezone": "Europe/Berlin",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	c.Check(filepath.Join(tmpDir, "/etc/writable/timezone"), testutil.FileEquals, "Europe/Berlin\n")
	p, err := os.Readlink(filepath.Join(tmpDir, "/etc/writable/localtime"))
	c.Assert(err, IsNil)
	c.Check(p, Equals, "/usr/share/zoneinfo/Europe/Berlin")
}
