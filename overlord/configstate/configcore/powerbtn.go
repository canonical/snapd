// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	supportedConfigurations["core.system.power-key-action"] = true
}

func powerBtnCfg(opts *fsOnlyContext) string {
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}
	return filepath.Join(rootDir, "/etc/systemd/logind.conf.d/00-snap-core.conf")
}

// switchHandlePowerKey changes the behavior when the power key is pressed
func switchHandlePowerKey(action string, opts *fsOnlyContext) error {
	validActions := map[string]bool{
		"ignore":       true,
		"poweroff":     true,
		"reboot":       true,
		"halt":         true,
		"kexec":        true,
		"suspend":      true,
		"hibernate":    true,
		"hybrid-sleep": true,
		"lock":         true,
	}

	cfgDir := filepath.Dir(powerBtnCfg(opts))
	if !osutil.IsDirectory(cfgDir) {
		mylog.Check(os.MkdirAll(cfgDir, 0755))
	}
	if !validActions[action] {
		return fmt.Errorf("invalid action %q supplied for system.power-key-action option", action)
	}

	content := fmt.Sprintf(`[Login]
HandlePowerKey=%s
`, action)
	return osutil.AtomicWriteFile(powerBtnCfg(opts), []byte(content), 0644, 0)
}

func handlePowerButtonConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	output := mylog.Check2(coreCfg(tr, "system.power-key-action"))

	if output == "" {
		if mylog.Check(os.Remove(powerBtnCfg(opts))); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		mylog.Check(switchHandlePowerKey(output, opts))
	}
	return nil
}
