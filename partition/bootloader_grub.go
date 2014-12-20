package partition

import (
)

type Grub struct {
	partition *Partition
}

func (g *Grub) Name() string {
	return "grub"
}

func (g *Grub) Installed() bool {
	// crude heuristic
	err := FileExists("/boot/grub/grub.cfg")

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

	args = append(args, "grub-install")
	args = append(args, other.parentName)

	// install grub
	err = g.partition.RunInChroot(args)
	if err != nil {
		return err
	}

	args = nil
	args = append(args, "update-grub")

	// create the grub config
	err = g.partition.RunInChroot(args)

	return err
}

func (g *Grub) GetAllBootVars() (vars []string, err error) {
	// FIXME: 'grub-editenv list'
	return vars, err
}

func (g *Grub) GetBootVar(name string) (value string) {
	// FIXME: 'grub-editenv list|grep $name'
	return value
}

func (g *Grub) SetBootVar(name, value string) (err error) {
	// FIXME: 'grub-editenv set name=value'
	return err
}

func (g *Grub) ClearBootVar(name string) (currentValue string, err error) {
	// FIXME: 'grub-editenv unset name'
	return currentValue, err
}

func (g *Grub) GetNextBootRootLabel() (label string) {
	// FIXME
	return label
}

func (g *Grub) GetCurrentBootRootLabel() (label string) {
	// FIXME
	return label
}
