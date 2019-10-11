// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

// Package recovery implements core recovery and install
package recovery

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"

	"github.com/chrisccoulson/go-tpm2"
	"github.com/chrisccoulson/ubuntu-core-fde-utils"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

const (
	sizeSector = 512
	sizeKB     = 1 << 10
	sizeMB     = 1 << 20
)

func Recover(version string) error {
	logger.Noticef("Run recovery %s", version)

	// Reset snap mode
	mntSysRecover := "/run/ubuntu-seed"
	if err := mountFilesystem("ubuntu-seed", mntSysRecover); err != nil {
		return err
	}

	env := grubenv.NewEnv(path.Join(mntSysRecover, "EFI/ubuntu/grubenv"))
	if err := env.Load(); err != nil {
		return fmt.Errorf("cannot load recovery boot vars: %s", err)
	}

	if env.Get("snap_recovery_mode") != "" {
		env.Set("snap_recovery_mode", "")

		if err := env.Save(); err != nil {
			return fmt.Errorf("cannot save recovery boot vars: %s", err)
		}
	}

	if err := umount(mntSysRecover); err != nil {
		return fmt.Errorf("cannot unmount recovery: %s", err)
	}

	// Mount existing ubuntu-data

	mntRecovery := "/run/recovery"

	if err := mountFilesystem("ubuntu-data", mntRecovery); err != nil {
		return err
	}

	// shortcut: we can do something better than just bind-mount everything

	extrausers := "/var/lib/extrausers"
	recoveryExtrausers := path.Join(mntRecovery, "system-data", extrausers)
	err := exec.Command("mount", "-o", "bind", recoveryExtrausers, extrausers).Run()
	if err != nil {
		return fmt.Errorf("cannot mount extrausers: %s", err)
	}

	recoveryHome := path.Join(mntRecovery, "user-data")
	err = exec.Command("mount", "-o", "bind", recoveryHome, "/home").Run()
	if err != nil {
		return fmt.Errorf("cannot mount home: %s", err)
	}

	logger.Noticef("done")

	return nil
}

func Install(version string) error {

	mntWritable := "/run/ubuntu-data"
	mntSysRecover := "/run/ubuntu-seed"
	mntSystemBoot := "/run/ubuntu-boot"

	if err := mountFilesystem("ubuntu-boot", mntSystemBoot); err != nil {
		return err
	}

	time.Sleep(200 * time.Millisecond)

	cryptdev := "ubuntu-data"

	logger.Noticef("Create LUKS master key")
	keySize := 64
	keyBuffer := make([]byte, keySize)
	n, err := rand.Read(keyBuffer)
	if n != keySize || err != nil {
		return fmt.Errorf("cannot create LUKS key: %s", err)
	}

	logger.Noticef("Install recovery %s", version)
	if err := createWritable(keyBuffer, cryptdev); err != nil {
		logger.Noticef("cannot create data partition: %s", err)
		return err
	}

	time.Sleep(200 * time.Millisecond)

	if err := mountFilesystem(path.Join("/dev/mapper", cryptdev), mntWritable); err != nil {
		return err
	}

	if err := mountFilesystem("ubuntu-seed", mntSysRecover); err != nil {
		return err
	}

	// Copy selected recovery to the new writable and system-boot
	core, kernel, err := updateRecovery(mntWritable, mntSysRecover, mntSystemBoot, version)
	if err != nil {
		logger.Noticef("cannot populate writable: %s", err)
		return err
	}

	logger.Noticef("Create lockout authorization")
	lockoutAuth := make([]byte, 16)
	n, err = rand.Read(lockoutAuth)
	if n != 16 || err != nil {
		return fmt.Errorf("cannot create lockout authorization: %s", err)
	}

	logger.Noticef("Provisioning the TPM")

	tpm, err := fdeutil.ConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot acquire TPM context: %s", err)
	}
	defer tpm.Close()

	if err := fdeutil.ProvisionTPM(tpm, lockoutAuth); err != nil {
		logger.Noticef("error provisioning the TPM: %s", err)
		return fmt.Errorf("cannot provision TPM: %s", err)
	}

	logger.Noticef("Seal and store keyfile")
	if err := storeKeyfile(tpm, mntSystemBoot, keyBuffer); err != nil {
		return fmt.Errorf("cannot store keyfile: %s", err)
	}

	if err := storeLockoutAuth(mntWritable, lockoutAuth); err != nil {
		return fmt.Errorf("cannot store lockout authorization: %s", err)
	}

	syscall.Sync()

	if err := umount(mntWritable); err != nil {
		return err
	}

	// Update our bootloader
	if err := updateBootloader(mntSysRecover, core, kernel); err != nil {
		logger.Noticef("cannot update bootloader: %s", err)
		return err
	}

	if err := umount(mntSystemBoot); err != nil {
		return err
	}

	if err := umount(mntSysRecover); err != nil {
		return err
	}

	return nil
}

func createWritable(keyBuffer []byte, cryptdev string) error {
	logger.Noticef("Creating new ubuntu-data partition")
	disk := &DiskDevice{}
	if err := disk.FindFromPartLabel("ubuntu-boot"); err != nil {
		return err
	}

	// FIXME: get values from gadget, system
	err := disk.CreateLUKSPartition(1000*sizeMB, "ubuntu-data", keyBuffer, cryptdev)
	if err != nil {
		return err
	}

	return nil
}

func mountFilesystem(label string, mountpoint string) error {
	// Create mountpoint
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	// Mount filesystem
	if err := mount(label, mountpoint); err != nil {
		return err
	}

	return nil
}

func storeKeyfile(tpm *tpm2.TPMContext, dir string, buffer []byte) error {
	// Seal keyfile
	keyfile := path.Join(dir, "keyfile")
	if err := fdeutil.SealKeyToTPM(tpm, keyfile, fdeutil.Create, nil, buffer); err != nil {
		logger.Noticef("sealing failed: %s", err)
		return err
	}

	// Don't remove this sync, it prevents file corruption on vfat
	syscall.Sync()

	return nil
}

// The lockout authorization file is stored in the encrypted partition
func storeLockoutAuth(dir string, lockoutAuth []byte) error {
	return ioutil.WriteFile(path.Join(dir, "system-data/lockoutAuth"), lockoutAuth, 0400)
}

func updateRecovery(mntWritable, mntSysRecover, mntSystemBoot, version string) (core string, kernel string, err error) {
	logger.Noticef("Populating new writable")

	seedPath := "system-data/var/lib/snapd/seed"
	snapPath := "system-data/var/lib/snapd/snaps"

	srcRecoverySystem := path.Join(mntSysRecover, "systems", version)
	// FIXME: this is cheating, we simply write all snaps for now instead
	// of just the ones that belong to the recovery system
	srcSnaps := path.Join(mntSysRecover, "snaps")
	dest := path.Join(mntWritable, seedPath)

	// needed as mount-point (and for snapd.core-fixup.services)
	if err = os.MkdirAll(path.Join(mntWritable, "system-data/boot"), 0755); err != nil {
		return "", "", err
	}

	// remove all previous content of seed and snaps (if any)
	// this allow us to call this function to update our recovery version
	if err = os.RemoveAll(dest); err != nil {
		return "", "", err
	}
	if err = os.RemoveAll(path.Join(mntWritable, snapPath)); err != nil {
		return "", "", err
	}

	dirs := []string{seedPath, snapPath, "user-data"}
	if err = mkdirs(mntWritable, dirs, 0755); err != nil {
		return "", "", err
	}

	// cp -a $srcSnaps/*, $dest+"/snaps"
	srcSnapFiles, err := ioutil.ReadDir(srcSnaps)
	if err != nil {
		return "", "", err
	}
	err = os.MkdirAll(dest+"/snaps", 0755)
	if err != nil {
		return "", "", err
	}
	for _, f := range srcSnapFiles {
		if err = copyTree(path.Join(srcSnaps, f.Name()), dest+"/snaps"); err != nil {
			return "", "", err
		}
	}

	// cp -a $srcRecoverySystem $dest
	seedFiles, err := ioutil.ReadDir(srcRecoverySystem)
	if err != nil {
		return "", "", err
	}
	for _, f := range seedFiles {
		if err = copyTree(path.Join(srcRecoverySystem, f.Name()), dest); err != nil {
			return "", "", err
		}
	}

	seedDest := path.Join(dest, "snaps")

	core = path.Base(globFile(seedDest, "core20_*.snap"))
	kernel = path.Base(globFile(seedDest, "pc-kernel_*.snap"))

	logger.Noticef("core: %s", core)
	logger.Noticef("kernel: %s", kernel)

	coreSnapPath := path.Join(mntWritable, snapPath, core)
	err = os.Symlink(path.Join("../seed/snaps", core), coreSnapPath)
	if err != nil {
		return "", "", err
	}

	kernelSnapPath := path.Join(mntWritable, snapPath, kernel)
	err = os.Symlink(path.Join("../seed/snaps", kernel), kernelSnapPath)
	if err != nil {
		return "", "", err
	}

	err = extractKernel(kernelSnapPath, mntSystemBoot)

	return core, kernel, err
}

func extractKernel(kernelPath, mntSystemBoot string) error {
	mntKernelSnap := "/run/kernel-snap"
	if err := os.MkdirAll(mntKernelSnap, 0755); err != nil {
		return fmt.Errorf("cannot create kernel mountpoint: %s", err)
	}
	logger.Noticef("mounting %s", kernelPath)
	err := exec.Command("mount", "-t", "squashfs", kernelPath, mntKernelSnap).Run()
	if err != nil {
		return fmt.Errorf("cannot mount kernel snap: %s", err)
	}
	for _, img := range []string{"kernel.img", "initrd.img"} {
		logger.Noticef("copying %s to %s", img, mntSystemBoot)
		err := osutil.CopyFile(path.Join(mntKernelSnap, img), path.Join(mntSystemBoot, img), osutil.CopyFlagOverwrite)
		if err != nil {
			return fmt.Errorf("cannot copy %s: %s", img, err)
		}
	}

	// Don't remove this sync, it prevents file corruption on vfat
	syscall.Sync()

	if err := umount(mntKernelSnap); err != nil {
		return err
	}
	return nil
}

func updateBootloader(mntSysRecover, core, kernel string) error {
	b, err := bootloader.Find()
	if err != nil {
		return err
	}

	bootVars := map[string]string{
		"snap_core":          core,
		"snap_kernel":        kernel,
		"snap_recovery_mode": "",
	}

	if err := b.SetBootVars(bootVars); err != nil {
		return fmt.Errorf("cannot set boot vars: %s", err)
	}

	// FIXME: update recovery boot vars
	// must do it in a bootloader-agnostic way
	env := grubenv.NewEnv(path.Join(mntSysRecover, "EFI/ubuntu/grubenv"))
	if err := env.Load(); err != nil {
		return fmt.Errorf("cannot load recovery boot vars: %s", err)
	}
	env.Set("snap_recovery_mode", "")
	env.Set("snap_core", core)
	env.Set("snap_kernel", kernel)
	if err := env.Save(); err != nil {
		return fmt.Errorf("cannot save recovery boot vars: %s", err)
	}
	syscall.Sync()

	return nil
}

func enableSysrq() error {
	f, err := os.Open("/proc/sys/kernel/sysrq")
	if err != nil {
		return err
	}
	defer f.Close()
	f.Write([]byte("1\n"))
	return nil
}

func Restart() error {
	if err := enableSysrq(); err != nil {
		return err
	}

	f, err := os.OpenFile("/proc/sysrq-trigger", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Write([]byte("b\n"))

	// look away
	select {}
}
