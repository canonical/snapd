// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2025 Canonical Ltd
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

package devicestate

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

type extraSnapdKernelCmdlineArg string

const (
	extraSnapdKernelCmdlineArgXKB extraSnapdKernelCmdlineArg = "snapd.xkb"
)

func (arg extraSnapdKernelCmdlineArg) validate(val string) error {
	switch arg {
	case extraSnapdKernelCmdlineArgXKB:
		// TODO:FDEM: add arg-specific validation?
	default:
		return fmt.Errorf("internal error: unexpected extra snapd kcmdline arg: %q", arg)
	}
	return nil
}

const extraSnapdKernelCmdlineArgsKey string = "extra-snapd-kcmdline-args"

// setExtraSnapdKernelCommandLineArg updates the specified extra snap kernel
// argument. An empty string removes the specified argument if it already
// existed. If the passed value is different from the existing argument
// value a change is triggered to update the kernel command line arguments,
// otherwise it is a no-op.
func setExtraSnapdKernelCommandLineArg(st *state.State, name extraSnapdKernelCmdlineArg, val string) (updated bool, err error) {
	if err := name.validate(val); err != nil {
		return false, err
	}

	var seeded bool
	err = st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	if !seeded {
		return false, fmt.Errorf("cannot set extra snapd kernel cmdline arguments until fully seeded")
	}

	var args map[extraSnapdKernelCmdlineArg]string
	if err := st.Get(extraSnapdKernelCmdlineArgsKey, &args); err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	oldVal := args[name]
	if val == oldVal {
		// Nothing changed, no-op.
		return false, nil
	}

	if args == nil {
		args = make(map[extraSnapdKernelCmdlineArg]string, 1)
	}

	summary := fmt.Sprintf("Set extra snapd kernel cmdline argument %s", name)
	if val == "" {
		summary = fmt.Sprintf("Unset extra snapd kernel cmdline argument %s", name)
		delete(args, name)
	} else {
		args[name] = val
	}
	st.Set(extraSnapdKernelCmdlineArgsKey, args)

	logger.Debugf("Setting extra snapd kernel cmdline argument %s=%q", name, val)

	// Value changed, Trigger a kernel cmdline update change.
	t := st.NewTask("update-managed-boot-config", summary)
	t.Set("no-restart", true)

	chg := st.NewChange("set-extra-snapd-kcmdline-arg", summary)
	chg.AddTask(t)
	return true, nil
}

// kernelCommandLineAppendArgsFromSnapd returns extra arguments that snapd
// might set internally using setExtraSnapdKernelCommandLineArg.
func kernelCommandLineAppendArgsFromSnapd(st *state.State) (string, error) {
	var args map[extraSnapdKernelCmdlineArg]string
	if err := st.Get(extraSnapdKernelCmdlineArgsKey, &args); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if len(args) == 0 {
		return "", nil
	}

	sortedArgs := make([]string, 0, len(args))
	for name, value := range args {
		// Values are quoted to protect against spaces.
		sortedArgs = append(sortedArgs, fmt.Sprintf("%s=%q", name, value))
	}
	sort.Strings(sortedArgs)
	return strings.Join(sortedArgs, " "), nil
}

// kernelCommandLineAppendArgsFromConfig returns extra arguments that we
// want to append to the kernel command line, searching first by looking
// at the task, and if not found, looking at the current configuration
// options. One thing or the other could happen depending on whether
// this is a task created when setting a kernel option or by gadget
// installation.
func kernelCommandLineAppendArgsFromConfig(tsk *state.Task, tr *config.Transaction,
	taskParam string) (string, error) {

	var value string
	err := tsk.Get(taskParam, &value)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, state.ErrNoState) {
		return "", err
	}

	var option string
	switch taskParam {
	case "cmdline-append":
		option = "system.kernel.cmdline-append"
	case "dangerous-cmdline-append":
		option = "system.kernel.dangerous-cmdline-append"
	default:
		return "", fmt.Errorf("internal error, unexpected task parameter %q", taskParam)
	}
	if err := tr.Get("core", option, &value); err != nil && !config.IsNoOption(err) {
		return "", err
	}

	return value, nil
}

func buildAppendedKernelCommandLine(t *state.Task, gd *gadget.GadgetData, deviceCtx snapstate.DeviceContext) (string, error) {
	tr := config.NewTransaction(t.State())
	rawCmdlineAppend, err := kernelCommandLineAppendArgsFromConfig(t, tr, "cmdline-append")
	if err != nil {
		return "", err
	}
	// Validation against allow list has already happened in
	// configcore, but the gadget might have changed, so we check
	// again and filter any unallowed argument.
	cmdlineAppend, forbidden := gadget.FilterKernelCmdline(rawCmdlineAppend, gd.Info.KernelCmdline.Allow)
	if forbidden != "" {
		warnMsg := fmt.Sprintf("%q is not allowed by the gadget and has been filtered out from the kernel command line", forbidden)
		logger.Notice(warnMsg)
		t.Logf(warnMsg)
	}

	// Dangerous extra cmdline only considered for dangerous models
	if deviceCtx.Model().Grade() == asserts.ModelDangerous {
		cmdlineAppendDanger, err := kernelCommandLineAppendArgsFromConfig(t, tr,
			"dangerous-cmdline-append")
		if err != nil {
			return "", err
		}
		cmdlineAppend = strutil.JoinNonEmpty(
			[]string{cmdlineAppend, cmdlineAppendDanger}, " ")
	}

	extraSnapdCmdlineAppend, err := kernelCommandLineAppendArgsFromSnapd(t.State())
	if err != nil {
		return "", err
	}
	cmdlineAppend = strutil.JoinNonEmpty(
		[]string{extraSnapdCmdlineAppend, cmdlineAppend}, " ")

	logger.Debugf("appended kernel command line part is %q", cmdlineAppend)

	return cmdlineAppend, nil
}
