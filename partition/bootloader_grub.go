//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package partition

import (
	"fmt"
	"strings"
)

const (
	BOOTLOADER_GRUB_DIR         = "/boot/grub"
	BOOTLOADER_GRUB_CONFIG_FILE = "/boot/grub/grub.cfg"
	BOOTLOADER_GRUB_ENV_FILE    = "/boot/grub/grubenv"

	BOOTLOADER_GRUB_ENV_CMD     = "/usr/bin/grub-editenv"
	BOOTLOADER_GRUB_INSTALL_CMD = "/usr/sbin/grub-install"
	BOOTLOADER_GRUB_UPDATE_CMD  = "/usr/sbin/update-grub"
)

type Grub struct {
	*BootLoaderType
}

// Create a new Grub bootloader object
func NewGrub(partition *Partition) *Grub {
	g := Grub{BootLoaderType: NewBootLoader(partition)}

	g.currentBootPath = BOOTLOADER_GRUB_DIR
	g.otherBootPath = g.currentBootPath

	return &g
}

func (g *Grub) Name() string {
	return "grub"
}

func (g *Grub) Installed() bool {

	// Use same heuristic as the initramfs.
	err1 := FileExists(BOOTLOADER_GRUB_CONFIG_FILE)
	err2 := FileExists(BOOTLOADER_GRUB_INSTALL_CMD)

	if err1 == nil && err2 == nil {
		return true
	}

	return false
}

// Make the Grub bootloader switch rootfs's.
//
// Approach:
//
// Re-install grub each time the rootfs is toggled by running
// grub-install chrooted into the other rootfs. Also update the grub
// configuration.
func (g *Grub) ToggleRootFS() (err error) {

	var args []string
	var other *BlockDevice

	other = g.partition.otherRootPartition()

	args = append(args, BOOTLOADER_GRUB_INSTALL_CMD)
	args = append(args, other.parentName)

	// install grub
	err = g.partition.runInChroot(args)
	if err != nil {
		return err
	}

	args = nil
	args = append(args, BOOTLOADER_GRUB_UPDATE_CMD)

	// create the grub config
	err = g.partition.runInChroot(args)
	if err != nil {
		return err
	}

	err = g.SetBootVar(BOOTLOADER_BOOTMODE_VAR,
		BOOTLOADER_BOOTMODE_VAR_START_VALUE)
	if err != nil {
		return err
	}

	// Record the partition that will be used for next boot. This
	// isn't necessary for correct operation under grub, but allows
	// us to query the next boot device easily.
	return g.SetBootVar(BOOTLOADER_ROOTFS_VAR, other.name)
}

func (g *Grub) GetAllBootVars() (vars []string, err error) {
	var args []string

	args = append(args, BOOTLOADER_GRUB_ENV_CMD)
	args = append(args, BOOTLOADER_GRUB_ENV_FILE)
	args = append(args, "list")

	return GetCommandStdout(args)
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
	var args []string

	args = append(args, BOOTLOADER_GRUB_ENV_CMD)
	args = append(args, BOOTLOADER_GRUB_ENV_FILE)
	args = append(args, "set")

	// XXX: note that strings are not quoted since because
	// RunCommand() does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	args = append(args, fmt.Sprintf("%s=%s", name, value))

	return RunCommand(args)
}

// FIXME: not atomic - need locking around snappy command!
func (g *Grub) ClearBootVar(name string) (currentValue string, err error) {
	var args []string

	currentValue, err = g.GetBootVar(name)
	if err != nil {
		return currentValue, err
	}

	args = append(args, BOOTLOADER_GRUB_ENV_CMD)
	args = append(args, BOOTLOADER_GRUB_ENV_FILE)
	args = append(args, "unset")
	args = append(args, name)

	return currentValue, RunCommand(args)
}

func (g *Grub) GetNextBootRootFSName() (label string, err error) {
	return g.GetBootVar(BOOTLOADER_ROOTFS_VAR)
}

func (g *Grub) GetRootFSName() (string) {
	return g.currentRootfs
}

func (g *Grub) GetOtherRootFSName() (string) {
	return g.otherRootfs
}

func (g *Grub) MarkCurrentBootSuccessful() (err error) {
	return g.SetBootVar(BOOTLOADER_BOOTMODE_VAR,
			    BOOTLOADER_BOOTMODE_VAR_END_VALUE)
}

func (g *Grub) SyncBootFiles() (err error) {
	// NOP
	return err
}

func (g *Grub) HandleAssets() (err error) {

	// NOP - since grub is used on generic hardware, it doesn't
	// need to make use of hardware-specific assets
	return err
}
