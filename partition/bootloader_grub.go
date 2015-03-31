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

	bootloaderGrubEnvCmdReal       = "/usr/bin/grub-editenv"
	bootloaderGrubUpdateCmdReal    = "/usr/sbin/update-grub"
	bootloaderGrubTrialBootVarReal = "snappy_trial_boot"
)

// var to make it testable
var (
	bootloaderGrubDir          = bootloaderGrubDirReal
	bootloaderGrubConfigFile   = bootloaderGrubConfigFileReal
	bootloaderGrubTrialBootVar = bootloaderGrubTrialBootVarReal
	bootloaderGrubEnvFile      = bootloaderGrubEnvFileReal

	bootloaderGrubEnvCmd    = bootloaderGrubEnvCmdReal
	bootloaderGrubUpdateCmd = bootloaderGrubUpdateCmdReal
)

type grub struct {
	*bootloaderType
}

const bootloaderNameGrub bootloaderName = "grub"

// newGrub create a new Grub bootloader object
func newGrub(partition *Partition) bootLoader {
	if !helpers.FileExists(bootloaderGrubConfigFile) || !helpers.FileExists(bootloaderGrubUpdateCmd) {
		return nil
	}
	b := newBootLoader(partition)
	if b == nil {
		return nil
	}
	g := &grub{bootloaderType: b}

	return g
}

func (g *grub) Name() bootloaderName {
	return bootloaderNameGrub
}

// ToggleRootFS make the Grub bootloader switch rootfs's.
//
// Approach:
//
// Update the grub configuration.
func (g *grub) ToggleRootFS() (err error) {

	// create the grub config
	if err := runInChroot(g.partition.MountTarget(), bootloaderGrubUpdateCmd); err != nil {
		return err
	}

	if err := g.setBootVar(bootloaderBootmodeVar, bootloaderBootmodeTry); err != nil {
		return err
	}

	// Record the partition that will be used for next boot. This
	// isn't necessary for correct operation under grub, but allows
	// us to query the next boot device easily.
	return g.setBootVar(bootloaderRootfsVar, g.otherRootfs)
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

func (g *grub) setBootVar(name, value string) (err error) {
	// note that strings are not quoted since because
	// RunCommand() does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	arg := fmt.Sprintf("%s=%s", name, value)
	return runCommand(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", arg)
}

func (g *grub) unsetBootVar(name string) (err error) {
	return runCommand(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "unset", name)
}

func (g *grub) GetNextBootRootFSName() (label string, err error) {
	return g.GetBootVar(bootloaderRootfsVar)
}

func (g *grub) GetRootFSName() string {
	return g.currentRootfs
}

func (g *grub) GetOtherRootFSName() string {
	return g.otherRootfs
}

func (g *grub) MarkCurrentBootSuccessful() (err error) {
	// Clear the variable set by grub on boot to denote a good
	// boot.
	if err := g.unsetBootVar(bootloaderGrubTrialBootVar); err != nil {
		return err
	}
	return g.setBootVar(bootloaderBootmodeVar, bootloaderBootmodeSuccess)
}

func (g *grub) SyncBootFiles() (err error) {
	// NOP
	return nil
}

func (g *grub) HandleAssets() (err error) {

	// NOP - since grub is used on generic hardware, it doesn't
	// need to make use of hardware-specific assets
	return nil
}

func (g *grub) AdditionalBindMounts() []string {
	// grub needs this in addition to "system-boot" as its the
	// well known location for its configuration
	return []string{"/boot/grub"}
}
