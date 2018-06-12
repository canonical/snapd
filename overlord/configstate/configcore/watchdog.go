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

package configcore

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
)

var watchdogConfigKeys = map[string]bool{
	"RuntimeWatchdogSec":  true,
	"ShutdownWatchdogSec": true,
}

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.watchdog.runtime-timeout"] = true
	supportedConfigurations["core.watchdog.shutdown-timeout"] = true
}

func watchdogConfEnvironment() string {
	return filepath.Join(dirs.SnapSystemdConfDir, "10-ubuntu-core-watchdog.conf")
}

func updateWatchdogConfig(config map[string]uint) error {
	path := watchdogConfEnvironment()

	configStr := []string{}
	for k, v := range config {
		if v > 0 {
			configStr = append(configStr, fmt.Sprintf("%s=%d\n", k, v))
		}
	}
	if len(configStr) == 0 {
		// No config, remove file
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	// We order the variables to have predictable output
	sort.Strings(configStr)
	content := "[Manager]\n" + strings.Join(configStr, "")

	// Compare with current file, go on only if different content
	if cfgFile, err := os.Open(path); err == nil {
		defer cfgFile.Close()

		current, err := ioutil.ReadAll(cfgFile)
		if err != nil {
			return err
		}

		if content == string(current) {
			return nil
		}
	}

	if err := os.MkdirAll(dirs.SnapSystemdConfDir, 0755); err != nil {
		return err
	}

	return ioutil.WriteFile(path, []byte(content), 0644)
}

func handleWatchdogConfiguration(tr Conf) error {
	config := map[string]uint{}

	for _, key := range []string{"runtime-timeout", "shutdown-timeout"} {
		output, err := coreCfg(tr, "watchdog."+key)
		if err != nil {
			return err
		}
		secs, err := getSystemdConfSeconds(output)
		if err != nil {
			return fmt.Errorf("cannot set timer to %q: %v", output, err)
		}
		switch key {
		case "runtime-timeout":
			config["RuntimeWatchdogSec"] = secs
		case "shutdown-timeout":
			config["ShutdownWatchdogSec"] = secs
		}
	}

	if err := updateWatchdogConfig(config); err != nil {
		return err
	}

	return nil
}

func getSystemdConfSeconds(timeStr string) (uint, error) {
	if timeStr == "" {
		return 0, nil
	}

	dur, err := time.ParseDuration(timeStr)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q: %v", timeStr, err)
	}
	if dur < 0 {
		return 0, fmt.Errorf("cannot use negative duration %q: %v", timeStr, err)
	}

	return uint(dur.Seconds()), nil
}

func validateWatchdogOptions(tr Conf) error {
	for _, key := range []string{"runtime-timeout", "shutdown-timeout"} {
		option, err := coreCfg(tr, "watchdog."+key)
		if err != nil {
			return err
		}
		if _, err = getSystemdConfSeconds(option); err != nil {
			return err
		}
	}

	return nil
}
