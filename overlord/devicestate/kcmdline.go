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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

type extraSnapdKernelCommandLineFragmentID string

const (
	extraSnapdKernelCommandLineFragmentXKB extraSnapdKernelCommandLineFragmentID = "xkb"
)

// expectedInstallTimeFragmentIDs lists the fragment IDs that may be initialized
// from the install-time persistence file. Only these keys are copied into
// state when lazily initializing from the file.
var expectedInstallTimeFragmentIDs = []extraSnapdKernelCommandLineFragmentID{
	extraSnapdKernelCommandLineFragmentXKB,
}

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

// kcmdlineExtraSnapdFragmentsFileName is the name of the JSON file written at
// install time (to the ubuntu-save device dir) holding the extra snapd kernel
// command line fragments map. It is read at runtime to lazily initialize
// state with the install-time choices.
const kcmdlineExtraSnapdFragmentsFileName = "kcmdline-extra-snapd-fragments.json"

// renderExtraSnapdKernelCommandLineFragments renders a map of extra snapd
// kernel command line fragments into sorted, space-joined args.
func renderExtraSnapdKernelCommandLineFragments(fragments map[string]string) string {
	if len(fragments) == 0 {
		return ""
	}

	sorted := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		sorted = append(sorted, fragment)
	}
	// Sorting is needed so that the same set of args would
	// always have the same order so we don't accidentally
	// trigger a kcmdline update when the args are unchanged.
	sort.Strings(sorted)
	return strings.Join(sorted, " ")
}

// initExtraSnapdFragmentsFromInstallTime initializes the extra snapd kernel command
// line fragments state from the install-time persistence file when state has not
// yet been set. If the state key is already set, it is a no-op.
func initExtraSnapdFragmentsFromInstallTime(st *state.State) error {
	var current map[extraSnapdKernelCommandLineFragmentID]string
	if err := st.Get(kcmdlineExtraSnapdFragmentsKey, &current); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if current != nil {
		// State already initialized, nothing to do.
		return nil
	}

	path := filepath.Join(dirs.SnapDeviceSaveDir, kcmdlineExtraSnapdFragmentsFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// No install-time file, nothing to do.
		return nil
	}
	if err != nil {
		return err
	}

	var fileFragments map[string]string
	if err := json.Unmarshal(data, &fileFragments); err != nil {
		return fmt.Errorf("cannot parse install-time kernel command line fragments file %q: %v", path, err)
	}

	fragments := make(map[extraSnapdKernelCommandLineFragmentID]string, len(expectedInstallTimeFragmentIDs))
	for _, id := range expectedInstallTimeFragmentIDs {
		if fragment, ok := fileFragments[string(id)]; ok && fragment != "" {
			fragments[id] = fragment
		}
	}
	st.Set(kcmdlineExtraSnapdFragmentsKey, fragments)
	return nil
}

// writeInstallTimeExtraSnapdFragments writes the given extra snapd kernel
// command line fragments map as a JSON file to the given device save
// directory. It is written at install time so the fragments can be lazily
// loaded into state at runtime.
func writeInstallTimeExtraSnapdFragments(deviceSaveDir string, fragments map[string]string) error {
	data, err := json.Marshal(fragments)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(deviceSaveDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(deviceSaveDir, kcmdlineExtraSnapdFragmentsFileName)
	return osutil.AtomicWriteFile(path, data, 0644, 0)
}

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
func setExtraSnapdKernelCommandLineFragment(st *state.State, fragmentID extraSnapdKernelCommandLineFragmentID, fragment string) error {
	if err := fragmentID.validate(fragment); err != nil {
		return err
	}

	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return fmt.Errorf("cannot set extra snapd kernel command line fragments until fully seeded")
	}

	// Ensure install-time fragments are seeded into state before comparing
	// against the current value so runtime updates correctly override the
	// install-time baseline.
	if err := initExtraSnapdFragmentsFromInstallTime(st); err != nil {
		return err
	}

	var currentFragments map[extraSnapdKernelCommandLineFragmentID]string
	if err := st.Get(kcmdlineExtraSnapdFragmentsKey, &currentFragments); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	currentFragment := currentFragments[fragmentID]
	if fragment == currentFragment {
		// Nothing changed, no-op.
		return nil
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
	// Make sure the pending changes are picked up soon.
	st.EnsureBefore(0)
	return nil
}

// kernelCommandLineAppendArgsFromSnapd returns extra arguments that snapd
// might set internally using setExtraSnapdKernelCommandLineFragment.
func kernelCommandLineAppendArgsFromSnapd(st *state.State) (string, error) {
	// Lazily seed install-time fragments into state if not yet set so that
	// the returned string includes any install-time choices that have not
	// been overridden at runtime.
	if err := initExtraSnapdFragmentsFromInstallTime(st); err != nil {
		return "", err
	}

	var fragments map[extraSnapdKernelCommandLineFragmentID]string
	if err := st.Get(kcmdlineExtraSnapdFragmentsKey, &fragments); err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if len(fragments) == 0 {
		return "", nil
	}

	rendered := make(map[string]string, len(fragments))
	for id, fragment := range fragments {
		rendered[string(id)] = fragment
	}
	return renderExtraSnapdKernelCommandLineFragments(rendered), nil
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
