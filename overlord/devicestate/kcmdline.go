// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2026 Canonical Ltd
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

type extraSnapdKernelCommandLineFragmentID string

const (
	extraSnapdKernelCommandLineFragmentXKB extraSnapdKernelCommandLineFragmentID = "xkb"
)

func (id extraSnapdKernelCommandLineFragmentID) validate(val string) error {
	switch id {
	case extraSnapdKernelCommandLineFragmentXKB:
		// TODO:FDEM: add kind-specific validation?
	default:
		return fmt.Errorf("internal error: unexpected extra snapd kernel command line fragment ID: %q", id)
	}
	return nil
}

const (
	// This holds kernel command line fragments that snapd adds internally.
	kcmdlineExtraSnapdFragmentsKey string = "kcmdline-extra-snapd-fragments"
	// This holds a boolean that indicates whether there are pending
	// extra snapd kernel command line fragments in state that need to be
	// appended to the kernel command line which can only be cleared by
	// tasks that update the kernel command line i.e. "update-managed-boot-config"
	// and "update-gadget-cmdline".
	kcmdlinePendingExtraSnapdFragmentsKey string = "kcmdline-pending-extra-snapd-fragments"
)

// setExtraSnapdKernelCommandLineFragment updates the specified extra snapd
// named fragment that is appended to the kernel command line. An empty
// string removes the specified fragment if it exists.
//
// If the passed arguments are different from the current arguments then
// "kcmdline-pending-extra-snapd-fragments" will be set to true which can only
// be cleared by tasks that apply the pending extra snapd appends from state
// i.e. "update-managed-boot-config" and "update-gadget-cmdline".
//
// Note that this only updates the specified fragment in snapd state and
// does not directly update the command line and key polices.
func setExtraSnapdKernelCommandLineFragment(st *state.State, fragmentID extraSnapdKernelCommandLineFragmentID, fragment string) (updated bool, err error) {
	if err := fragmentID.validate(fragment); err != nil {
		return false, err
	}

	var seeded bool
	err = st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	if !seeded {
		return false, fmt.Errorf("cannot set extra snapd kernel command line fragments until fully seeded")
	}

	var currentFragments map[extraSnapdKernelCommandLineFragmentID]string
	if err := st.Get(kcmdlineExtraSnapdFragmentsKey, &currentFragments); err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	currentFragment := currentFragments[fragmentID]
	if fragment == currentFragment {
		// Nothing changed, no-op.
		return false, nil
	}

	if currentFragments == nil {
		currentFragments = make(map[extraSnapdKernelCommandLineFragmentID]string, 1)
	}

	if fragment == "" {
		delete(currentFragments, fragmentID)
	} else {
		currentFragments[fragmentID] = fragment
	}
	st.Set(kcmdlineExtraSnapdFragmentsKey, currentFragments)
	st.Set(kcmdlinePendingExtraSnapdFragmentsKey, true)
	return true, nil
}

// kernelCommandLineAppendArgsFromSnapd returns extra arguments that snapd
// might set internally using setExtraSnapdKernelCommandLineFragment.
func kernelCommandLineAppendArgsFromSnapd(st *state.State) (string, error) {
	var fragments map[extraSnapdKernelCommandLineFragmentID]string
	if err := st.Get(kcmdlineExtraSnapdFragmentsKey, &fragments); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if len(fragments) == 0 {
		return "", nil
	}

	// XXX: Prune fragments that are no longer used?
	sorted := make([]string, 0, len(fragments))
	for _, cmdlineAppend := range fragments {
		sorted = append(sorted, cmdlineAppend)
	}
	// Sorting is needed so that the same set of args would
	// always have the same order so we don't accidentally
	// trigger a kcmdline update when the args are unchanged.
	sort.Strings(sorted)
	return strings.Join(sorted, " "), nil
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
		return "", fmt.Errorf("internal error: unexpected task parameter %q", taskParam)
	}
	if err := tr.Get("core", option, &value); err != nil && !config.IsNoOption(err) {
		return "", err
	}

	return value, nil
}

func buildAppendedKernelCommandLine(t *state.Task, gd *gadget.GadgetData, deviceCtx snapstate.DeviceContext) (string, error) {
	extraSnapdCmdlineAppend, err := kernelCommandLineAppendArgsFromSnapd(t.State())
	if err != nil {
		return "", err
	}

	tr := config.NewTransaction(t.State())
	rawConfigCmdlineAppend, err := kernelCommandLineAppendArgsFromConfig(t, tr, "cmdline-append")
	if err != nil {
		return "", err
	}
	// Validation against allow list has already happened in
	// configcore, but the gadget might have changed, so we check
	// again and filter any unallowed argument.
	configCmdlineAppend, forbidden := gadget.FilterKernelCmdline(rawConfigCmdlineAppend, gd.Info.KernelCmdline.Allow)
	if forbidden != "" {
		warnMsg := fmt.Sprintf("%q is not allowed by the gadget and has been filtered out from the kernel command line", forbidden)
		logger.Notice(warnMsg)
		t.Logf(warnMsg)
	}

	// Dangerous extra cmdline only considered for dangerous models
	if deviceCtx.Model().Grade() == asserts.ModelDangerous {
		configCmdlineAppendDanger, err := kernelCommandLineAppendArgsFromConfig(t, tr,
			"dangerous-cmdline-append")
		if err != nil {
			return "", err
		}
		configCmdlineAppend = strutil.JoinNonEmpty(
			[]string{configCmdlineAppend, configCmdlineAppendDanger}, " ")
	}

	cmdlineAppend := strutil.JoinNonEmpty(
		[]string{extraSnapdCmdlineAppend, configCmdlineAppend}, " ")

	logger.Debugf("appended kernel command line part is %q", cmdlineAppend)

	return cmdlineAppend, nil
}
