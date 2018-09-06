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
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.watchdog.runtime-timeout"] = true
	supportedConfigurations["core.watchdog.shutdown-timeout"] = true
}

func updateWatchdogConfig(config map[string]uint) error {
	dir := dirs.SnapSystemdConfDir
	name := "10-snapd-watchdog.conf"
	dirContent := make(map[string]*osutil.FileState, 1)

	configStr := []string{}
	for k, v := range config {
		if v > 0 {
			configStr = append(configStr, fmt.Sprintf("%s=%d\n", k, v))
		}
	}
	if len(configStr) > 0 {
		// We order the variables to have predictable output
		sort.Strings(configStr)
		content := "[Manager]\n" + strings.Join(configStr, "")
		dirContent[name] = &osutil.FileState{
			Content: []byte(content),
			Mode:    0644,
		}
	}

	glob := name
	_, _, err := osutil.EnsureDirState(dir, glob, dirContent)
	return err
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
