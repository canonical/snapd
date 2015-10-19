// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	"fmt"

	"launchpad.net/snappy/helpers"

	"github.com/mvo5/goconfigparser"
)

const (
	bootloaderGrubDirReal        = "/boot/grub"
	bootloaderGrubConfigFileReal = "/boot/grub/grub.cfg"
	bootloaderGrubEnvFileReal    = "/boot/grub/grubenv"

	bootloaderGrubEnvCmdReal = "/usr/bin/grub-editenv"
)

// var to make it testable
var (
	bootloaderGrubDir        = bootloaderGrubDirReal
	bootloaderGrubConfigFile = bootloaderGrubConfigFileReal
	bootloaderGrubEnvFile    = bootloaderGrubEnvFileReal

	bootloaderGrubEnvCmd = bootloaderGrubEnvCmdReal
)

type grub struct {
	bootloaderType
}

const bootloaderNameGrub bootloaderName = "grub"

// newGrub create a new Grub bootloader object
func newGrub(partition *Partition) BootLoader {
	if !helpers.FileExists(bootloaderGrubConfigFile) {
		return nil
	}

	b := newBootLoader(partition, bootloaderGrubDir)
	if b == nil {
		return nil
	}
	g := grub{bootloaderType: *b}

	return &g
}

func (g *grub) Name() bootloaderName {
	return bootloaderNameGrub
}

// ToggleRootFS make the Grub bootloader switch rootfs's.
//
// Approach:
//
// Update the grub configuration.
func (g *grub) ToggleRootFS(otherRootfs string) (err error) {

	if err := g.SetBootVar(bootloaderBootmodeVar, bootloaderBootmodeTry); err != nil {
		return err
	}

	// Record the partition that will be used for next boot. This
	// isn't necessary for correct operation under grub, but allows
	// us to query the next boot device easily.
	return g.SetBootVar(bootloaderRootfsVar, otherRootfs)
}

func (g *grub) GetBootVar(name string) (value string, err error) {
	// Grub doesn't provide a get verb, so retrieve all values and
	// search for the required variable ourselves.
	output, err := runCommandWithStdout(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "list")
	if err != nil {
		return "", err
	}

	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadString(output); err != nil {
		return "", err
	}

	return cfg.Get("", name)
}

func (g *grub) SetBootVar(name, value string) (err error) {
	// note that strings are not quoted since because
	// RunCommand() does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	arg := fmt.Sprintf("%s=%s", name, value)
	return runCommand(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", arg)
}

func (g *grub) GetNextBootRootFSName() (label string, err error) {
	return g.GetBootVar(bootloaderRootfsVar)
}

func (g *grub) MarkCurrentBootSuccessful(currentRootfs string) (err error) {
	// Clear the variable set on boot to denote a good boot.
	if err := g.SetBootVar(bootloaderTrialBootVar, "0"); err != nil {
		return err
	}

	if err := g.SetBootVar(bootloaderRootfsVar, currentRootfs); err != nil {
		return err
	}

	return g.SetBootVar(bootloaderBootmodeVar, bootloaderBootmodeSuccess)
}

func (g *grub) BootDir() string {
	return bootloaderGrubDir
}
