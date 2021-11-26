// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type tmpfsSuite struct {
	configcoreSuite

	servOverridePath string
	servOverrideDir  string
}

var _ = Suite(&tmpfsSuite{})

func (s *tmpfsSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	s.servOverrideDir = filepath.Join(dirs.SnapServicesDir, "tmp.mount.d")
	s.servOverridePath = filepath.Join(s.servOverrideDir, "override.conf")
}

// Configure with different valid values
func (s *tmpfsSuite) TestConfigureTmpfsGoodVals(c *C) {
	systemctlArgs := [][]string{}
	for _, size := range []string{"100m", "1g", "512k", "104857600",
		"2M", "7G", "1024K", "20%"} {

		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"tmpfs.size": size,
			},
		})
		c.Assert(err, IsNil)

		c.Check(s.servOverridePath, testutil.FileEquals,
			fmt.Sprintf("[Mount]\nOptions=mode=1777,strictatime,nosuid,nodev,size=%s\n",
				size))
		systemctlArgs = append(systemctlArgs,
			[]string{"daemon-reload"},
			[]string{"stop", "tmp.mount"},
			[]string{"show", "--property=ActiveState", "tmp.mount"},
			[]string{"start", "tmp.mount"})
	}

	c.Check(s.systemctlArgs, DeepEquals, systemctlArgs)
}

// Configure with different invalid values
func (s *tmpfsSuite) TestConfigureTmpfsBadVals(c *C) {
	for _, size := range []string{"100p", "0x123", "10485f7600", "20%%"} {

		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"tmpfs.size": size,
			},
		})
		c.Assert(err, ErrorMatches, `cannot set tmpfs size.*`)

		_, err = os.Stat(s.servOverridePath)
		c.Assert(os.IsNotExist(err), Equals, true)
	}

	c.Assert(s.systemctlArgs, IsNil)
}

// Ensure things are fine if destination folder already existed
func (s *tmpfsSuite) TestConfigureTmpfsgAllConfDirExistsAlready(c *C) {
	// make tmp.mount.d directory already
	err := os.MkdirAll(s.servOverrideDir, 0755)
	c.Assert(err, IsNil)

	size := "100m"
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"tmpfs.size": size,
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.servOverridePath, testutil.FileEquals,
		fmt.Sprintf("[Mount]\nOptions=mode=1777,strictatime,nosuid,nodev,size=%s\n", size))

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"stop", "tmp.mount"},
		{"show", "--property=ActiveState", "tmp.mount"},
		{"start", "tmp.mount"},
	})
}

// Test cfg file is not updated if we set the same size that is already set
func (s *tmpfsSuite) TestConfigureTmpfsNoFileUpdate(c *C) {
	err := os.MkdirAll(s.servOverrideDir, 0755)
	c.Assert(err, IsNil)
	size := "100m"
	content := "[Mount]\nOptions=mode=1777,strictatime,nosuid,nodev,size=" + size + "\n"
	err = ioutil.WriteFile(s.servOverridePath, []byte(content), 0644)
	c.Assert(err, IsNil)

	info, err := os.Stat(s.servOverridePath)
	c.Assert(err, IsNil)

	fileModTime := info.ModTime()

	// To make sure the times will defer if the file is newly written
	time.Sleep(100 * time.Millisecond)

	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"tmpfs.size": size,
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.servOverridePath, testutil.FileEquals, content)

	info, err = os.Stat(s.servOverridePath)
	c.Assert(err, IsNil)
	c.Assert(info.ModTime(), Equals, fileModTime)

	c.Check(s.systemctlArgs, HasLen, 0)
}

// Test that config file is removed when unsetting
func (s *tmpfsSuite) TestConfigureTmpfsRemovesIfUnset(c *C) {
	err := os.MkdirAll(s.servOverrideDir, 0755)
	c.Assert(err, IsNil)

	// add canary to ensure we don't touch other files
	canary := filepath.Join(s.servOverrideDir, "05-canary.conf")
	err = ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	content := "[Mount]\nOptions=mode=1777,strictatime,nosuid,nodev,size=1g\n"
	err = ioutil.WriteFile(s.servOverridePath, []byte(content), 0644)
	c.Assert(err, IsNil)

	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"tmpfs.size": "",
		},
	})
	c.Assert(err, IsNil)

	// ensure the file got deleted
	c.Check(osutil.FileExists(s.servOverridePath), Equals, false)
	// but the canary is still here
	c.Check(osutil.FileExists(canary), Equals, true)

	// apply defaults
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"stop", "tmp.mount"},
		{"show", "--property=ActiveState", "tmp.mount"},
		{"start", "tmp.mount"},
	})

}

// Test applying on image preparation
func (s *tmpfsSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"tmpfs.size": "4096k",
	})

	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	tmpfsOverrCfg := filepath.Join(tmpDir,
		"/etc/systemd/system/tmp.mount.d/override.conf")
	c.Check(tmpfsOverrCfg, testutil.FileEquals,
		"[Mount]\nOptions=mode=1777,strictatime,nosuid,nodev,size=4096k\n")
}
