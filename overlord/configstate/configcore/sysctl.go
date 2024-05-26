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
	"os"
	"path/filepath"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

// see https://www.kernel.org/doc/Documentation/sysctl/kernel.txt
// The idea is that options of the form system.kernel. should
// match the (/proc/)sys/kernel hierarchy etc.

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.kernel.printk.console-loglevel"] = true
}

const (
	sysctlConfsDir  = "/etc/sysctl.d"
	snapdSysctlConf = "99-snapd.conf"
)

// these are the sysctl parameters prefixes we handle
var sysctlPrefixes = []string{"kernel.printk"}

func validateSysctlOptions(tr ConfGetter) error {
	consoleLoglevelStr := mylog.Check2(coreCfg(tr, "system.kernel.printk.console-loglevel"))

	if consoleLoglevelStr != "" {
		if n := mylog.Check2(strconv.ParseUint(consoleLoglevelStr, 10, 8)); err != nil || (n < 0 || n > 7) {
			return fmt.Errorf("console-loglevel must be a number between 0 and 7, not: %s", consoleLoglevelStr)
		}
	}
	return nil
}

func handleSysctlConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	root := dirs.GlobalRootDir
	if opts != nil {
		root = opts.RootDir
	}

	consoleLoglevelStr := mylog.Check2(coreCfg(tr, "system.kernel.printk.console-loglevel"))

	content := bytes.NewBuffer(nil)
	if consoleLoglevelStr != "" {
		content.WriteString(fmt.Sprintf("kernel.printk = %s 4 1 7\n", consoleLoglevelStr))
	} else {
		// Don't write values to content so that the config
		// file gets removed and console-loglevel gets reset
		// to default value from 10-console-messages.conf

		// TODO: this logic will need more non-obvious work to support
		// kernel parameters that don't have already on-disk defaults.
	}
	dirContent := map[string]osutil.FileState{}
	if content.Len() > 0 {
		dirContent[snapdSysctlConf] = &osutil.MemoryFileState{
			Content: content.Bytes(),
			Mode:    0644,
		}
	}

	dir := filepath.Join(root, sysctlConfsDir)
	if opts != nil {
		mylog.Check(os.MkdirAll(dir, 0755))
	}

	// write the new config
	glob := snapdSysctlConf
	changed, removed := mylog.Check3(osutil.EnsureDirState(dir, glob, dirContent))

	if opts == nil {
		if len(changed) > 0 || len(removed) > 0 {
			// apply our configuration or default configuration
			// via systemd-sysctl for the relevant prefixes
			return systemd.Sysctl(sysctlPrefixes)
		}
	}

	return nil
}
