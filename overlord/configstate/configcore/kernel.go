// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

const (
	optionKernelCmdlineAppend              = "system.kernel.cmdline-append"
	optionKernelDangerousCmdlineAppend     = "system.kernel.dangerous-cmdline-append"
	coreOptionKernelCmdlineAppend          = "core." + optionKernelCmdlineAppend
	coreOptionKernelDangerousCmdlineAppend = "core." + optionKernelDangerousCmdlineAppend
)

func init() {
	supportedConfigurations[coreOptionKernelCmdlineAppend] = true
	supportedConfigurations[coreOptionKernelDangerousCmdlineAppend] = true
}

func changedKernelConfigs(c RunTransaction) []string {
	changed := []string{}
	for _, name := range c.Changes() {
		// Note that we cannot just check the prefix as we have
		// system.kernel.* options also defined in sysctl.go.
		if name == coreOptionKernelCmdlineAppend || name == coreOptionKernelDangerousCmdlineAppend {
			nameWithoutSnap := strings.SplitN(name, ".", 2)[1]
			changed = append(changed, nameWithoutSnap)
		}
	}
	return changed
}

func validateCmdlineParamsAreAllowed(st *state.State, devCtx snapstate.DeviceContext, cmdline string) error {
	if devCtx.IsClassicBoot() {
		return fmt.Errorf("changing the kernel command line is not supported on a classic system")
	}

	gd, err := devicestate.CurrentGadgetData(st, devCtx)
	if err != nil {
		return err
	}

	logger.Debugf("gadget data read from %s", gd.RootDir)

	if _, forbidden := gadget.FilterKernelCmdline(cmdline, gd.Info.KernelCmdline.Allow); forbidden != "" {
		return fmt.Errorf("%q is not allowed in the kernel command line by the gadget", forbidden)
	}

	return nil
}

func validateCmdlineAppend(c RunTransaction) error {
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

	for _, opt := range changed {
		cmdAppend, err := coreCfg(c, opt)
		if err != nil {
			return err
		}

		logger.Debugf("kernel option: validating %s=%q", opt, cmdAppend)
		if opt == optionKernelCmdlineAppend {
			// check against allowed values from gadget
			if err := validateCmdlineParamsAreAllowed(c.State(), devCtx, cmdAppend); err != nil {
				return err
			}
		} else { // OptionKernelDangerousCmdlineAppend
			if devCtx.Model().Grade() != asserts.ModelDangerous {
				// TODO we should return an error if this is an API call
				// and do nothing if setting defaults (so gadget can be
				// reused with different models).
				logger.Noticef("WARNING: %s ignored as this is not a dangerous model", opt)
			}
		}
	}

	return nil
}

func createApplyCmdlineChange(c RunTransaction, kernelOpts []string) (*state.Change, error) {
	st := c.State()
	st.Lock()
	defer st.Unlock()

	// error out if some other change is touching the kernel command line
	if err := snapstate.CheckUpdateKernelCommandLineConflict(st, ""); err != nil {
		return nil, err
	}

	// precalculate task arguments, so we do not need to destroy change/task
	// if there is an error
	args := []struct {
		name    string
		cmdline string
	}{}
	for _, opt := range kernelOpts {
		cmdline, err := coreCfg(c, opt)
		if err != nil {
			return nil, err
		}
		// opt must match system.kernel.{dangerous-,}cmdline-append (so the
		// slice must have size 3 and next expression should not fail).
		name := strings.Split(opt, ".")[2]
		args = append(args, struct {
			name    string
			cmdline string
		}{
			name: name, cmdline: cmdline,
		})
	}

	// We need to create a new change that will change the kernel
	// command line and wait for it to finish, otherwise we cannot
	// wait on the changes to happen.
	// TODO fix this in the future.
	cmdlineChg := st.NewChange("apply-cmdline-append",
		i18n.G("Update kernel command line due to change in system configuration"))
	// Add task to the new change to set the new kernel command line
	t := st.NewTask("update-gadget-cmdline",
		"Update kernel command line from system configuration")
	// Pass options to the task (changes in the options are not
	// committed yet so the task cannot simply get them from the
	// configuration)
	for _, arg := range args {
		t.Set(arg.name, arg.cmdline)
	}

	cmdlineChg.AddTask(t)
	st.EnsureBefore(0)

	return cmdlineChg, nil
}

func isDangerousModel(st *state.State) (bool, error) {
	st.Lock()
	defer st.Unlock()
	devCtx, err := devicestate.DeviceCtx(st, nil, nil)
	if err != nil {
		return false, err
	}

	return devCtx.Model().Grade() == asserts.ModelDangerous, nil
}

func handleCmdlineAppend(c RunTransaction, opts *fsOnlyContext) error {
	kernelOpts := changedKernelConfigs(c)
	if len(kernelOpts) == 0 {
		return nil
	}
	logger.Debugf("handling %v", kernelOpts)

	st := c.State()
	isDangModel, err := isDangerousModel(st)
	if err != nil {
		return err
	}
	// nothing to do if non-dangerous model and the only option set is
	// the dangerous one, we simply return with success
	if !isDangModel && len(kernelOpts) == 1 && kernelOpts[0] == optionKernelDangerousCmdlineAppend {
		return nil
	}

	cmdlineChg, err := createApplyCmdlineChange(c, kernelOpts)
	if err != nil {
		return err
	}

	select {
	case <-cmdlineChg.Ready():
		st.Lock()
		defer st.Unlock()
		return cmdlineChg.Err()
	case <-time.After(5 * time.Minute):
		// Resealing may take a bit of time so we try to stay on the safe side
		// with a 5 minutes timeout.
		return fmt.Errorf("%s is taking too long", cmdlineChg.Kind())
	}
}
