// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type debugSuite struct {
	configcoreSuite

	snapdEnvPath       string
	systemdLogConfPath string
}

var _ = Suite(&debugSuite{})

func (s *debugSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	envDir := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "environment")
	s.snapdEnvPath = filepath.Join(envDir, "snapd.conf")
	s.systemdLogConfPath = filepath.Join(dirs.SnapSystemdConfDir,
		"20-debug_systemd_log-level.conf")

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/etc/environment"), nil, 0644)
	c.Assert(err, IsNil)
}

func (s *debugSuite) testConfigureDebugSnapdLogGoodVals(c *C, valChanges bool) {
	var loggerOpts *logger.LoggerOptions
	r := configcore.MockLoggerSimpleSetup(func(opts *logger.LoggerOptions) error {
		loggerOpts = opts
		return nil
	})
	defer r()

	for _, val := range []string{"true", "false", ""} {
		prevVal := val
		if valChanges {
			if val == "true" {
				prevVal = "false"
			} else {
				prevVal = "true"
			}
		}
		err := configcore.Run(coreDev, &mockConf{
			state:   s.state,
			conf:    map[string]interface{}{"debug.snapd.log": prevVal},
			changes: map[string]interface{}{"debug.snapd.log": val},
		})

		c.Assert(err, IsNil)

		switch val {
		case "true":
			c.Check(s.snapdEnvPath, testutil.FileEquals, "SNAPD_DEBUG=1\n")
			c.Check(loggerOpts, DeepEquals, &logger.LoggerOptions{ForceDebug: true})
		case "false", "":
			c.Check(s.snapdEnvPath, testutil.FileAbsent)
			c.Check(loggerOpts, DeepEquals, &logger.LoggerOptions{ForceDebug: false})
		}
	}
}

func (s *debugSuite) TestConfigureDebugSnapdLogGoodVals(c *C) {
	valChanges := true
	s.testConfigureDebugSnapdLogGoodVals(c, valChanges)
}

func (s *debugSuite) TestConfigureDebugSnapdLogGoodValsNoChange(c *C) {
	valChanges := false
	s.testConfigureDebugSnapdLogGoodVals(c, valChanges)
}

func (s *debugSuite) TestConfigureDebugSnapdLogBadVals(c *C) {
	r := configcore.MockLoggerSimpleSetup(func(opts *logger.LoggerOptions) error {
		c.Error("loggerSimpleSetup should not have been called")
		return nil
	})
	defer r()

	for _, val := range []string{"1", "foo"} {
		err := configcore.Run(coreDev, &mockConf{
			state:   s.state,
			conf:    map[string]interface{}{"debug.snapd.log": ""},
			changes: map[string]interface{}{"debug.snapd.log": val},
		})
		c.Assert(err, ErrorMatches,
			"debug.snapd.log can only be set to 'true' or 'false'")

		c.Check(s.snapdEnvPath, testutil.FileAbsent)
	}
}

func (s *debugSuite) TestConfigureSystemdLogLevelGoodVals(c *C) {
	var systemctlArgs []string
	numCalls := 0
	systemctlMock := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = args
		numCalls++
		return nil, nil
	})
	defer systemctlMock()

	validVals := []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug",
		"0", "1", "2", "3", "45", "6", "7", ""}
	for _, val := range validVals {
		conf := &mockConf{
			state:   s.state,
			conf:    map[string]interface{}{"debug.systemd.log-level": ""},
			changes: map[string]interface{}{"debug.systemd.log-level": val},
		}
		err := configcore.Run(coreDev, conf)

		c.Assert(err, IsNil)

		if val == "" {
			// Unsetting should remove the file and set log level
			// to the default
			c.Check(s.systemdLogConfPath, testutil.FileAbsent)
			val = "info"
		} else {
			confData := fmt.Sprintf("[Manager]\nLogLevel=%s\n", val)
			c.Check(s.systemdLogConfPath, testutil.FileEquals, confData)
		}

		// Check call to systemctl
		c.Check(systemctlArgs, DeepEquals, []string{"log-level", val})
	}
}

func (s *debugSuite) TestConfigureSystemdLogLevelBadVals(c *C) {
	systemctlMock := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		c.Error("systemctl should not be called in this test")
		return nil, nil
	})
	defer systemctlMock()

	for _, val := range []string{"foo", "8", "-1"} {
		conf := &mockConf{
			state:   s.state,
			conf:    map[string]interface{}{"debug.systemd.log-level": "info"},
			changes: map[string]interface{}{"debug.systemd.log-level": val},
		}
		err := configcore.Run(coreDev, conf)

		c.Check(err, ErrorMatches,
			fmt.Sprintf(`%q is not a valid value for debug\.systemd\.log\-level.*`, val))
		c.Check(s.systemdLogConfPath, testutil.FileAbsent)
	}
}

func (s *debugSuite) TestConfigureSystemdLogLevelOldSystemd(c *C) {
	var systemctlArgs []string
	systemctlMock := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = args
		return nil, errors.New("old systemd")
	})
	defer systemctlMock()

	sysdAnalyzeCmd := testutil.MockCommand(c, "systemd-analyze", "")
	defer sysdAnalyzeCmd.Restore()

	val := "debug"
	err := configcore.Run(coreDev, &mockConf{
		state:   s.state,
		conf:    map[string]interface{}{"debug.systemd.log-level": ""},
		changes: map[string]interface{}{"debug.systemd.log-level": val},
	})
	c.Assert(err, IsNil)

	confData := fmt.Sprintf("[Manager]\nLogLevel=%s\n", val)
	c.Check(s.systemdLogConfPath, testutil.FileEquals, confData)

	// Check calls
	c.Check(systemctlArgs, DeepEquals, []string{"log-level", val})
	c.Check(sysdAnalyzeCmd.Calls(), DeepEquals, [][]string{{"systemd-analyze", "set-log-level", val}})
}
