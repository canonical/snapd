// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package devicestate

import (
	"fmt"
	"gopkg.in/tomb.v2"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
)

var (
	bootForceReseal = boot.ForceReseal
	restartRequest  = restart.Request
)

func reencryptPartitions(model *asserts.Model, gadgetDir string, recoveryKey sb.RecoveryKey) (map[string]keys.EncryptionKey, error) {
	info, err := gadget.ReadInfoAndValidate(gadgetDir, model, nil)
	if err != nil {
		return nil, err
	}

	bootVol, err := gadget.FindBootVolume(info.Volumes)
	if err != nil {
		return nil, err
	}

	diskFromBootPartition, err := disks.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil)
	if err != nil {
		return nil, fmt.Errorf("cannot find disk from boot partition: %w", err)
	}

	bootDevice := diskFromBootPartition.KernelDeviceNode()

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %w", bootDevice, err)
	}

	volCompatOps := &gadget.VolumeCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption:      map[string]gadget.StructureEncryptionParameters{},
	}

	encryptionParam := gadget.StructureEncryptionParameters{Method: gadget.EncryptionLUKS}
	for _, volStruct := range bootVol.Structure {
		switch volStruct.Role {
		case gadget.SystemData, gadget.SystemSave:
			volCompatOps.ExpectedStructureEncryption[volStruct.Name] = encryptionParam
		}
	}

	yamlIdxToOnDiskStruct, err := gadget.EnsureVolumeCompatibility(bootVol, diskLayout, volCompatOps)
	if err != nil {
		return nil, fmt.Errorf("gadget and system-boot device %v partition table not compatible: %w", bootDevice, err)
	}

	keyForRole := make(map[string]keys.EncryptionKey)

	for yamlIdx, onDiskStruct := range yamlIdxToOnDiskStruct {
		vs := bootVol.StructFromYamlIndex(yamlIdx)
		if vs == nil {
			continue
		}
		switch vs.Role {
		case gadget.SystemSave, gadget.SystemData:
			encryptionKey, err := keys.NewEncryptionKey()
			if err != nil {
				return nil, fmt.Errorf("cannot create encryption key: %w", err)
			}
			sb.ChangeLUKS2KeyUsingRecoveryKey(onDiskStruct.Node, recoveryKey, encryptionKey)
			keyForRole[vs.Role] = encryptionKey
		default:
		}
	}

	return keyForRole, nil
}

func (m *DeviceManager) doReseal(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	mgr := deviceMgr(st)
	sysKeys, err := mgr.EnsureRecoveryKeys()
	if err != nil {
		return err
	}

	recoveryKey, err := sb.ParseRecoveryKey(sysKeys.RecoveryKey)
	if err != nil {
		return err
	}

	var reboot bool

	t.Get("reboot-after", &reboot)

	model, err := m.Model()
	if err != nil {
		return err
	}
	deviceCtx, err := DeviceCtx(m.state, nil, nil)
	if err != nil {
		return err
	}

	gadgetSnapInfo, err := snapstate.GadgetInfo(st, deviceCtx)
	if err != nil {
		return err
	}

	keyForRole, err := reencryptPartitions(model, gadgetSnapInfo.MountDir(), recoveryKey)
	if err != nil {
		return err
	}

	if err := bootForceReseal(keyForRole, st.Unlocker()); err != nil {
		return err
	}

	if reboot {
		restartRequest(m.state, restart.RestartSystemNow, nil)
	}
	return nil
}
