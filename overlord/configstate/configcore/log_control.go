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
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.printk.console-loglevel"] = true
}

func validateLogControlSettings(tr config.ConfGetter) error {
	consoleLoglevelStr, err := coreCfg(tr, "system.printk.console-loglevel")
	if err != nil {
		return err
	}
	if consoleLoglevelStr != "" {
		if n, err := strconv.ParseUint(consoleLoglevelStr, 10, 8); err != nil || (n < 0 || n > 7) {
			return fmt.Errorf("loglevel must be a number between 0 and 7, not %q", consoleLoglevelStr)
		}
	}
	return nil
}

func handleLogControlConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	root := dirs.GlobalRootDir
	if opts != nil {
		root = opts.RootDir
	}
	cfgPath := filepath.Join(root, "/etc/sysctl.d/10-console-messages.conf")

	consoleLoglevelStr, err := coreCfg(tr, "system.printk.console-loglevel")
	if err != nil {
		return nil
	}

	if consoleLoglevelStr != "" {
		sysctl := fmt.Sprintf("kernel.printk=%s", consoleLoglevelStr)
		content := fmt.Sprintf("kernel.printk = %s 4 1 7\n", consoleLoglevelStr)
		if err := osutil.AtomicWriteFile(cfgPath, []byte(content), 0644, 0); err != nil {
			return err
		} else {
			if opts == nil {
				output, err := exec.Command("sysctl", "-w", sysctl).CombinedOutput()
				if err != nil {
					return osutil.OutputErr(output, err)
				}
			}
		}
	}

	return nil
}
