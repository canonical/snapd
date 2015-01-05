package partition

import (
	"strings"
)

const (
	BOOTLOADER_GRUB_CONFIG_FILE = "/boot/grub/grub.cfg"
	BOOTLOADER_GRUB_ENV_FILE = "/boot/grub/grubenv"

	BOOTLOADER_GRUB_ENV_CMD = "/usr/bin/grub-editenv"
	BOOTLOADER_GRUB_INSTALL_CMD = "/usr/sbin/grub-install"
	BOOTLOADER_GRUB_UPDATE_CMD = "/usr/sbin/update-grub"
)

type Grub struct {
	partition *Partition
}

func (g *Grub) Name() string {
	return "grub"
}

func (g *Grub) Installed() bool {
	// crude heuristic
	err := FileExists(BOOTLOADER_GRUB_CONFIG_FILE)

	if err == nil {
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

	other = g.partition.OtherRootPartition()

	args = append(args, BOOTLOADER_GRUB_INSTALL_CMD)
	args = append(args, other.parentName)

	// install grub
	err = g.partition.RunInChroot(args)
	if err != nil {
		return err
	}

	args = nil
	args = append(args, BOOTLOADER_GRUB_UPDATE_CMD)

	// create the grub config
	err = g.partition.RunInChroot(args)

	return err
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
	args = append(args, name)
	args = append(args, value)

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

func (g *Grub) GetNextBootRootLabel() (label string) {
	// FIXME: call GetBootVar("snappy_ab")
	return label
}

func (g *Grub) GetCurrentBootRootLabel() (label string) {
	// FIXME: lsblk output
	return label
}
