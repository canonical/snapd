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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.hostname"] = true
	config.RegisterExternalConfig("core", "system.hostname", getHostnameFromSystemHelper)
}

// The hostname can also be set via hostnamectl so we cannot be more strict
// than hostnamectl itself.
// See: systemd/src/basic/hostname-util.c:ostname_is_valid
var validHostnameRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,62}(\.[a-zA-Z0-9-]{1,63})*$`).MatchString

// Note that HOST_NAME_MAX is 64 on Linux, but DNS allows domain names
// up to 255 characters
const HOST_NAME_MAX = 64

func validateHostname(hostname string) error {
	validHostname := validHostnameRegexp(hostname)
	if !validHostname {
		return fmt.Errorf("cannot set hostname %q: name not valid", hostname)
	}
	if len(hostname) > HOST_NAME_MAX {
		return fmt.Errorf("cannot set hostname %q: name too long", hostname)
	}

	return nil
}

func handleHostnameConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	hostname := mylog.Check2(coreCfg(tr, "system.hostname"))

	// nothing to do
	if hostname == "" {
		return nil
	}
	// runtime system
	if opts == nil {
		currentHostname := mylog.Check2(getHostnameFromSystem())

		if hostname == currentHostname {
			return nil
		}
		output := mylog.Check2(exec.Command("hostnamectl", "set-hostname", hostname).CombinedOutput())

	} else {
		mylog.Check(validateHostname(hostname))

		// On the UC16/UC18/UC20 images the file /etc/hostname is a
		// symlink to /etc/writable/hostname. The /etc/hostname is
		// not part of the "writable-path" so we must set the file
		// in /etc/writable here for this to work.
		hostnamePath := filepath.Join(opts.RootDir, "/etc/writable/hostname")
		mylog.Check(os.MkdirAll(filepath.Dir(hostnamePath), 0755))
		mylog.Check(osutil.AtomicWriteFile(hostnamePath, []byte(hostname+"\n"), 0644, 0))

	}

	return nil
}

func getHostnameFromSystemHelper(key string) (interface{}, error) {
	// XXX: should we error for subkeys here?
	return getHostnameFromSystem()
}

func getHostnameFromSystem() (string, error) {
	// try pretty hostname first
	output, stderr := mylog.Check3(osutil.RunSplitOutput("hostnamectl", "status", "--pretty"))

	prettyHostname := strings.TrimSpace(string(output))
	if len(prettyHostname) > 0 {
		return prettyHostname, nil
	}

	// then static hostname
	output, stderr = mylog.Check3(osutil.RunSplitOutput("hostnamectl", "status", "--static"))

	return strings.TrimSpace(string(output)), nil
}
