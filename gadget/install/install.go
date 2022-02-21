// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/timings"
	secbootcore "github.com/snapcore/secboot"
)

// diskWithSystemSeed will locate a disk that has the partition corresponding
// to a structure with SystemSeed role of the specified gadget volume and return
// the device node.
func diskWithSystemSeed(lv *gadget.LaidOutVolume) (device string, err error) {
	for _, vs := range lv.LaidOutStructure {
		// XXX: this part of the finding maybe should be a
		// method on gadget.*Volume
		if vs.Role == gadget.SystemSeed {
			device, err = gadget.FindDeviceForStructure(&vs)
			if err != nil {
				return "", fmt.Errorf("cannot find device for role system-seed: %v", err)
			}

			disk, err := disks.DiskFromPartitionDeviceNode(device)
			if err != nil {
				return "", err
			}
			return disk.KernelDeviceNode(), nil
		}
	}
	return "", fmt.Errorf("cannot find role system-seed in gadget")
}

func roleOrLabelOrName(part gadget.OnDiskStructure) string {
	switch {
	case part.Role != "":
		return part.Role
	case part.Label != "":
		return part.Label
	case part.Name != "":
		return part.Name
	default:
		return "unknown"
	}
}

type repartSummary []repartSummaryDisk

type repartSummaryDisk struct {
	Type       string `json:"type"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	File       string `json:"file"`
	Node       string `json:"node"`
	Offset     uint64 `json:"offset"`
	OldSize    uint64 `json:"old_size"`
	RawSize    uint64 `json:"raw_size"`
	OldPadding uint64 `json:"old_padding"`
	RawPadding uint64 `json:"raw_padding"`
	Activity   string `json:"activity"`
}

func RunWithSystemdRepart(model gadget.Model, gadgetRoot, device string, options Options, observer gadget.ContentObserver) (*InstalledSystemSideData, error) {
	info, err := gadget.ReadInfoAndValidate(gadgetRoot, model, nil)

	var system_volume *gadget.Volume
	for _, vol := range info.Volumes {
		for _, structure := range vol.Structure {
			if structure.Role == gadget.SystemBoot {
				system_volume = vol
			}
		}
	}
	if system_volume == nil {
		return nil, fmt.Errorf("System volume not found")
	}

	if device == "" {
		disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuSeedDir, nil)
		if err != nil {
			return nil, err
		}
		device = disk.KernelDeviceNode()
	}

	encrypt := options.EncryptionType != secboot.EncryptionTypeNone
	err = gadget.GenerateRepartConfig(gadgetRoot, system_volume, options.EncryptionType != secboot.EncryptionTypeNone, observer)

	labelRole := make(map[string]string)
	labelFS := make(map[string]string)
	labelLabel := make(map[string]string)
	for _, struc := range system_volume.Structure {
		label := struc.Label
		if struc.Label == "" {
			label = struc.Name
		}
		if label == "" {
			continue
		}
		if encrypt && (struc.Role == "system-data" || struc.Role == "system-save") {
			label = fmt.Sprintf("%v-enc", label)
		}
		labelRole[label] = struc.Role
		labelFS[label] = struc.Filesystem
		labelLabel[label] = struc.Label
	}
	if err != nil {
		return nil, err
	}

	var temporaryKey secboot.EncryptionKey
	if encrypt {
		temporaryKey, err = secboot.NewEncryptionKey()
		if err != nil {
			return nil, err
		}
	}
	args := []string{"--json=short", "--definitions=/run/repart.d", "--dry-run=no"}
	if encrypt {
		args = append(args, "--key-file=/proc/self/fd/3")
	}
	if device != "" {
		args = append(args, device)
	}
	logger.Noticef("Calling: systemd-repart %v", strings.Join(args, " "))
	cmd := exec.Command("systemd-repart", args...)

	var keyRead *os.File
	var keyWrite *os.File
	if encrypt {
		keyRead, keyWrite, err = os.Pipe()
		defer keyRead.Close()
		defer keyWrite.Close()
		if err != nil {
			return nil, err
		}
		cmd.ExtraFiles = []*os.File{keyRead}
	}
	var output bytes.Buffer
	var errors bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &errors
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	if encrypt {
		keyRead.Close()
		// TODO: synchronize
		go func () {
			_, err = keyWrite.Write(temporaryKey)
			if err != nil {
				logger.Noticef("Error writing the key")
			}
			keyWrite.Close()
		}()
	}
	err = cmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("systemd-repart failed: %v\n%v", err, errors.String())
	}

	var dump repartSummary
	if err := json.Unmarshal(output.Bytes(), &dump); err != nil {
		return nil, fmt.Errorf("Cannot parse systemd-repart output: %v", err)
	}

	var keysForRoles map[string]*EncryptionKeySet

	if encrypt {
		for _, part := range dump {
			if part.Activity != "create" {
				continue
			}
			role, hasRole := labelRole[part.Label]
			if hasRole && (role == "system-data" || role == "system-save") {
				key, err := secboot.NewEncryptionKey()
				if err != nil {
					return nil, err
				}
				recoveryKey, err := secboot.NewRecoveryKey()
				if err != nil {
					return nil, err
				}
				err = secboot.AddRecoveryKey(temporaryKey, recoveryKey, part.Node)
				if err != nil {
					return nil, err
				}
				err = secbootcore.ChangeLUKS2KeyUsingRecoveryKey(part.Node, secbootcore.RecoveryKey(recoveryKey), key)
				if err != nil {
					return nil, err
				}
				if keysForRoles == nil {
					keysForRoles = map[string]*EncryptionKeySet{}
				}

				keysForRoles[role] = &EncryptionKeySet{
					Key:         key,
					RecoveryKey: recoveryKey,
				}
			}
		}
	}

	for _, part := range dump {
		if part.Activity != "create" {
			continue
		}

		node := part.Node

		role, hasRole := labelRole[part.Label]
		if hasRole && keysForRoles != nil {
			keys, hasKeys := keysForRoles[role]
			if hasKeys {
				cmd := exec.Command("cryptsetup", "open", "--key-file", "-", part.Node, part.Label)
				cmd.Stdin = bytes.NewReader(keys.Key[:])
				if output, err := cmd.CombinedOutput(); err != nil {
					logger.Noticef("cryptsetup: %v", output)
					return nil, err
				}
				node = fmt.Sprintf("/dev/mapper/%s", part.Label)
			}
		}

		FS, hasFS := labelFS[part.Label]
		label, hasLabel := labelLabel[part.Label]
		if options.Mount && hasFS && hasLabel {
			mountpoint := filepath.Join(boot.InitramfsRunMntDir, label)
			if err := os.MkdirAll(mountpoint, 0755); err != nil {
				return nil, fmt.Errorf("cannot create mountpoint: %v", err)
			}
			err = os.MkdirAll(mountpoint, 0o777)
			if err != nil {
				return nil, err
			}
			err = sysMount(node, mountpoint, FS, 0, "")
			if err != nil {
				return nil, err
			}
		}
	}

	return &InstalledSystemSideData{
		KeysForRoles: keysForRoles,
	}, nil
}

// Run bootstraps the partitions of a device, by either creating
// missing ones or recreating installed ones.
func Run(model gadget.Model, gadgetRoot, kernelRoot, device string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	logger.Noticef("installing a new system")
	logger.Noticef("        gadget data from: %v", gadgetRoot)
	logger.Noticef("        encryption: %v", options.EncryptionType)

	ret, err := RunWithSystemdRepart(model, gadgetRoot, device, options, observer)
	if err == nil {
		return ret, nil
	} else {
		logger.Noticef("Using systemd-repart failed: %v", err)
	}

	if gadgetRoot == "" {
		return nil, fmt.Errorf("cannot use empty gadget root directory")
	}

	if model.Grade() == asserts.ModelGradeUnset {
		return nil, fmt.Errorf("cannot run install mode on non-UC20+ system")
	}

	lv, _, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model)
	if err != nil {
		return nil, fmt.Errorf("cannot layout the volume: %v", err)
	}
	// TODO: resolve content paths from gadget here

	// XXX: the only situation where auto-detect is not desired is
	//      in (spread) testing - consider to remove forcing a device
	//
	// auto-detect device if no device is forced
	if device == "" {
		device, err = diskWithSystemSeed(lv)
		if err != nil {
			return nil, fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(device)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", device, err)
	}

	// check if the current partition table is compatible with the gadget,
	// ignoring partitions added by the installer (will be removed later)
	if err := gadget.EnsureLayoutCompatibility(lv, diskLayout, nil); err != nil {
		return nil, fmt.Errorf("gadget and %v partition table not compatible: %v", device, err)
	}

	// remove partitions added during a previous install attempt
	if err := removeCreatedPartitions(gadgetRoot, lv, diskLayout); err != nil {
		return nil, fmt.Errorf("cannot remove partitions from previous install: %v", err)
	}
	// at this point we removed any existing partition, nuke any
	// of the existing sealed key files placed outside of the
	// encrypted partitions (LP: #1879338)
	sealedKeyFiles, _ := filepath.Glob(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "*.sealed-key"))
	for _, keyFile := range sealedKeyFiles {
		if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot cleanup obsolete key file: %v", keyFile)
		}
	}

	var created []gadget.OnDiskStructure
	timings.Run(perfTimings, "create-partitions", "Create partitions", func(timings.Measurer) {
		created, err = createMissingPartitions(gadgetRoot, diskLayout, lv)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create the partitions: %v", err)
	}

	makeKeySet := func() (*EncryptionKeySet, error) {
		key, err := secboot.NewEncryptionKey()
		if err != nil {
			return nil, fmt.Errorf("cannot create encryption key: %v", err)
		}

		rkey, err := secboot.NewRecoveryKey()
		if err != nil {
			return nil, fmt.Errorf("cannot create recovery key: %v", err)
		}
		return &EncryptionKeySet{
			Key:         key,
			RecoveryKey: rkey,
		}, nil
	}
	roleNeedsEncryption := func(role string) bool {
		return role == gadget.SystemData || role == gadget.SystemSave
	}
	var keysForRoles map[string]*EncryptionKeySet

	for _, part := range created {
		roleFmt := ""
		if part.Role != "" {
			roleFmt = fmt.Sprintf("role %v", part.Role)
		}
		logger.Noticef("created new partition %v for structure %v (size %v) %s",
			part.Node, part, part.Size.IECString(), roleFmt)
		encrypt := (options.EncryptionType != secboot.EncryptionTypeNone)
		if encrypt && roleNeedsEncryption(part.Role) {
			var keys *EncryptionKeySet
			timings.Run(perfTimings, fmt.Sprintf("make-key-set[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption key set for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				keys, err = makeKeySet()
			})
			if err != nil {
				return nil, err
			}
			logger.Noticef("encrypting partition device %v", part.Node)
			var dataPart encryptedDevice
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption device for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				dataPart, err = newEncryptedDeviceLUKS(&part, keys.Key, part.Label)
			})
			if err != nil {
				return nil, err
			}

			timings.Run(perfTimings, fmt.Sprintf("add-recovery-key[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Adding recovery key for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				err = dataPart.AddRecoveryKey(keys.Key, keys.RecoveryKey)
			})
			if err != nil {
				return nil, err
			}

			// update the encrypted device node
			part.Node = dataPart.Node()
			if keysForRoles == nil {
				keysForRoles = map[string]*EncryptionKeySet{}
			}
			keysForRoles[part.Role] = keys
			logger.Noticef("encrypted device %v", part.Node)
		}

		// use the diskLayout.SectorSize here instead of lv.SectorSize, we check
		// that if there is a sector-size specified in the gadget that it
		// matches what is on the disk, but sometimes there may not be a sector
		// size specified in the gadget.yaml, but we will always have the sector
		// size from the physical disk device
		timings.Run(perfTimings, fmt.Sprintf("make-filesystem[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create filesystem for %s", part.Node), func(timings.Measurer) {
			err = makeFilesystem(&part, diskLayout.SectorSize)
		})
		if err != nil {
			return nil, fmt.Errorf("cannot make filesystem for partition %s: %v", roleOrLabelOrName(part), err)
		}

		timings.Run(perfTimings, fmt.Sprintf("write-content[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Write content for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
			err = writeContent(&part, observer)
		})
		if err != nil {
			return nil, err
		}

		if options.Mount && part.Label != "" && part.HasFilesystem() {
			if err := mountFilesystem(&part, boot.InitramfsRunMntDir); err != nil {
				return nil, err
			}
		}
	}

	return &InstalledSystemSideData{
		KeysForRoles: keysForRoles,
	}, nil
}
