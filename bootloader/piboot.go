// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package bootloader

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// sanity - piboot implements the required interfaces
var (
	_ Bootloader                             = (*piboot)(nil)
	_ ExtractedRecoveryKernelImageBootloader = (*piboot)(nil)
)

const (
	pibootCfgFilename = "piboot.conf"
	pibootPartFolder  = "/piboot/ubuntu/"
	// TODO Use boot.InitramfsUbuntuSeedDir
	// Need to solve circular dependency
	runMntDir     = "/run/mnt/"
	ubuntuSeedDir = runMntDir + "ubuntu-seed/"
)

type piboot struct {
	rootdir string
	basedir string
}

func (p *piboot) setDefaults() {
	p.basedir = "/boot/piboot/"
}

func (p *piboot) processBlOpts(blOpts *Options) {
	if blOpts == nil {
		return
	}

	switch {
	case blOpts.Role == RoleRecovery || blOpts.NoSlashBoot:
		if !blOpts.PrepareImageTime {
			p.rootdir = ubuntuSeedDir
		}
		// RoleRecovery or NoSlashBoot imply we use
		// the environment file in /piboot/ubuntu as
		// it exists on the partition directly
		p.basedir = pibootPartFolder
	}
}

// newPiboot creates a new Piboot bootloader object
func newPiboot(rootdir string, blOpts *Options) Bootloader {
	p := &piboot{
		rootdir: rootdir,
	}
	p.setDefaults()
	p.processBlOpts(blOpts)
	return p
}

func (p *piboot) Name() string {
	return "piboot"
}

func (p *piboot) dir() string {
	if p.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(p.rootdir, p.basedir)
}

func (p *piboot) envFile() string {
	return filepath.Join(p.dir(), pibootCfgFilename)
}

// piboot enabled if env file exists
func (p *piboot) Present() (bool, error) {
	return osutil.FileExists(p.envFile()), nil
}

// Variables stored in ubuntu-seed:
//   snapd_recovery_system
//   snapd_recovery_mode
//   snapd_recovery_kernel
// Variables stored in ubuntu-boot:
//   kernel_status
//   snap_kernel
//   snap_try_kernel
//   snapd_extra_cmdline_args
//   snapd_full_cmdline_args
//   recovery_system_status
//   try_recovery_system
func (p *piboot) SetBootVars(values map[string]string) error {
	env, err := ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return err
	}

	dirty := false
	needsReconfig := false
	pibootConfigureAllow := true
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		// "kernel_status" transitions from "try" to "trying"/"" are
		// coming from the initramfs (simulating what the bootloader
		// should do) and do not retrigger a configuration.
		if k == "kernel_status" && env.Get(k) == "try" {
			pibootConfigureAllow = false
		}
		env.Set(k, v)
		dirty = true
		// Cases that change the bootloader configuration
		if k == "snapd_recovery_mode" || k == "kernel_status" {
			needsReconfig = true
		}
		if k == "snap_try_kernel" && v == "" {
			// refresh (ok or not) finished, remove tryboot.txt
			// (os_prefix in config.txt will be changed now in
			// loadAndApplyConfig in the ok case)
			trybootPath := filepath.Join(ubuntuSeedDir, "tryboot.txt")
			if err := os.Remove(trybootPath); err != nil {
				logger.Noticef("cannot remove %s: %v", trybootPath, err)
			}
		}
	}

	if dirty {
		if err := env.Save(); err != nil {
			return err
		}
	}

	if needsReconfig && pibootConfigureAllow {
		if err := p.loadAndApplyConfig(env); err != nil {
			return err
		}
	}

	return nil
}

func (p *piboot) loadAndApplyConfig(env *ubootenv.Env) error {
	var prefix, cfgDir, dstDir string

	cfgFile := "config.txt"
	if env.Get("snapd_recovery_mode") == "run" {
		kernelSnap := env.Get("snap_kernel")
		kernStat := env.Get("kernel_status")
		if kernStat == "try" {
			// snap_try_kernel will be set when installing a new kernel
			kernelSnap = env.Get("snap_try_kernel")
			cfgFile = "tryboot.txt"
		}
		prefix = filepath.Join(pibootPartFolder, kernelSnap)
		cfgDir = ubuntuSeedDir
		dstDir = filepath.Join(ubuntuSeedDir, prefix)
	} else {
		// install/recovery modes, use recovery kernel
		prefix = filepath.Join("/systems", env.Get("snapd_recovery_system"),
			"kernel")
		cfgDir = p.rootdir
		dstDir = filepath.Join(p.rootdir, prefix)
	}

	logger.Debugf("configure piboot %s with prefix %q, cfgDir %q, dstDir %q",
		cfgFile, prefix, cfgDir, dstDir)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	return p.applyConfig(env, cfgFile, prefix, cfgDir, dstDir)
}

// Sets os_prefix in RPi config.txt or tryboot.txt
func (p *piboot) setRPiCfgOsPrefix(prefix, inFile, outFile string) error {
	buf, err := ioutil.ReadFile(inFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(buf), "\n")

	replaced := false
	newOsPrefix := "os_prefix=" + prefix + "/"
	for i, line := range lines {
		if strings.HasPrefix(line, "os_prefix=") {
			lines[i] = newOsPrefix
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, newOsPrefix)
		lines = append(lines, "")
	}

	output := strings.Join(lines, "\n")
	return osutil.AtomicWriteFile(outFile, []byte(output), 0644, 0)
}

func (p *piboot) createCmdline(env *ubootenv.Env, defaultsFile, outFile string) error {
	var base string
	fullCmdline := env.Get("snapd_full_cmdline_args")
	if fullCmdline != "" {
		base = fullCmdline
	} else {
		buf, err := ioutil.ReadFile(defaultsFile)
		if err != nil {
			return err
		}

		lines := strings.Split(string(buf), "\n")
		base = lines[0]
		base += " " + env.Get("snapd_extra_cmdline_args")
	}

	mode := env.Get("snapd_recovery_mode")
	cmdline := base + " snapd_recovery_mode=" + mode
	if mode != "run" {
		cmdline += " snapd_recovery_system=" + env.Get("snapd_recovery_system")
	}
	// Signal when we are trying a new kernel
	kernelStatus := env.Get("kernel_status")
	if kernelStatus == "try" {
		cmdline += " kernel_status=trying"
	}
	cmdline += "\n"

	logger.Debugf("writing kernel command line to %s", outFile)

	return osutil.AtomicWriteFile(outFile, []byte(cmdline), 0644, 0)
}

// Configure pi bootloader with a given os_prefix. cfgDir contains the
// config files, and dstDir is where we will place the kernel command
// line.
func (p *piboot) applyConfig(env *ubootenv.Env,
	configFile, prefix, cfgDir, dstDir string) error {

	kernStat := env.Get("kernel_status")
	cmdlineFile := filepath.Join(dstDir, "cmdline.txt")
	// These two files are part of the gadget
	templCmdlineFile := filepath.Join(cfgDir, "cmdline.templ.txt")
	templConfigFile := filepath.Join(cfgDir, "config.templ.txt")

	if err := p.createCmdline(env, templCmdlineFile, cmdlineFile); err != nil {
		return err
	}
	if err := p.setRPiCfgOsPrefix(prefix, templConfigFile,
		filepath.Join(cfgDir, configFile)); err != nil {
		return err
	}

	if kernStat == "try" {
		// The reboot parameter makes sure we use tryboot.cfg config
		// TODO Maybe this should be done somewhere else in snapd?
		if err := osutil.AtomicWriteFile("/run/systemd/reboot-param",
			[]byte("0 tryboot\n"), 0644, 0); err != nil {
			return err
		}
	}

	return nil
}

func (p *piboot) GetBootVars(names ...string) (map[string]string, error) {
	out := make(map[string]string)

	env, err := ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (p *piboot) InstallBootConfig(gadgetDir string, blOpts *Options) error {
	gadgetFile := filepath.Join(gadgetDir, pibootCfgFilename)
	st, err := os.Stat(gadgetFile)
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		// We create a valid env file if the one from gadget is empty
		err := os.MkdirAll(filepath.Dir(p.envFile()), 0755)
		if err != nil {
			return err
		}

		// TODO: what's a reasonable size for this file?
		env, err := ubootenv.Create(p.envFile(), 4096)
		if err != nil {
			return err
		}

		if err := env.Save(); err != nil {
			return nil
		}

		return nil
	}

	systemFile := p.envFile()
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (p *piboot) layoutKernelAssetsToDir(snapf snap.Container, dstDir string) error {
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	if err := extractKernelAssetsToBootDir(dstDir, snapf, assets); err != nil {
		return err
	}

	bcomFiles := filepath.Join(dstDir, "dtbs/broadcom/*")
	if output, err := exec.Command("sh", "-c",
		"mv "+bcomFiles+" "+dstDir).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot move RPi dtbs to %s:\n%s",
			dstDir, output)
	}
	overlaysDir := filepath.Join(dstDir, "dtbs/overlays/")
	newOvDir := filepath.Join(dstDir, "overlays/")
	if err := os.Rename(overlaysDir, newOvDir); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	// README file is needed so os_prefix is honored for overlays. See
	// https://www.raspberrypi.com/documentation/computers/config_txt.html#os_prefix
	readmeOverlays, err := os.Create(filepath.Join(dstDir, "overlays", "README"))
	if err != nil {
		return err
	}
	readmeOverlays.Close()
	return nil
}

func (p *piboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	// Rootdir will point to ubuntu-boot, but we need to put things in ubuntu-seed
	dstDir := filepath.Join(ubuntuSeedDir, pibootPartFolder, s.Filename())

	logger.Debugf("ExtractKernelAssets to %s", dstDir)

	return p.layoutKernelAssetsToDir(snapf, dstDir)
}

func (p *piboot) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo,
	snapf snap.Container) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}

	recoveryKernelAssetsDir :=
		filepath.Join(p.rootdir, recoverySystemDir, "kernel")
	logger.Debugf("ExtractRecoveryKernelAssets to %s", recoveryKernelAssetsDir)

	return p.layoutKernelAssetsToDir(snapf, recoveryKernelAssetsDir)
}

func (p *piboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(
		filepath.Join(ubuntuSeedDir, pibootPartFolder), s)
}
