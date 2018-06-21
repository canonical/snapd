// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type watchdogSuite struct {
	configcoreSuite

	mockEtcEnvironment string
}

var _ = Suite(&watchdogSuite{})

func (s *watchdogSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.mockEtcEnvironment = filepath.Join(dirs.SnapSystemdConfDir, "10-ubuntu-core-watchdog.conf")
	err := os.MkdirAll(dirs.SnapSystemdConfDir, 0755)
	c.Assert(err, IsNil)
}

func (s *watchdogSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *watchdogSuite) TestConfigureWatchdog(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for option, val := range map[string]string{"runtime-timeout": "10", "shutdown-timeout": "60"} {

		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("watchdog.%s", option): val + "s",
			},
		})
		c.Assert(err, IsNil)

		var systemdOption string
		switch option {
		case "runtime-timeout":
			systemdOption = "RuntimeWatchdogSec"
		case "shutdown-timeout":
			systemdOption = "ShutdownWatchdogSec"
		}
		c.Check(s.mockEtcEnvironment, testutil.FileEquals,
			fmt.Sprintf("[Manager]\n%s=%s\n", systemdOption, val))
	}
}

func (s *watchdogSuite) TestConfigureWatchdogUnits(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	times := []int{56, 432}
	type timeUnit struct {
		unit  string
		toSec int
	}

	for _, tunit := range []timeUnit{{"s", 1}, {"m", 60}, {"h", 3600}} {
		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"watchdog.runtime-timeout":  fmt.Sprintf("%d", times[0]) + tunit.unit,
				"watchdog.shutdown-timeout": fmt.Sprintf("%d", times[1]) + tunit.unit,
			},
		})
		c.Assert(err, IsNil)
		c.Check(s.mockEtcEnvironment, testutil.FileEquals, "[Manager]\n"+
			fmt.Sprintf("RuntimeWatchdogSec=%d\n", times[0]*tunit.toSec)+
			fmt.Sprintf("ShutdownWatchdogSec=%d\n", times[1]*tunit.toSec))
	}
}

func (s *watchdogSuite) TestConfigureWatchdogAll(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	times := []int{10, 100}
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"watchdog.runtime-timeout":  fmt.Sprintf("%ds", times[0]),
			"watchdog.shutdown-timeout": fmt.Sprintf("%ds", times[1]),
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.mockEtcEnvironment, testutil.FileEquals, "[Manager]\n"+
		fmt.Sprintf("RuntimeWatchdogSec=%d\n", times[0])+
		fmt.Sprintf("ShutdownWatchdogSec=%d\n", times[1]))
}

func (s *watchdogSuite) TestConfigureWatchdogBadFormat(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	type badValErr struct {
		val string
		err string
	}
	for _, badVal := range []badValErr{{"BAD", ".*invalid duration.*"},
		{"-5s", ".*negative duration.*"},
		{"34k", ".*unknown unit.*"}} {
		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"watchdog.runtime-timeout": badVal.val,
			},
		})
		c.Assert(err, ErrorMatches, badVal.err)
	}
}

func (s *watchdogSuite) TestConfigureWatchdogNoFileUpdate(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	times := []int{10, 100}
	content := "[Manager]\n" +
		fmt.Sprintf("RuntimeWatchdogSec=%d\n", times[0]) +
		fmt.Sprintf("ShutdownWatchdogSec=%d\n", times[1])
	err := ioutil.WriteFile(s.mockEtcEnvironment, []byte(content), 0644)
	c.Assert(err, IsNil)

	info, err := os.Stat(s.mockEtcEnvironment)
	c.Assert(err, IsNil)

	fileModTime := info.ModTime()

	// To make sure the times will defer if the file is newly written
	time.Sleep(100 * time.Millisecond)

	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"watchdog.runtime-timeout":  fmt.Sprintf("%ds", times[0]),
			"watchdog.shutdown-timeout": fmt.Sprintf("%ds", times[1]),
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.mockEtcEnvironment, testutil.FileEquals, content)

	info, err = os.Stat(s.mockEtcEnvironment)
	c.Assert(err, IsNil)
	c.Assert(info.ModTime(), Equals, fileModTime)
}

func (s *watchdogSuite) TestConfigureWatchdogRemovesIfEmpty(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// add canary to ensure we don't touch other files
	canary := filepath.Join(dirs.SnapSystemdConfDir, "05-canary.conf")
	err := ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	content := `[Manager]
RuntimeWatchdogSec=10
ShutdownWatchdogSec=20
`
	err = ioutil.WriteFile(s.mockEtcEnvironment, []byte(content), 0644)
	c.Assert(err, IsNil)

	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"watchdog.runtime-timeout":  0,
			"watchdog.shutdown-timeout": 0,
		},
	})
	c.Assert(err, IsNil)

	// ensure the file got deleted
	c.Check(osutil.FileExists(s.mockEtcEnvironment), Equals, false)
	// but the canary is still here
	c.Check(osutil.FileExists(canary), Equals, true)
}
