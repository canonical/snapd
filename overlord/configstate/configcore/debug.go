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

package configcore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

const (
	optionDebugSnapdLog            = "debug.snapd.log"
	optionDebugSystemdLogLevel     = "debug.systemd.log-level"
	coreOptionDebugSnapdLog        = "core." + optionDebugSnapdLog
	coreOptionDebugSystemdLogLevel = "core." + optionDebugSystemdLogLevel
)

var loggerSimpleSetup = logger.SimpleSetup

func init() {
	supportedConfigurations[coreOptionDebugSnapdLog] = true
	supportedConfigurations[coreOptionDebugSystemdLogLevel] = true
}

func validateDebugSnapdLogSetting(tr RunTransaction) error {
	return validateBoolFlag(tr, optionDebugSnapdLog)
}

func handleDebugSnapdLogConfiguration(tr RunTransaction, opts *fsOnlyContext) error {
	// Run only if the option changed to avoid extra filesystem access
	if !strutil.ListContains(tr.Changes(), coreOptionDebugSnapdLog) {
		return nil
	}

	debugLog, err := coreCfg(tr, optionDebugSnapdLog)
	if err != nil {
		return err
	}

	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}
	envDir := filepath.Join(dirs.SnapdStateDir(rootDir), "environment")

	snapdEnvPath := filepath.Join(envDir, "snapd.conf")

	var enableDebug bool
	switch debugLog {
	case "true":
		if err := os.Mkdir(envDir, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		if err := osutil.EnsureFileState(snapdEnvPath, &osutil.MemoryFileState{
			Content: []byte("SNAPD_DEBUG=1\n"),
			Mode:    os.FileMode(0644),
		}); err != nil {
			return err
		}
		enableDebug = true
	case "false", "":
		// We simply remove the env file as for the moment we use it
		// just for SNAPD_DEBUG. If we change that we will need to
		// locate the variable in the file and remove just that.
		if err := os.Remove(snapdEnvPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		enableDebug = false
	default:
		return fmt.Errorf("%s must be true of false, not: %q", optionDebugSnapdLog, debugLog)
	}

	// Enable/disable debug logging for current snapd instance
	if err := loggerSimpleSetup(&logger.LoggerOptions{ForceDebug: enableDebug}); err != nil {
		return err
	}

	return nil
}

func validateDebugSystemdLogLevelSetting(tr RunTransaction) error {
	value, err := coreCfg(tr, optionDebugSystemdLogLevel)
	if err != nil {
		return err
	}

	switch value {
	case "emerg", "alert", "crit", "err", "warning", "notice", "info", "debug",
		"0", "1", "2", "3", "45", "6", "7", "":
		// noop
	default:
		return fmt.Errorf("%q is not a valid value for %s (see systemd(1))",
			value, optionDebugSystemdLogLevel)
	}
	return nil
}

func handleDebugSystemdLogLevelConfiguration(tr RunTransaction, opts *fsOnlyContext) error {
	// Run only if the option changed to avoid extra filesystem
	// access / systemctl calls
	if !strutil.ListContains(tr.Changes(), coreOptionDebugSystemdLogLevel) {
		return nil
	}

	logLevel, err := coreCfg(tr, optionDebugSystemdLogLevel)
	if err != nil {
		return err
	}

	var sysd systemd.Systemd
	confDir := ""
	if opts != nil {
		confDir = dirs.SnapSystemdConfDirUnder(opts.RootDir)
		sysd = systemd.NewEmulationMode(opts.RootDir)
	} else {
		confDir = dirs.SnapSystemdConfDir
		sysd = systemd.New(systemd.SystemMode, &sysdLogger{})
	}
	confFile := filepath.Join(confDir, "20-debug_systemd_log-level.conf")

	if logLevel == "" {
		// On unsetting, remove the file.
		if err := os.Remove(confFile); err != nil {
			// Should be here, show a warning
			logger.Noticef("warning: while removing %q: %v", confFile, err)
		}
		// and set log level to the default
		logLevel = "info"
	} else {
		// Otherwise, write persistent configuration
		if err := os.MkdirAll(confDir, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		confData := fmt.Sprintf("[Manager]\nLogLevel=%s\n", logLevel)
		if err := osutil.EnsureFileState(confFile, &osutil.MemoryFileState{
			Content: []byte(confData),
			Mode:    os.FileMode(0644),
		}); err != nil {
			return err
		}
	}

	// Set log level for the current systemd instance
	return sysd.SetLogLevel(logLevel)
}
