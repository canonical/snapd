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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.watchdog.runtime-timeout"] = true
	supportedConfigurations["core.watchdog.shutdown-timeout"] = true
}

func updateWatchdogConfig(config map[string]uint, opts *fsOnlyContext) error {
	var sysd systemd.Systemd

	dir := dirs.SnapSystemdConfDir
	if opts != nil {
		dir = dirs.SnapSystemdConfDirUnder(opts.RootDir)
	} else {
		sysd = systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.SystemMode, &sysdLogger{})
	}

	name := "10-snapd-watchdog.conf"
	dirContent := make(map[string]osutil.FileState, 1)

	configStr := []string{}
	for k, v := range config {
		if v > 0 {
			configStr = append(configStr, fmt.Sprintf("%s=%d\n", k, v))
		}
	}
	if len(configStr) > 0 {
		mylog.Check(os.MkdirAll(dir, 0755))

		// We order the variables to have predictable output
		sort.Strings(configStr)
		content := "[Manager]\n" + strings.Join(configStr, "")
		dirContent[name] = &osutil.MemoryFileState{
			Content: []byte(content),
			Mode:    0644,
		}
	}

	glob := name
	changed, removed := mylog.Check3(osutil.EnsureDirState(dir, glob, dirContent))

	// something was changed, reexec systemd manager
	if sysd != nil && (len(changed) > 0 || len(removed) > 0) {
		return sysd.DaemonReexec()
	}

	return nil
}

func handleWatchdogConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	config := map[string]uint{}

	for _, key := range []string{"runtime-timeout", "shutdown-timeout"} {
		output := mylog.Check2(coreCfg(tr, "watchdog."+key))

		secs := mylog.Check2(getSystemdConfSeconds(output))

		switch key {
		case "runtime-timeout":
			config["RuntimeWatchdogSec"] = secs
		case "shutdown-timeout":
			config["ShutdownWatchdogSec"] = secs
		}
	}
	mylog.Check(updateWatchdogConfig(config, opts))

	return nil
}

func getSystemdConfSeconds(timeStr string) (uint, error) {
	if timeStr == "" {
		return 0, nil
	}

	dur := mylog.Check2(time.ParseDuration(timeStr))

	if dur < 0 {
		return 0, fmt.Errorf("cannot use negative duration %q: %v", timeStr, err)
	}

	return uint(dur.Seconds()), nil
}

func validateWatchdogOptions(tr ConfGetter) error {
	for _, key := range []string{"runtime-timeout", "shutdown-timeout"} {
		option := mylog.Check2(coreCfg(tr, "watchdog."+key))
		mylog.Check2(getSystemdConfSeconds(option))

	}

	return nil
}
