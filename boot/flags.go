// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package boot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var (
	errNotUC20 = fmt.Errorf("cannot get boot flags on pre-UC20 device")

	understoodBootFlags = []string{
		// the factory boot flag is set to indicate that this is a
		// boot inside a factory environment
		"factory",
	}
)

type unknownFlagError string

func (e unknownFlagError) Error() string {
	return string(e)
}

func IsUnknownBootFlagError(e error) bool {
	_, ok := e.(unknownFlagError)
	return ok
}

// splitBootFlagString splits the given comma delimited list of boot flags, removing
// empty strings.
// Note that this explicitly does not filter out unsupported boot flags in the
// off chance that an old version of the initramfs is reading new boot flags
// written by a new version of snapd in userspace on a previous boot.
func splitBootFlagString(s string) []string {
	flags := []string{}
	for _, flag := range strings.Split(s, ",") {
		if flag != "" {
			flags = append(flags, flag)
		}
	}

	return flags
}

func checkBootFlagList(flags []string, allowList []string) ([]string, error) {
	allowedFlags := make([]string, 0, len(flags))
	disallowedFlags := make([]string, 0, len(flags))
	if len(allowList) != 0 {
		// then we need to enforce the allow list
		for _, flag := range flags {
			if strutil.ListContains(allowList, flag) {
				allowedFlags = append(allowedFlags, flag)
			} else {
				if flag == "" {
					// this is to make it more obvious
					disallowedFlags = append(disallowedFlags, `""`)
				} else {
					disallowedFlags = append(disallowedFlags, flag)
				}
			}
		}
	}
	if len(allowedFlags) != len(flags) {
		return allowedFlags, unknownFlagError(fmt.Sprintf("unknown boot flags %v not allowed", disallowedFlags))
	}
	return flags, nil
}

func serializeBootFlags(flags []string) string {
	// drop empty strings before serializing
	nonEmptyFlags := make([]string, 0, len(flags))
	for _, flag := range flags {
		if strings.TrimSpace(flag) != "" {
			nonEmptyFlags = append(nonEmptyFlags, flag)
		}
	}

	return strings.Join(nonEmptyFlags, ",")
}

// setImageBootFlags sets the provided flags in the provided
// bootenv-representing map. It first checks them.
func setImageBootFlags(flags []string, blVars map[string]string) error {
	// check that the flagList is supported
	if _, err := checkBootFlagList(flags, understoodBootFlags); err != nil {
		return err
	}

	// also ensure that the serialized value of the boot flags fits inside the
	// bootenv value, on lk systems the max size of a bootenv value is 255 chars
	s := serializeBootFlags(flags)
	if len(s) > 254 {
		return fmt.Errorf("internal error: boot flags too large to fit inside bootenv value")
	}

	blVars["snapd_boot_flags"] = s
	return nil
}

// InitramfsActiveBootFlags returns the set of boot flags that are currently set
// for the current boot, by querying them directly from the source. This method
// is only meant to be used from the initramfs, since it may query the bootenv
// or query the modeenv depending on the current mode of the system.
// For detecting the current set of boot flags outside of the initramfs, use
// BootFlags(), which will query for the runtime version of the flags in /run
// that the initramfs will have setup for userspace.
// Note that no filtering is done on the flags in order to allow new flags to be
// used by a userspace that is newer than the initramfs, but empty flags will be
// dropped automatically.
// Only to be used on UC20+ systems with recovery systems.
func InitramfsActiveBootFlags(mode string, rootfsDir string) ([]string, error) {
	switch mode {
	case ModeRecover:
		// no boot flags are consumed / used on recover mode, so return nothing
		return nil, nil

	case ModeRunCVM:
		// no boot flags are consumed / used on CVM mode, so return nothing
		return nil, nil

	case ModeRun:
		// boot flags come from the modeenv
		modeenv, err := ReadModeenv(rootfsDir)
		if err != nil {
			return nil, err
		}

		// TODO: consider passing in the modeenv or returning the modeenv here
		// to reduce the number of times we read the modeenv ?
		return modeenv.BootFlags, nil

	case ModeFactoryReset:
		// Reuse the code from ModeInstall as we have a lot of
		// identical conditions.
		fallthrough
	case ModeInstall:
		// boot flags always come from the bootenv of the recovery bootloader
		// in install mode
		return readBootFlagsFromRecoveryBootloader()

	default:
		return nil, fmt.Errorf("internal error: unsupported mode %q", mode)
	}
}

func readBootFlagsFromRecoveryBootloader() ([]string, error) {
	opts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return nil, err
	}

	m, err := bl.GetBootVars("snapd_boot_flags")
	if err != nil {
		return nil, err
	}

	return splitBootFlagString(m["snapd_boot_flags"]), nil
}

// InitramfsExposeBootFlagsForSystem sets the boot flags for the current boot in
// the /run file that will be consulted in userspace by BootFlags() below. It is
// meant to be used only from the initramfs.
// Note that no filtering is done on the flags in order to allow new flags to be
// used by a userspace that is newer than the initramfs, but empty flags will be
// dropped automatically.
// Only to be used on UC20+ systems with recovery systems.
func InitramfsExposeBootFlagsForSystem(flags []string) error {
	s := serializeBootFlags(flags)

	if err := os.MkdirAll(filepath.Dir(snapBootFlagsFile), 0755); err != nil {
		return err
	}

	return os.WriteFile(snapBootFlagsFile, []byte(s), 0644)
}

// BootFlags returns the current set of boot flags active for this boot. It uses
// the initramfs-capture values in /run. The flags from the initramfs are
// checked against the currently understood set of flags, so that if there are
// unrecognized flags, they are removed from the returned list and the returned
// error will have IsUnknownFlagErroror() return true. This is to allow gracefully
// ignoring unknown boot flags while still processing supported flags.
// Only to be used on UC20+ systems with recovery systems.
func BootFlags(dev snap.Device) ([]string, error) {
	if !dev.HasModeenv() {
		return nil, errNotUC20
	}

	// read the file that the initramfs wrote in /run, we don't use the modeenv
	// or bootenv to avoid ambiguity about whether the flags in the modeenv or
	// bootenv are for this boot or the next one, but the initramfs will always
	// copy the flags that were set into /run, so we always know the current
	// boot's flags are written in /run
	b, err := os.ReadFile(snapBootFlagsFile)
	if err != nil {
		return nil, err
	}

	flags := splitBootFlagString(string(b))
	if allowFlags, err := checkBootFlagList(flags, understoodBootFlags); err != nil {
		if e, ok := err.(unknownFlagError); ok {
			return allowFlags, e
		}
		return nil, err
	}
	return flags, nil
}

// nextBootFlags returns the set of boot flags that are applicable for the next
// boot. This information always comes from the modeenv, since the only
// situation where boot flags are set for the next boot and we query their state
// is during run mode. The next boot flags for install mode are not queried
// during prepare-image time, since they are only written to the bootenv at
// prepare-image time.
// Only to be used on UC20+ systems with recovery systems.
// TODO: should this accept a modeenv that was previously read from i.e.
// devicestate manager?
func nextBootFlags(dev snap.Device) ([]string, error) {
	if !dev.HasModeenv() {
		return nil, errNotUC20
	}

	m, err := ReadModeenv("")
	if err != nil {
		return nil, err
	}

	return m.BootFlags, nil
}

// setNextBootFlags sets the boot flags for the next boot to take effect after
// rebooting. This information always gets saved to the modeenv.
// Only to be used on UC20+ systems with recovery systems.
func setNextBootFlags(dev snap.Device, rootDir string, flags []string) error {
	if !dev.HasModeenv() {
		return errNotUC20
	}

	// XXX take the modeenv lock?

	m, err := ReadModeenv(rootDir)
	if err != nil {
		return err
	}

	// for run time, enforce the allow list so we don't write unsupported boot
	// flags
	if _, err := checkBootFlagList(flags, understoodBootFlags); err != nil {
		return err
	}

	m.BootFlags = flags

	return m.Write()
}

// HostUbuntuDataForMode returns a list of locations where the run
// mode root filesystem is mounted for the given mode.
// For run mode, it's "/run/mnt/data" and "/".
// For install mode it's "/run/mnt/ubuntu-data".
// For factory-reset mode it's "/run/mnt/ubuntu-data"
// For recover mode it's either "/host/ubuntu-data" or nil if that is not
// mounted. Note that, for recover mode, this function only returns a non-empty
// return value if the partition is mounted and trusted, there are certain
// corner-cases where snap-bootstrap in the initramfs may have mounted
// ubuntu-data in an untrusted manner, but for the purposes of this function
// that is ignored.
// This is primarily meant to be consumed by "snap{,ctl} system-mode".
//
// TODO: pass a "snap.Device" here and add "SystemMode() string" to that
func HostUbuntuDataForMode(mode string, mod gadget.Model) ([]string, error) {
	var runDataRootfsMountLocations []string
	switch mode {
	case ModeRun:
		// in run mode we have both /run/mnt/data and "/"
		runDataRootfsMountLocations = []string{InitramfsDataDir, dirs.GlobalRootDir}
	case ModeRecover:
		// TODO: should this be it's own dedicated helper to read degraded.json?

		// for recover mode, the source of truth to determine if we have the
		// host mount is snap-bootstrap's /run/snapd/snap-bootstrap/degraded.json, so
		// we have to go parse that
		degradedJSONFile := filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json")
		b, err := os.ReadFile(degradedJSONFile)
		if err != nil {
			return nil, err
		}

		degradedJSON := struct {
			UbuntuData struct {
				MountState    string `json:"mount-state"`
				MountLocation string `json:"mount-location"`
			} `json:"ubuntu-data"`
		}{}

		err = json.Unmarshal(b, &degradedJSON)
		if err != nil {
			return nil, err
		}

		// don't permit mounted-untrusted state, only mounted state is allowed
		if degradedJSON.UbuntuData.MountState == "mounted" {
			runDataRootfsMountLocations = []string{degradedJSON.UbuntuData.MountLocation}
		}
		// otherwise leave it empty

	case ModeInstall:
		// On *Core* the var we have is
		// /run/mnt/ubuntu-data/writable, but the caller
		// probably wants /run/mnt/ubuntu-data there. For classic
		// the dir is /run/mnt/ubuntu-data already

		// note that we may be running in install mode before this directory is
		// actually created so check if it exists first
		var installModeLocation string
		if mod.Classic() {
			installModeLocation = InstallHostWritableDir(mod)
		} else {
			installModeLocation = filepath.Dir(InstallHostWritableDir(mod))
		}
		if exists, _, _ := osutil.DirExists(installModeLocation); exists {
			runDataRootfsMountLocations = []string{installModeLocation}
		}

	case ModeFactoryReset:
		// In factory reset, our conditions are a lot similar to install mode,
		// as we recreate the ubuntu-data partition. Make similar assumptions
		// and checks like ModeInstall. Take into account ubuntu-data might not
		// be mounted when this check is called.
		var factoryResetModeLocation string
		if mod.Classic() {
			factoryResetModeLocation = InstallHostWritableDir(mod)
		} else {
			factoryResetModeLocation = filepath.Dir(InstallHostWritableDir(mod))
		}
		if exists, _, _ := osutil.DirExists(factoryResetModeLocation); exists {
			runDataRootfsMountLocations = []string{factoryResetModeLocation}
		}
	default:
		return nil, ErrUnsupportedSystemMode
	}

	return runDataRootfsMountLocations, nil
}
