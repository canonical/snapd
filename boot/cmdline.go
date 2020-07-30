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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/logger"
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
	// the kernel commandline - can be overridden in tests
	procCmdline = "/proc/cmdline"

	validModes = []string{ModeInstall, ModeRecover, ModeRun}
)

func whichModeAndRecoverySystem(cmdline []byte) (mode string, sysLabel string, err error) {
	scanner := bufio.NewScanner(bytes.NewBuffer(cmdline))
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		w := scanner.Text()
		if strings.HasPrefix(w, "snapd_recovery_mode=") {
			if mode != "" {
				return "", "", fmt.Errorf("cannot specify mode more than once")
			}
			mode = strings.SplitN(w, "=", 2)[1]
			if mode == "" {
				mode = ModeInstall
			}
			if !strutil.ListContains(validModes, mode) {
				return "", "", fmt.Errorf("cannot use unknown mode %q", mode)
			}
		}
		if strings.HasPrefix(w, "snapd_recovery_system=") {
			if sysLabel != "" {
				return "", "", fmt.Errorf("cannot specify recovery system label more than once")
			}
			sysLabel = strings.SplitN(w, "=", 2)[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	switch {
	case mode == "" && sysLabel == "":
		return "", "", fmt.Errorf("cannot detect mode nor recovery system to use")
	case mode == ModeInstall && sysLabel == "":
		return "", "", fmt.Errorf("cannot specify install mode without system label")
	case mode == ModeRun && sysLabel != "":
		// XXX: should we silently ignore the label? at least log for now
		logger.Noticef(`ignoring recovery system label %q in "run" mode`, sysLabel)
		sysLabel = ""
	}
	return mode, sysLabel, nil
}

// ModeAndRecoverySystemFromKernelCommandLine returns the current system mode
// and the recovery system label as passed in the kernel command line by the
// bootloader.
func ModeAndRecoverySystemFromKernelCommandLine() (mode, sysLabel string, err error) {
	cmdline, err := ioutil.ReadFile(procCmdline)
	if err != nil {
		return "", "", err
	}
	return whichModeAndRecoverySystem(cmdline)
}

// MockProcCmdline overrides the path to /proc/cmdline. For use in tests.
func MockProcCmdline(newPath string) (restore func()) {
	oldProcCmdline := procCmdline
	procCmdline = newPath
	return func() {
		procCmdline = oldProcCmdline
	}
}

var errBootConfigNotManaged = errors.New("boot config is not managed")

func getBootloaderManagingItsAssets(where string, opts *bootloader.Options) (bootloader.ManagedAssetsBootloader, error) {
	bl, err := bootloader.Find(where, opts)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find managed assets bootloader under %q: %v", where, err)
	}
	mbl, ok := bl.(bootloader.ManagedAssetsBootloader)
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
	// get the ubuntu-seed bootloader
	opts := &bootloader.Options{
		NoSlashBoot: true,
	}
	bootloaderRootDir := InitramfsUbuntuBootDir
	modeArg := "snapd_recovery_mode=run"
	systemArg := ""
	if mode == ModeRecover {
		// dealing with recovery system bootloader
		opts.Recovery = true
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
