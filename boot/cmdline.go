// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package boot

import (
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/strutil"
)

const (
	// ModeRun indicates the regular operating system mode of the device.
	ModeRun = "run"
	// ModeInstall is a mode in which a new system is installed on the
	// device.
	ModeInstall = "install"
	// ModeRecover is a mode in which the device boots into the recovery
	// system.
	ModeRecover = "recover"
	// ModeFactoryReset is a mode in which the device performs a factory
	// reset.
	ModeFactoryReset = "factory-reset"
	// ModeRunCVM is Azure CVM specific run mode fde + classic debs
	ModeRunCVM = "cloudimg-rootfs"
)

var validModes = []string{ModeInstall, ModeRecover, ModeFactoryReset, ModeRun, ModeRunCVM}

// ModeAndRecoverySystemFromKernelCommandLine returns the current system mode
// and the recovery system label as passed in the kernel command line by the
// bootloader.
func ModeAndRecoverySystemFromKernelCommandLine() (mode, sysLabel string, err error) {
	m := mylog.Check2(kcmdline.KeyValues("snapd_recovery_mode", "snapd_recovery_system"))

	var modeOk bool
	mode, modeOk = m["snapd_recovery_mode"]

	// no mode specified gets interpreted as install
	if modeOk {
		if mode == "" {
			mode = ModeInstall
		} else if !strutil.ListContains(validModes, mode) {
			return "", "", fmt.Errorf("cannot use unknown mode %q", mode)
		}
	}

	sysLabel = m["snapd_recovery_system"]

	switch {
	case mode == "" && sysLabel == "":
		return "", "", fmt.Errorf("cannot detect mode nor recovery system to use")
	case mode == "" && sysLabel != "":
		return "", "", fmt.Errorf("cannot specify system label without a mode")
	case mode == ModeInstall && sysLabel == "":
		return "", "", fmt.Errorf("cannot specify install mode without system label")
	case mode == ModeRun && sysLabel != "":
		// XXX: should we silently ignore the label? at least log for now
		logger.Noticef(`ignoring recovery system label %q in "run" mode`, sysLabel)
		sysLabel = ""
	}
	return mode, sysLabel, nil
}

var errBootConfigNotManaged = errors.New("boot config is not managed")

func getBootloaderManagingItsAssets(where string, opts *bootloader.Options) (bootloader.TrustedAssetsBootloader, error) {
	bl := mylog.Check2(bootloader.Find(where, opts))

	mbl, ok := bl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// the bootloader cannot manage its scripts
		return nil, errBootConfigNotManaged
	}
	return mbl, nil
}

// bootVarsForTrustedCommandLineFromGadget returns a set of boot
// variables that carry the command line arguments defined by the
// gadget and some system options (cmdlineApped). This is only useful
// if snapd is managing the boot config.
func bootVarsForTrustedCommandLineFromGadget(gadgetDirOrSnapPath, cmdlineAppend string, defaultCmdline string, model gadget.Model) (map[string]string, error) {
	extraOrFull, full, removeArgs := mylog.Check4(gadget.KernelCommandLineFromGadget(gadgetDirOrSnapPath, model))

	logger.Debugf("trusted command line: from gadget: %q, from options: %q",
		extraOrFull, cmdlineAppend)

	extraOrFull = strutil.JoinNonEmpty([]string{extraOrFull, cmdlineAppend}, " ")

	keepDefaultArgs := kcmdline.RemoveMatchingFilter(defaultCmdline, removeArgs)

	// gadget has the kernel command line
	args := map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "",
	}
	if full {
		args["snapd_full_cmdline_args"] = extraOrFull
	} else {
		args["snapd_full_cmdline_args"] = strutil.JoinNonEmpty(append(keepDefaultArgs, extraOrFull), " ")
	}
	if len(args["snapd_full_cmdline_args"]) == 0 {
		// grub.cfg tests if snapd_full_cmdline_args is set by looking if it is not empty.
		// Here, it should be set, but empty. So adding a space will force grub.cfg to use it.
		args["snapd_full_cmdline_args"] = " "
	}
	return args, nil
}

const (
	currentEdition = iota
	candidateEdition
)

func composeCommandLine(currentOrCandidate int, mode, system, gadgetDirOrSnapPath string, model gadget.Model) (string, error) {
	if mode != ModeRun && mode != ModeRecover && mode != ModeFactoryReset {
		return "", fmt.Errorf("internal error: unsupported command line mode %q", mode)
	}
	// get the run mode bootloader under the native run partition layout
	opts := &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	}
	bootloaderRootDir := InitramfsUbuntuBootDir
	components := bootloader.CommandLineComponents{
		ModeArg: "snapd_recovery_mode=run",
	}
	if mode == ModeRecover || mode == ModeFactoryReset {
		if system == "" {
			return "", fmt.Errorf("internal error: system is unset")
		}
		// dealing with recovery system bootloader
		opts.Role = bootloader.RoleRecovery
		bootloaderRootDir = InitramfsUbuntuSeedDir
		// recovery mode & system command line arguments
		modeArg := "snapd_recovery_mode=recover"
		if mode == ModeFactoryReset {
			modeArg = "snapd_recovery_mode=factory-reset"
		}
		components = bootloader.CommandLineComponents{
			ModeArg:   modeArg,
			SystemArg: fmt.Sprintf("snapd_recovery_system=%v", system),
		}
	}
	mbl := mylog.Check2(getBootloaderManagingItsAssets(bootloaderRootDir, opts))

	if gadgetDirOrSnapPath != "" {
		extraOrFull, full, removeArgs := mylog.Check4(gadget.KernelCommandLineFromGadget(gadgetDirOrSnapPath, model))
		components.RemoveArgs = removeArgs

		// gadget provides some part of the kernel command line
		if full {
			components.FullArgs = extraOrFull
		} else {
			components.ExtraArgs = extraOrFull
		}
	}
	if currentOrCandidate == currentEdition {
		return mbl.CommandLine(components)
	} else {
		return mbl.CandidateCommandLine(components)
	}
}

// ComposeRecoveryCommandLine composes the kernel command line used when booting
// a given system in recover mode.
func ComposeRecoveryCommandLine(model *asserts.Model, system, gadgetDirOrSnapPath string) (string, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		return "", nil
	}
	return composeCommandLine(currentEdition, ModeRecover, system, gadgetDirOrSnapPath, model)
}

// ComposeCommandLine composes the kernel command line used when booting the
// system in run mode.
func ComposeCommandLine(model *asserts.Model, gadgetDirOrSnapPath string) (string, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		return "", nil
	}
	return composeCommandLine(currentEdition, ModeRun, "", gadgetDirOrSnapPath, model)
}

// ComposeCandidateCommandLine composes the kernel command line used when
// booting the system in run mode with the current built-in edition of managed
// boot assets.
func ComposeCandidateCommandLine(model *asserts.Model, gadgetDirOrSnapPath string) (string, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		return "", nil
	}
	return composeCommandLine(candidateEdition, ModeRun, "", gadgetDirOrSnapPath, model)
}

// ComposeCandidateRecoveryCommandLine composes the kernel command line used
// when booting the given system in recover mode with the current built-in
// edition of managed boot assets.
func ComposeCandidateRecoveryCommandLine(model *asserts.Model, system, gadgetDirOrSnapPath string) (string, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		return "", nil
	}
	return composeCommandLine(candidateEdition, ModeRecover, system, gadgetDirOrSnapPath, model)
}

// observeSuccessfulCommandLine observes a successful boot with a command line
// and takes an action based on the contents of the modeenv. The current kernel
// command lines in the modeenv can have up to 2 entries when the managed
// bootloader boot config gets updated.
func observeSuccessfulCommandLine(model *asserts.Model, m *Modeenv) (*Modeenv, error) {
	// TODO:UC20 only care about run mode for now
	if m.Mode != "run" {
		return m, nil
	}

	switch len(m.CurrentKernelCommandLines) {
	case 0:
		// maybe a compatibility scenario, no command lines tracked in
		// modeenv yet, this can happen when having booted with a newer
		// snapd
		return observeSuccessfulCommandLineCompatBoot(model, m)
	case 1:
		// no command line update
		return m, nil
	default:
		return observeSuccessfulCommandLineUpdate(m)
	}
}

// observeSuccessfulCommandLineUpdate observes a successful boot with a command
// line which is expected to be listed among the current kernel command line
// entries carried in the modeenv. One of those entries must match the current
// kernel command line of a running system and will be recorded alone as in use.
func observeSuccessfulCommandLineUpdate(m *Modeenv) (*Modeenv, error) {
	newM := mylog.Check2(m.Copy())

	// get the current command line
	cmdlineBootedWith := mylog.Check2(kcmdline.KernelCommandLine())

	if !strutil.ListContains([]string(m.CurrentKernelCommandLines), cmdlineBootedWith) {
		return nil, fmt.Errorf("current command line content %q not matching any expected entry",
			cmdlineBootedWith)
	}
	newM.CurrentKernelCommandLines = bootCommandLines{cmdlineBootedWith}

	return newM, nil
}

// observeSuccessfulCommandLineCompatBoot observes a successful boot with a
// kernel command line, where the list of current kernel command lines in the
// modeenv is unpopulated. This handles a compatibility scenario with systems
// that were installed using a previous version of snapd. It verifies that the
// expected kernel command line matches the one the system booted with and
// populates modeenv kernel command line list accordingly.
func observeSuccessfulCommandLineCompatBoot(model *asserts.Model, m *Modeenv) (*Modeenv, error) {
	// since this is a compatibility scenario, the kernel command line
	// arguments would not have come from the gadget before either
	cmdlineExpected := mylog.Check2(ComposeCommandLine(model, ""))

	if cmdlineExpected == "" {
		// there is no particular command line expected for this model
		// and system bootloader, indicating that the command line is
		// not being tracked
		return m, nil
	}
	cmdlineBootedWith := mylog.Check2(kcmdline.KernelCommandLine())

	if cmdlineExpected != cmdlineBootedWith {
		return nil, fmt.Errorf("unexpected current command line: %q", cmdlineBootedWith)
	}
	newM := mylog.Check2(m.Copy())

	newM.CurrentKernelCommandLines = bootCommandLines{cmdlineExpected}
	return newM, nil
}

type commandLineUpdateReason int

const (
	commandLineUpdateReasonSnapd commandLineUpdateReason = iota
	commandLineUpdateReasonGadget
)

// observeCommandLineUpdate observes a pending kernel command line change caused
// by an update of boot config or the gadget snap. When needed, the modeenv is
// updated with a candidate command line and the encryption keys are resealed.
// This helper should be called right before updating the managed boot config.
func observeCommandLineUpdate(model *asserts.Model, reason commandLineUpdateReason, gadgetSnapOrDir, cmdlineOpt string) (updated bool, err error) {
	// TODO:UC20: consider updating a recovery system command line

	m := mylog.Check2(loadModeenv())

	if len(m.CurrentKernelCommandLines) == 0 {
		return false, fmt.Errorf("internal error: current kernel command lines is unset")
	}
	// this is the current expected command line which was recorded by
	// bootstate
	cmdline := m.CurrentKernelCommandLines[0]
	// this is the new expected command line
	var candidateCmdline string
	switch reason {
	case commandLineUpdateReasonSnapd:
		// pending boot config update
		candidateCmdline = mylog.Check2(ComposeCandidateCommandLine(model, gadgetSnapOrDir))
	case commandLineUpdateReasonGadget:
		// pending gadget update
		candidateCmdline = mylog.Check2(ComposeCommandLine(model, gadgetSnapOrDir))
	}

	// Add part coming from options
	candidateCmdline = strutil.JoinNonEmpty(
		[]string{candidateCmdline, cmdlineOpt}, " ")
	if cmdline == candidateCmdline {
		// command line is the same or no actual change in modeenv
		return false, nil
	}
	logger.Debugf("kernel commandline changes from %q to %q", cmdline, candidateCmdline)
	// actual change of the command line content
	m.CurrentKernelCommandLines = bootCommandLines{cmdline, candidateCmdline}
	mylog.Check(m.Write())

	expectReseal := true
	mylog.Check(resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal, nil))

	return true, nil
}

// kernelCommandLinesForResealWithFallback provides the list of kernel command
// lines for use during reseal. During normal operation, the command lines will
// be listed in the modeenv.
func kernelCommandLinesForResealWithFallback(modeenv *Modeenv) (cmdlines []string, err error) {
	if len(modeenv.CurrentKernelCommandLines) > 0 {
		return modeenv.CurrentKernelCommandLines, nil
	}
	// fallback for when reseal is called before mark boot successful set a
	// default during snapd update, since this is a compatibility scenario
	// there would be no kernel command lines arguments coming from the
	// gadget either
	gadgetDir := ""
	cmdline := mylog.Check2(composeCommandLine(currentEdition, ModeRun, "", gadgetDir, modeenv.ModelForSealing()))

	return []string{cmdline}, nil
}
