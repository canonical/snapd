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

package boot

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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
)

var (
	validModes = []string{ModeInstall, ModeRecover, ModeRun}
)

// ModeAndRecoverySystemFromKernelCommandLine returns the current system mode
// and the recovery system label as passed in the kernel command line by the
// bootloader.
func ModeAndRecoverySystemFromKernelCommandLine() (mode, sysLabel string, err error) {
	m, err := osutil.KernelCommandLineKeyValues("snapd_recovery_mode", "snapd_recovery_system")
	if err != nil {
		return "", "", err
	}
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
	bl, err := bootloader.Find(where, opts)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find trusted assets bootloader under %q: %v", where, err)
	}
	mbl, ok := bl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// the bootloader cannot manage its scripts
		return nil, errBootConfigNotManaged
	}
	return mbl, nil
}

const (
	currentEdition = iota
	candidateEdition
)

func composeCommandLine(model *asserts.Model, currentOrCandidate int, mode, system string) (string, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		return "", nil
	}
	if mode != ModeRun && mode != ModeRecover {
		return "", fmt.Errorf("internal error: unsupported command line mode %q", mode)
	}
	// get the run mode bootloader under the native run partition layout
	opts := &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	}
	bootloaderRootDir := InitramfsUbuntuBootDir
	modeArg := "snapd_recovery_mode=run"
	systemArg := ""
	if mode == ModeRecover {
		if system == "" {
			return "", fmt.Errorf("internal error: system is unset")
		}
		// dealing with recovery system bootloader
		opts.Role = bootloader.RoleRecovery
		bootloaderRootDir = InitramfsUbuntuSeedDir
		// recovery mode & system command line arguments
		modeArg = "snapd_recovery_mode=recover"
		systemArg = fmt.Sprintf("snapd_recovery_system=%v", system)
	}
	mbl, err := getBootloaderManagingItsAssets(bootloaderRootDir, opts)
	if err != nil {
		if err == errBootConfigNotManaged {
			return "", nil
		}
		return "", err
	}
	// TODO:UC20: fetch extra args from gadget
	extraArgs := ""
	if currentOrCandidate == currentEdition {
		return mbl.CommandLine(modeArg, systemArg, extraArgs)
	} else {
		return mbl.CandidateCommandLine(modeArg, systemArg, extraArgs)
	}
}

// ComposeRecoveryCommandLine composes the kernel command line used when booting
// a given system in recover mode.
func ComposeRecoveryCommandLine(model *asserts.Model, system string) (string, error) {
	return composeCommandLine(model, currentEdition, ModeRecover, system)
}

// ComposeCommandLine composes the kernel command line used when booting the
// system in run mode.
func ComposeCommandLine(model *asserts.Model) (string, error) {
	return composeCommandLine(model, currentEdition, ModeRun, "")
}

// ComposeCandidateCommandLine composes the kernel command line used when
// booting the system in run mode with the current built-in edition of managed
// boot assets.
func ComposeCandidateCommandLine(model *asserts.Model) (string, error) {
	return composeCommandLine(model, candidateEdition, ModeRun, "")
}

// ComposeCandidateRecoveryCommandLine composes the kernel command line used
// when booting the given system in recover mode with the current built-in
// edition of managed boot assets.
func ComposeCandidateRecoveryCommandLine(model *asserts.Model, system string) (string, error) {
	return composeCommandLine(model, candidateEdition, ModeRecover, system)
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
		// compatibility scenario, no command lines tracked in modeenv
		// yet, this can happen when having booted with a newer snapd
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
	newM, err := m.Copy()
	if err != nil {
		return nil, err
	}

	// get the current command line
	cmdlineBootedWith, err := osutil.KernelCommandLine()
	if err != nil {
		return nil, err
	}
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
	cmdlineExpected, err := ComposeCommandLine(model)
	if err != nil {
		return nil, err
	}
	cmdlineBootedWith, err := osutil.KernelCommandLine()
	if err != nil {
		return nil, err
	}
	if cmdlineExpected != cmdlineBootedWith {
		return nil, fmt.Errorf("unexpected current command line: %q", cmdlineBootedWith)
	}
	newM, err := m.Copy()
	if err != nil {
		return nil, err
	}
	newM.CurrentKernelCommandLines = bootCommandLines{cmdlineExpected}
	return newM, nil
}

// observeCommandLineUpdate observes a pending kernel command line change caused
// by an update of boot config. When needed, the modeenv is updated with a
// candidate command line and the encryption keys are resealed. This helper
// should be called right before updating the managed boot config.
func observeCommandLineUpdate(model *asserts.Model) error {
	// TODO:UC20: consider updating a recovery system command line

	m, err := loadModeenv()
	if err != nil {
		return err
	}

	if len(m.CurrentKernelCommandLines) == 0 {
		return fmt.Errorf("internal error: current kernel command lines is unset")
	}
	// this is the current expected command line which was recorded by
	// bootstate
	cmdline := m.CurrentKernelCommandLines[0]
	// this is the new expected command line
	candidateCmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return err
	}
	if cmdline == candidateCmdline {
		// no change in command line contents, nothing to do
		return nil
	}
	m.CurrentKernelCommandLines = bootCommandLines{cmdline, candidateCmdline}

	if err := m.Write(); err != nil {
		return err
	}

	expectReseal := true
	if err := resealKeyToModeenv(dirs.GlobalRootDir, model, m, expectReseal); err != nil {
		return err
	}
	return nil
}

// kernelCommandLinesForResealWithFallback provides the list of kernel command
// lines for use during reseal. During normal operation, the command lines will
// be listed in the modeenv.
func kernelCommandLinesForResealWithFallback(model *asserts.Model, modeenv *Modeenv) (cmdlines []string, err error) {
	if len(modeenv.CurrentKernelCommandLines) > 0 {
		return modeenv.CurrentKernelCommandLines, nil
	}
	// fallback for when reseal is called before mark boot successful set a
	// default during snapd update
	cmdline, err := ComposeCommandLine(model)
	if err != nil {
		return nil, err
	}
	return []string{cmdline}, nil
}
