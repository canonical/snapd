// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

const (
	OptionKernelCmdlineAppend              = "system.kernel.cmdline-append"
	OptionKernelDangerousCmdlineAppend     = "system.kernel.dangerous-cmdline-append"
	CoreOptionKernelCmdlineAppend          = "core.system.kernel.cmdline-append"
	CoreOptionKernelDangerousCmdlineAppend = "core.system.kernel.dangerous-cmdline-append"
)

func init() {
	supportedConfigurations[CoreOptionKernelCmdlineAppend] = true
	supportedConfigurations[CoreOptionKernelDangerousCmdlineAppend] = true
}

func changedKernelConfigs(c RunTransaction) []string {
	changed := []string{}
	for _, name := range c.Changes() {
		// Note that we cannot just check the prefix as we have
		// system.kernel.* options also defined in sysctl.go.
		if name == CoreOptionKernelCmdlineAppend || name == CoreOptionKernelDangerousCmdlineAppend {
			changed = append(changed, name)
		}
	}
	return changed
}

func validateParamsAreAllowed(st *state.State, devCtx snapstate.DeviceContext, params []string) error {
	// gd, err := devicestate.CurrentGadgetInfo(st, devCtx)
	// if err != nil {
	// 	return err
	// }
	// logger.Debugf("gadget data read from %s", gd.RootDir)
	// TODO use gadgetdata to check against allowed values

	return nil
}

func validateCmdlineExtra(c RunTransaction) error {
	changed := changedKernelConfigs(c)
	if len(changed) == 0 {
		return nil
	}

	st := c.State()
	st.Lock()
	defer st.Unlock()
	devCtx, err := devicestate.DeviceCtx(st, nil, nil)
	if err != nil {
		return err
	}

	for _, opt := range changedKernelConfigs(c) {
		optWithoutSnap := strings.SplitN(opt, ".", 2)[1]
		cmdExtra, err := coreCfg(c, optWithoutSnap)
		if err != nil {
			return err
		}

		logger.Debugf("validating %s=%q", opt, cmdExtra)
		params, err := osutil.KernelCommandLineSplit(cmdExtra)
		if err != nil {
			return err
		}
		if optWithoutSnap == OptionKernelCmdlineAppend {
			// check against allowed values from gadget
			if err := validateParamsAreAllowed(c.State(), devCtx, params); err != nil {
				return fmt.Errorf("while validating params: %v", err)
			}
		} else { // OptionKernelDangerousCmdlineAppend
			if devCtx.Model().Grade() != asserts.ModelDangerous {
				return fmt.Errorf("cannot use %s for non-dangerous model",
					optWithoutSnap)
			}
		}
	}

	return nil
}

func handleCmdlineExtra(c RunTransaction, opts *fsOnlyContext) error {
	kernelOpts := changedKernelConfigs(c)
	if len(kernelOpts) == 0 {
		return nil
	}
	logger.Debugf("handling %v", kernelOpts)

	st := c.State()
	st.Lock()

	// We need to commit now the changes, otherwise they won't be
	// seen by the change we are about to create - as we wait for
	// the new change to finish, the end of the hook which is
	// committing these changes will not have been called yet.
	// TODO how to fix this in a cleaner way?
	c.Commit()

	// We need to create a new change that will change the kernel
	// command line and wait for it to finish, otherwise we cannot
	// wait on the changes to happen.
	// TODO fix this in the future.
	cmdlineChg := st.NewChange("apply-extra-cmdline",
		i18n.G("Updating command line due to change in system configuration"))
	// Add task to the new change to set the new kernel command line
	t := st.NewTask("update-gadget-cmdline",
		"Updating command line due to change in system configuration")
	t.Set("system-option", true)
	cmdlineChg.AddTask(t)
	st.EnsureBefore(0)

	st.Unlock()

	select {
	case <-cmdlineChg.Ready():
		st.Lock()
		defer st.Unlock()
		return cmdlineChg.Err()
	case <-time.After(2 * config.ConfigureHookTimeout() / 3):
		return fmt.Errorf("%s is taking too long", cmdlineChg.Kind())
	}
}
