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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type hostnameSuite struct {
	configcoreSuite
}

var _ = Suite(&hostnameSuite{})

func (s *hostnameSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
}

func (s *hostnameSuite) TestConfigureHostnameInvalid(c *C) {
	invalidHostnames := []string{
		"-no-start-with-dash", "no-upper-A", "no-ä", "no/slash",
		strings.Repeat("x", 64),
	}

	for _, name := range invalidHostnames {
		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.hostname": name,
			},
		})
		c.Assert(err, ErrorMatches, `cannot set hostname.*`)
	}
}

func (s *hostnameSuite) TestConfigureHostnameIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockedHostnamectl := testutil.MockCommand(c, "hostnamectl", "")
	defer mockedHostnamectl.Restore()

	validHostnames := []string{"foo", strings.Repeat("x", 63)}

	for _, hostname := range validHostnames {
		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.hostname": hostname,
			},
		})
		c.Assert(err, IsNil)
		c.Check(mockedHostnamectl.Calls(), DeepEquals, [][]string{
			{"hostnamectl", "set-hostname", hostname},
		})
		mockedHostnamectl.ForgetCalls()
	}
}

func (s *hostnameSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.hostname": "bar",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(tmpDir, conf, nil), IsNil)

	c.Check(filepath.Join(tmpDir, "/etc/writable/hostname"), testutil.FileEquals, "bar\n")
}
