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

package configcore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.timezone"] = true
	// and register it as a external config
	config.RegisterExternalConfig("core", "system.timezone", getTimezoneFromSystemVC)
}

var validTimezone = regexp.MustCompile(`^[a-zA-Z0-9+_-]+(/[a-zA-Z0-9+_-]+)?(/[a-zA-Z0-9+_-]+)?$`).MatchString

func validateTimezoneSettings(tr ConfGetter) error {
	timezone, err := coreCfg(tr, "system.timezone")
	if err != nil {
		return err
	}
	if timezone == "" {
		return nil
	}
	if !validTimezone(timezone) {
		return fmt.Errorf("cannot set timezone %q: name not valid", timezone)
	}

	return nil
}

func handleTimezoneConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	timezone, err := coreCfg(tr, "system.timezone")
	if err != nil {
		return err
	}
	// nothing to do
	if timezone == "" {
		return nil
	}
	// runtime system
	if opts == nil {
		// see if anything has changed
		currentTimezone, err := getTimezoneFromSystem()
		if err != nil {
			return err
		}
		if timezone == currentTimezone {
			return nil
		}

		output, err := exec.Command("timedatectl", "set-timezone", timezone).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot set timezone: %v", osutil.OutputErr(output, err))
		}
	} else {
		// On the UC16/UC18/UC20 images the file /etc/hostname is a
		// symlink to /etc/writable/hostname. The /etc/hostname is
		// not part of the "writable-path" so we must set the file
		// in /etc/writable here for this to work.
		localtimePath := filepath.Join(opts.RootDir, "/etc/writable/localtime")
		if err := os.MkdirAll(filepath.Dir(localtimePath), 0755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Join("/usr/share/zoneinfo", timezone), localtimePath); err != nil {
			return err
		}
		timezonePath := filepath.Join(opts.RootDir, "/etc/writable/timezone")
		if err := osutil.AtomicWriteFile(timezonePath, []byte(timezone+"\n"), 0644, 0); err != nil {
			return fmt.Errorf("cannot write timezone: %v", err)
		}
	}

	return nil
}

func getTimezoneFromSystemVC(key string) (interface{}, error) {
	return getTimezoneFromSystem()
}

func getTimezoneFromSystem() (string, error) {
	// We cannot use "timedatectl show" here because it is only
	// available on UC20.
	//
	// Note that this code only runs on UbuntuCore systems which all
	// have /etc/writable/localtime
	link, err := os.Readlink(filepath.Join(dirs.GlobalRootDir, "/etc/writable/localtime"))
	// see localtime(5)
	// "If /etc/localtime is missing, the default "UTC" timezone is used."
	if os.IsNotExist(err) {
		return "UTC", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot get timezone: %v", err)
	}
	val := strings.TrimPrefix(link, "/usr/share/zoneinfo/")
	return val, nil
}
