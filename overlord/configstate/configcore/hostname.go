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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.hostname"] = true
}

// We are conservative here and follow hostname(7). The hostnamectl
// binary is more liberal but let's err on the side of caution for
// now.
var validHostname = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`).MatchString

func validateHostnameSettings(tr config.ConfGetter) error {
	hostname, err := coreCfg(tr, "system.hostname")
	if err != nil {
		return err
	}
	if hostname == "" {
		return nil
	}
	if !validHostname(hostname) {
		return fmt.Errorf("cannot set hostname %q: name not valid", hostname)
	}

	return nil
}

func handleHostnameConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	// TODO: convert to "virtual" configuration nodes once we have support
	// for this. The current code is not ideal because if one calls
	// `snap get system system.hostname` the answer can be ""
	// when not set via snap set.
	//
	// It will also override any hostname on the next `snap set` run
	// that was written not using `snap set system system.hostname`.
	hostname, err := coreCfg(tr, "system.hostname")
	if err != nil {
		return nil
	}
	// nothing to do
	if hostname == "" {
		return nil
	}
	// runtime system
	if opts == nil {
		output, err := exec.Command("hostnamectl", "set-hostname", hostname).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot set hostname: %v", osutil.OutputErr(output, err))
		}
	} else {
		// On the UC16/UC18/UC20 images the file /etc/hostname is a
		// symlink to /etc/writable/hostname. The /etc/hostname is
		// not part of the "writable-path" so we must set the file
		// in /etc/writable here for this to work.
		hostnamePath := filepath.Join(opts.RootDir, "/etc/writable/hostname")
		if err := os.MkdirAll(filepath.Dir(hostnamePath), 0755); err != nil {
			return err
		}
		if err := osutil.AtomicWriteFile(hostnamePath, []byte(hostname+"\n"), 0644, 0); err != nil {
			return fmt.Errorf("cannot write hostname: %v", err)
		}
	}

	return nil
}
