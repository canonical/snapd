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
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// ensure piboot implements the required interfaces
var (
	_ Bootloader                             = (*piboot)(nil)
	_ ExtractedRecoveryKernelImageBootloader = (*piboot)(nil)
	_ NotScriptableBootloader                = (*piboot)(nil)
	_ RebootBootloader                       = (*piboot)(nil)
)

const (
	pibootCfgFilename = "piboot.conf"
	pibootPartFolder  = "/piboot/ubuntu/"
)

// TODO The ubuntu-seed folder should be eventually passed around when
// creating the bootloader.
// This is in a variable so it can be mocked in tests
var ubuntuSeedDir = "/run/mnt/ubuntu-seed/"

// More variables to facilitate mocking
var (
	rpi4RevisionCodesPath   = "/sys/firmware/devicetree/base/system/linux,revision"
	rpi4EepromTimeStampPath = "/proc/device-tree/chosen/bootloader/build-timestamp"
)

type piboot struct {
	rootdir          string
	basedir          string
	prepareImageTime bool
}

func (p *piboot) setDefaults() {
	p.basedir = "/boot/piboot/"
}

func (p *piboot) processBlOpts(blOpts *Options) {
	if blOpts == nil {
		return
	}

	p.prepareImageTime = blOpts.PrepareImageTime
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
//
//	snapd_recovery_system
//	snapd_recovery_mode
//	snapd_recovery_kernel
//
// Variables stored in ubuntu-boot:
//
//	kernel_status
//	snap_kernel
//	snap_try_kernel
//	snapd_extra_cmdline_args
//	snapd_full_cmdline_args
//	recovery_system_status
//	try_recovery_system
func (p *piboot) SetBootVars(values map[string]string) error {
	env := mylog.Check2(ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort))

	// Set when we change a boot env variable, to know if we need to save the env
	dirtyEnv := false
	// Flag to know if we need to write piboot's config.txt or tryboot.txt
	reconfigBootloader := false
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirtyEnv = true
		// Cases that change the bootloader configuration
		if k == "snapd_recovery_mode" || k == "kernel_status" {
			reconfigBootloader = true
		}
		if k == "snap_try_kernel" && v == "" {
			// Refresh (ok or not) finished, remove tryboot.txt.
			// os_prefix in config.txt will be changed now in
			// loadAndApplyConfig in the ok case. Note that removing
			// it is safe as tryboot.txt is used only when a special
			// volatile boot flag is set, so we always have a valid
			// config.txt that will allow booting.
			trybootPath := filepath.Join(ubuntuSeedDir, "tryboot.txt")
			mylog.Check(os.Remove(trybootPath))

		}
	}

	if dirtyEnv {
		mylog.Check(env.Save())
	}

	if reconfigBootloader {
		mylog.Check(p.loadAndApplyConfig(env))
	}

	return nil
}

func (p *piboot) SetBootVarsFromInitramfs(values map[string]string) error {
	env := mylog.Check2(ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort))

	dirtyEnv := false
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirtyEnv = true
	}

	if dirtyEnv {
		mylog.Check(env.Save())
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
	mylog.Check(os.MkdirAll(dstDir, 0755))

	return p.applyConfig(env, cfgFile, prefix, cfgDir, dstDir)
}

// Writes os_prefix in RPi config.txt or tryboot.txt
func (p *piboot) writeRPiCfgWithOsPrefix(prefix, inFile, outFile string) error {
	buf := mylog.Check2(os.ReadFile(inFile))

	lines := strings.Split(string(buf), "\n")

	replaced := false
	newOsPrefix := "os_prefix=" + prefix + "/"
	for i, line := range lines {
		if strings.HasPrefix(line, "os_prefix=") {
			if replaced {
				logger.Noticef("unexpected extra os_prefix line: %q", line)
				lines[i] = "# " + lines[i]
				continue
			}
			lines[i] = newOsPrefix
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, newOsPrefix)
		lines = append(lines, "")
	}

	output := strings.Join(lines, "\n")
	return osutil.AtomicWriteFile(outFile, []byte(output), 0644, 0)
}

func (p *piboot) writeCmdline(env *ubootenv.Env, defaultsFile, outFile string) error {
	buf := mylog.Check2(os.ReadFile(defaultsFile))

	lines := strings.Split(string(buf), "\n")
	cmdline := lines[0]

	mode := env.Get("snapd_recovery_mode")
	cmdline += " snapd_recovery_mode=" + mode
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
	configFile, prefix, cfgDir, dstDir string,
) error {
	cmdlineFile := filepath.Join(dstDir, "cmdline.txt")
	refCmdlineFile := filepath.Join(cfgDir, "cmdline.txt")
	currentConfigFile := filepath.Join(cfgDir, "config.txt")
	mylog.Check(p.writeCmdline(env, refCmdlineFile, cmdlineFile))
	mylog.Check(p.writeRPiCfgWithOsPrefix(prefix, currentConfigFile,
		filepath.Join(cfgDir, configFile)))

	return nil
}

func (p *piboot) GetBootVars(names ...string) (map[string]string, error) {
	env := mylog.Check2(ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort))

	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (p *piboot) InstallBootConfig(gadgetDir string, blOpts *Options) error {
	mylog.
		// We create an empty env file
		Check(os.MkdirAll(filepath.Dir(p.envFile()), 0755))

	// TODO: what's a reasonable size for this file?
	env := mylog.Check2(ubootenv.Create(p.envFile(), 4096, ubootenv.CreateOptions{HeaderFlagByte: true}))

	return env.Save()
}

func (p *piboot) layoutKernelAssetsToDir(snapf snap.Container, dstDir string) error {
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	mylog.Check(extractKernelAssetsToBootDir(dstDir, snapf, assets))

	// remove subdirs so mv does not complain about non-empty dirs
	// if extraction happens multiple times
	newOvDir := filepath.Join(dstDir, "overlays/")
	mylog.Check(os.RemoveAll(newOvDir))

	// armhf and arm64 pi-kernel store dtbs in different places
	// (dtbs/ or dtbs/broadcom/ respectively)
	var dtbDir string
	if _, isDir, _ := osutil.DirExists(filepath.Join(dstDir, "dtbs/broadcom")); isDir {
		dtbDir = "dtbs/broadcom"
		overlaysDir := filepath.Join(dstDir, "dtbs/overlays/")
		mylog.Check(os.Rename(overlaysDir, newOvDir))

	} else {
		dtbDir = "dtbs"
	}

	dtbFiles := filepath.Join(dstDir, dtbDir, "*")
	if output := mylog.Check2(exec.Command("sh", "-c",
		"mv "+dtbFiles+" "+dstDir).CombinedOutput()); err != nil {
		return fmt.Errorf("cannot move RPi dtbs to %s:\n%s",
			dstDir, output)
	}

	// README file is needed so os_prefix is honored for overlays. See
	// https://www.raspberrypi.com/documentation/computers/config_txt.html#os_prefix
	readmeOverlays := mylog.Check2(os.Create(filepath.Join(dstDir, "overlays", "README")))

	readmeOverlays.Close()
	return nil
}

func (p *piboot) eepromVersionSupportsTryboot() (bool, error) {
	// To find out the EEPROM version we do the same as the
	// rpi-eeprom-update script (see
	// https://github.com/raspberrypi/rpi-eeprom/blob/master/rpi-eeprom-update)
	buf := mylog.Check2(os.ReadFile(rpi4EepromTimeStampPath))

	// The timestamp is seconds since the epoch, UTC time
	eepromTs := binary.BigEndian.Uint32(buf)
	// 2021-03-18 or more modern supports tryboot, see
	// https://github.com/raspberrypi/rpi-eeprom/blob/master/firmware/release-notes.md#2021-04-19---promote-2021-03-18-from-latest-to-default---default
	// The timestamp we compare with (1616057651 seconds since the epoch,
	// which is jue 18 mar 2021 08:54:11 UTC) can be found with:
	// $ strings pieeprom-2021-03-18.bin | grep BUILD_TIMESTAMP
	return eepromTs >= 1616057651, nil
}

func (p *piboot) isRaspberryPi4() bool {
	// For RPi4 detection we do the same as the rpi-eeprom-update script (see
	// https://github.com/raspberrypi/rpi-eeprom/blob/master/rpi-eeprom-update)
	buf := mylog.Check2(os.ReadFile(rpi4RevisionCodesPath))

	// This is an RPi4 if we have new style codes (RPi2 or newer) and the
	// processor is BCM2711 (RPi4's SoC). For details, see
	// https://www.raspberrypi.com/documentation/computers/raspberry-pi.html#raspberry-pi-revision-codes
	boardInfo := binary.BigEndian.Uint32(buf)
	return ((boardInfo>>23)&1) == 1 && ((boardInfo>>12)&0xF) == 3
}

func (p *piboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	if !p.prepareImageTime {
		// If this is an RPi4, check first if EEPROM supports tryboot
		if p.isRaspberryPi4() {
			supportsTryboot := mylog.Check2(p.eepromVersionSupportsTryboot())

			if !supportsTryboot {
				return fmt.Errorf("your EEPROM does not support tryboot, please upgrade to a newer one before installing Ubuntu Core - see http://forum.snapcraft.io/t/29455 for more details")
			}
		}
	}

	// Rootdir will point to ubuntu-boot, but we need to put things in ubuntu-seed
	dstDir := filepath.Join(ubuntuSeedDir, pibootPartFolder, s.Filename())

	logger.Debugf("ExtractKernelAssets to %s", dstDir)

	return p.layoutKernelAssetsToDir(snapf, dstDir)
}

func (p *piboot) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo,
	snapf snap.Container,
) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}

	recoveryKernelAssetsDir := filepath.Join(p.rootdir, recoverySystemDir, "kernel")
	logger.Debugf("ExtractRecoveryKernelAssets to %s", recoveryKernelAssetsDir)

	return p.layoutKernelAssetsToDir(snapf, recoveryKernelAssetsDir)
}

func (p *piboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(
		filepath.Join(ubuntuSeedDir, pibootPartFolder), s)
}

func (p *piboot) GetRebootArguments() (string, error) {
	env := mylog.Check2(ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort))

	kernStat := env.Get("kernel_status")
	if kernStat == "try" {
		// The reboot parameter makes sure we use tryboot.cfg config
		return "0 tryboot", nil
	}

	return "", nil
}
