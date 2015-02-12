//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package partition

import (
	"fmt"
	"strings"
)

var (
	bootloaderGrubDir        = "/boot/grub"
	bootloaderGrubConfigFile = "/boot/grub/grub.cfg"
	bootloaderGrubEnvFile    = "/boot/grub/grubenv"

	bootloaderGrubEnvCmd     = "/usr/bin/grub-editenv"
	bootloaderGrubInstallCmd = "/usr/sbin/grub-install"
	bootloaderGrubUpdateCmd  = "/usr/sbin/update-grub"
)

type Grub struct {
	*BootLoaderType
}

// Create a new Grub bootloader object
func NewGrub(partition *Partition) *Grub {
	if !fileExists(bootloaderGrubConfigFile) || !fileExists(bootloaderGrubInstallCmd) {
		return nil
	}
	b := NewBootLoader(partition)
	if b == nil {
		return nil
	}
	g := &Grub{BootLoaderType: b}
	g.currentBootPath = bootloaderGrubDir
	g.otherBootPath = g.currentBootPath

	return g
}

func (g *Grub) Name() string {
	return "grub"
}

// Make the Grub bootloader switch rootfs's.
//
// Approach:
//
// Re-install grub each time the rootfs is toggled by running
// grub-install chrooted into the other rootfs. Also update the grub
// configuration.
func (g *Grub) ToggleRootFS() (err error) {

	other := g.partition.otherRootPartition()

	// install grub
	err = runInChroot(g.partition.MountTarget(), bootloaderGrubInstallCmd, other.parentName)
	if err != nil {
		return err
	}

	// create the grub config
	err = runInChroot(g.partition.MountTarget(), bootloaderGrubUpdateCmd)
	if err != nil {
		return err
	}

	err = g.SetBootVar(bootloaderBootmodeVar, bootloaderBootmodeTry)
	if err != nil {
		return err
	}

	// Record the partition that will be used for next boot. This
	// isn't necessary for correct operation under grub, but allows
	// us to query the next boot device easily.
	return g.SetBootVar(bootloaderRootfsVar, g.otherRootfs)
}

func (g *Grub) GetAllBootVars() (vars []string, err error) {
	return runCommandWithStdout(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "list")
}

func (g *Grub) GetBootVar(name string) (value string, err error) {
	var values []string

	// Grub doesn't provide a get verb, so retrieve all values and
	// search for the required variable ourselves.
	values, err = g.GetAllBootVars()

	if err != nil {
		return value, err
	}

	for _, line := range values {
		if line == "" || line == "\n" {
			continue
		}

		fields := strings.Split(string(line), "=")
		if fields[0] == name {
			return fields[1], err
		}
	}

	return value, err
}

func (g *Grub) SetBootVar(name, value string) (err error) {
	// note that strings are not quoted since because
	// RunCommand() does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	arg := fmt.Sprintf("%s=%s", name, value)
	return runCommand(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", arg)
}

// FIXME: not atomic - need locking around snappy command!
func (g *Grub) ClearBootVar(name string) (currentValue string, err error) {
	currentValue, err = g.GetBootVar(name)
	if err != nil {
		return currentValue, err
	}

	return currentValue, runCommand(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "unset", name)
}

func (g *Grub) GetNextBootRootFSName() (label string, err error) {
	return g.GetBootVar(bootloaderRootfsVar)
}

func (g *Grub) GetRootFSName() string {
	return g.currentRootfs
}

func (g *Grub) GetOtherRootFSName() string {
	return g.otherRootfs
}

func (g *Grub) MarkCurrentBootSuccessful() (err error) {
	return g.SetBootVar(bootloaderBootmodeVar, bootloaderBootmodeSuccess)
}

func (g *Grub) SyncBootFiles() (err error) {
	// NOP
	return nil
}

func (g *Grub) HandleAssets() (err error) {

	// NOP - since grub is used on generic hardware, it doesn't
	// need to make use of hardware-specific assets
	return nil
}
