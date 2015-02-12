//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package partition

import (
	"fmt"

	"github.com/mvo5/goconfigparser"
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

const BootloaderNameGrub BootloaderName = "grub"

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

func (g *Grub) Name() BootloaderName {
	return BootloaderNameGrub
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
	if err := runInChroot(g.partition.MountTarget(), bootloaderGrubInstallCmd, other.parentName); err != nil {
		return err
	}

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

func (g *Grub) GetBootVar(name string) (value string, err error) {
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

func (g *Grub) setBootVar(name, value string) (err error) {
	// note that strings are not quoted since because
	// RunCommand() does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	arg := fmt.Sprintf("%s=%s", name, value)
	return runCommand(bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", arg)
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
	return g.setBootVar(bootloaderBootmodeVar, bootloaderBootmodeSuccess)
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
