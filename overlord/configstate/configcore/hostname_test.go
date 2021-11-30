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

	script := `if [ "$1" = "status" ]; then echo bar; fi`
	s.mockedHostnamectl = testutil.MockCommand(c, "hostnamectl", script)
	s.AddCleanup(s.mockedHostnamectl.Restore)

	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)
}

func (s *hostnameSuite) TestConfigureHostnameFsOnlyInvalid(c *C) {
	tmpdir := c.MkDir()

	filler := strings.Repeat("x", 60)
	invalidHostnames := []string{
		"-no-start-with-dash", "no-ä", "no/slash", "foo..bar",
		strings.Repeat("x", 64),
		strings.Join([]string{filler, filler, filler, filler, filler}, "."),
		// systemd testcases, see test-hostname-util.c
		"foobar.com.", "fooBAR.", "fooBAR.com.", "fööbar",
		".", "..", "foobar.", ".foobar", "foo..bar", "foo.bar..",
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"au-xph5-rvgrdsb5hcxc-47et3a5vvkrc-server-wyoz4elpdpe3.openstack.local",
	}

	for _, name := range invalidHostnames {
		conf := configcore.PlainCoreConfig(map[string]interface{}{
			"system.hostname": name,
		})
		err := configcore.FilesystemOnlyApply(coreDev, tmpdir, conf)
		c.Assert(err, ErrorMatches, `cannot set hostname.*`, Commentf("%v", name))
	}

	c.Check(s.mockedHostnamectl.Calls(), HasLen, 0)
}

func (s *hostnameSuite) TestConfigureHostnameFsOnlyHappy(c *C) {
	tmpdir := c.MkDir()

	filler := strings.Repeat("x", 16)
	validHostnames := []string{
		"a",
		"foo",
		strings.Repeat("x", 63),
		"foo-bar",
		"foo-------bar",
		"foo99",
		"99foo",
		"localhost.localdomain",
		"foo.-bar.com",
		"can-end-with-a-dash-",
		// can look like a serial
		"C253432146-00214",
		"C253432146-00214UPPERATTHEENDTOO",
		// FQDN is ok too
		"CS1.lse.ac.uk.edu",
		// 3*16 + 12 + 3 dots = 63
		strings.Join([]string{filler, filler, filler, strings.Repeat("x", 12)}, "."),
		// systemd testcases, see test-hostname-util.c
		"foobar", "foobar.com", "fooBAR", "fooBAR.com",
	}

	for _, name := range validHostnames {
		conf := configcore.PlainCoreConfig(map[string]interface{}{
			"system.hostname": name,
		})
		err := configcore.FilesystemOnlyApply(coreDev, tmpdir, conf)
		c.Assert(err, IsNil)
	}

	c.Check(s.mockedHostnamectl.Calls(), HasLen, 0)
}

func (s *hostnameSuite) TestConfigureHostnameWithState(c *C) {
	hostnames := []string{
		"good",
		"bäd-hostname-is-only-validated-by-hostnamectl",
	}

	for _, hostname := range hostnames {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.hostname": hostname,
			},
		})
		c.Assert(err, IsNil)
		c.Check(s.mockedHostnamectl.Calls(), DeepEquals, [][]string{
			{"hostnamectl", "status", "--pretty"},
			{"hostnamectl", "set-hostname", hostname},
		})
		s.mockedHostnamectl.ForgetCalls()
	}
}

func (s *hostnameSuite) TestConfigureHostnameIntegrationSameHostname(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// and set new hostname to "foo"
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			// hostname is already "bar"
			"system.hostname": "bar",
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.mockedHostnamectl.Calls(), DeepEquals, [][]string{
		{"hostnamectl", "status", "--pretty"},
	})
}

func (s *hostnameSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.hostname": "bar",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	c.Check(filepath.Join(tmpDir, "/etc/writable/hostname"), testutil.FileEquals, "bar\n")
}
