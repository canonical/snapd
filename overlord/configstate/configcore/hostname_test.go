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

	mockedHostnamectl *testutil.MockCmd
}

var _ = Suite(&hostnameSuite{})

func (s *hostnameSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)

	s.mockedHostnamectl = testutil.MockCommand(c, "hostnamectl", "")
	s.AddCleanup(s.mockedHostnamectl.Restore)
}

func (s *hostnameSuite) TestConfigureHostnameInvalid(c *C) {
	invalidHostnames := []string{
		"-no-start-with-dash", "no-upper-A", "no-ä", "no/slash",
		"ALL-CAPS-IS-NEVER-OKAY", "no-SHOUTING-allowed",
		strings.Repeat("x", 64),
	}

	for _, name := range invalidHostnames {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.hostname": name,
			},
		})
		c.Assert(err, ErrorMatches, `cannot set hostname.*`)
	}

	c.Check(s.mockedHostnamectl.Calls(), HasLen, 0)
}

func (s *hostnameSuite) TestConfigureHostnameIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockedHostname := testutil.MockCommand(c, "hostname", "echo bar")
	defer mockedHostname.Restore()

	validHostnames := []string{
		"foo",
		strings.Repeat("x", 63),
		"foo-bar",
		"foo-------bar",
		"foo99",
		"99foo",
		"can-end-with-a-dash-",
	}

	for _, hostname := range validHostnames {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.hostname": hostname,
			},
		})
		c.Assert(err, IsNil)
		c.Check(mockedHostname.Calls(), DeepEquals, [][]string{
			{"hostname"},
		})
		c.Check(s.mockedHostnamectl.Calls(), DeepEquals, [][]string{
			{"hostnamectl", "set-hostname", hostname},
		})
		s.mockedHostnamectl.ForgetCalls()
		mockedHostname.ForgetCalls()
	}
}

func (s *hostnameSuite) TestConfigureHostnameIntegrationSameHostname(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// pretent current hostname is "foo"
	mockedHostname := testutil.MockCommand(c, "hostname", "echo foo")
	defer mockedHostname.Restore()
	// and set new hostname to "foo"
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.hostname": "foo",
		},
	})
	c.Assert(err, IsNil)
	c.Check(mockedHostname.Calls(), DeepEquals, [][]string{
		{"hostname"},
	})
	c.Check(s.mockedHostnamectl.Calls(), HasLen, 0)
}

func (s *hostnameSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.hostname": "bar",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	c.Check(filepath.Join(tmpDir, "/etc/writable/hostname"), testutil.FileEquals, "bar\n")
}
