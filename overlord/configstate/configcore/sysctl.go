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
	"bytes"
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
	supportedConfigurations["core.system.kernel.printk.console-loglevel"] = true
}

func validateSysctlOptions(tr config.ConfGetter) error {
	consoleLoglevelStr, err := coreCfg(tr, "system.kernel.printk.console-loglevel")
	if err != nil {
		return err
	}
	if consoleLoglevelStr != "" {
		if n, err := strconv.ParseUint(consoleLoglevelStr, 10, 8); err != nil || (n < 0 || n > 7) {
			return fmt.Errorf("console-loglevel must be a number between 0 and 7, not %q", consoleLoglevelStr)
		}
	}
	return nil
}

func handleSysctlConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	root := dirs.GlobalRootDir
	if opts != nil {
		root = opts.RootDir
	}
	dir := filepath.Join(root, "/etc/sysctl.d")
	name := "99-snapd.conf"
	content := bytes.NewBuffer(nil)

	consoleLoglevelStr, err := coreCfg(tr, "system.kernel.printk.console-loglevel")
	if err != nil {
		return nil
	}

	var sysctlConf string
	if consoleLoglevelStr != "" {
		content.WriteString(fmt.Sprintf("kernel.printk = %s 4 1 7\n", consoleLoglevelStr))
		sysctlConf = filepath.Join(dir, name)
	} else {
		// Don't write values to content so that the file setting this option gets removed.
		// Reset console-loglevel to default value.
		sysctlConf = filepath.Join(dir, "10-console-messages.conf")
	}
	dirContent := map[string]osutil.FileState{}
	if content.Len() > 0 {
		dirContent[name] = &osutil.MemoryFileState{
			Content: content.Bytes(),
			Mode:    0644,
		}
	}

	// write the new config
	glob := name
	changed, removed, err := osutil.EnsureDirState(dir, glob, dirContent)
	if err != nil {
		return err
	}

	if opts == nil {
		if len(changed) > 0 || len(removed) > 0 {
			if output, err := exec.Command("sysctl", "-p", sysctlConf).CombinedOutput(); err != nil {
				return osutil.OutputErr(output, err)
			}
		}
	}

	return nil
}
