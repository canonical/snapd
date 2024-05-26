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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type backlightSuite struct {
	configcoreSuite
}

var _ = Suite(&backlightSuite{})

func (s *backlightSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755))

}

func (s *backlightSuite) TestConfigureBacklightServiceMaskIntegration(c *C) {
	s.systemctlArgs = nil
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.disable-backlight-service": true,
		},
	}))

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "mask", "systemd-backlight@.service"},
	})
}

func (s *backlightSuite) TestConfigureBacklightServiceUnmaskIntegration(c *C) {
	s.systemctlArgs = nil
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.disable-backlight-service": false,
		},
	}))

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "unmask", "systemd-backlight@.service"},
	})
}

func (s *backlightSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.disable-backlight-service": "true",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", tmpDir, "mask", "systemd-backlight@.service"},
	})
}
