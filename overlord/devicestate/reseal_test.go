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

package devicestate_test

import (
	"encoding/hex"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type deviceMgrResealSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrResealSuite{})

var pcSnapYaml = `
name: pc
type: gadget
`

var pcGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed
        size: 100M
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
      - name: ubuntu-boot
        role: system-boot
        size: 100M
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
      - name: ubuntu-data
        role: system-data
        size: 100M
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
      - name: ubuntu-save
        role: system-save
        size: 100M
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
`

func (s *deviceMgrResealSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.setupBaseTest(c, false)
	s.setUC20PCModelInState(c)
	devicestate.SetSystemMode(s.mgr, "run")
	gadgetSideInfo := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   "pc-id",
	}
	snaptest.MockSnapWithFiles(c, pcSnapYaml, &snap.SideInfo{Revision: snap.R(1)}, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
	})

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{gadgetSideInfo}),
		Current:  snap.R(1),
		SnapType: "gadget",
	})

	seedPart := disks.Partition{
		FilesystemLabel:  "ubuntu-seed",
		PartitionLabel:   "ubuntu-seed",
		PartitionUUID:    "ubuntu-seed-partuuid",
		KernelDeviceNode: "/dev/fakedevice0p1",
		PartitionType:    "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
		DiskIndex:        1,
		StartInBytes:     1024 * 1024,
		SizeInBytes:      100 * 1024 * 1024,
	}

	bootPart := disks.Partition{
		FilesystemLabel:  "ubuntu-boot",
		PartitionLabel:   "ubuntu-boot",
		PartitionUUID:    "ubuntu-boot-partuuid",
		KernelDeviceNode: "/dev/fakedevice0p2",
		DiskIndex:        2,
		StartInBytes:     101 * 1024 * 1024,
		SizeInBytes:      100 * 1024 * 1024,
	}

	dataPart := disks.Partition{
		FilesystemLabel:  "ubuntu-data-enc",
		FilesystemType:   "crypto_LUKS",
		PartitionLabel:   "ubuntu-data",
		PartitionUUID:    "ubuntu-data-partuuid",
		KernelDeviceNode: "/dev/fakedevice0p4",
		DiskIndex:        3,
		StartInBytes:     201 * 1024 * 1024,
		SizeInBytes:      100 * 1024 * 1024,
	}

	savePart := disks.Partition{
		FilesystemLabel:  "ubuntu-save-enc",
		FilesystemType:   "crypto_LUKS",
		PartitionLabel:   "ubuntu-save",
		PartitionUUID:    "ubuntu-save-partuuid",
		KernelDeviceNode: "/dev/fakedevice0p3",
		DiskIndex:        4,
		StartInBytes:     301 * 1024 * 1024,
		SizeInBytes:      100 * 1024 * 1024,
	}

	fakeDisk := &disks.MockDiskMapping{
		DevNum:  "42:0",
		DevNode: "/dev/fakedevice0",
		DevPath: "/sys/block/fakedevice0",
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			dataPart,
			savePart,
		},
		DiskSchema:          "gpt",
		SectorSizeBytes:     512,
		DiskUsableSectorEnd: 2*1024*1024 - 1,
		DiskSizeInBytes:     1024 * 1024 * 1024,
	}

	s.AddCleanup(disks.MockDeviceNameToDiskMapping(
		map[string]*disks.MockDiskMapping{
			"/dev/fakedevice0": fakeDisk,
		},
	))

	s.AddCleanup(disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: "/run/mnt/ubuntu-boot", IsDecryptedDevice: false}: fakeDisk,
		},
	))
}

func (s *deviceMgrResealSuite) testResealHappy(c *C, reboot bool) {
	mockSystemRecoveryKeys(c, false)
	mockSnapFDEFile(c, "marker", nil)

	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	defer devicestate.MockSecbootEnsureRecoveryKey(func(keyFile string, rkeyDevs []secboot.RecoveryKeyDevice) (keys.RecoveryKey, error) {
		var rkey keys.RecoveryKey
		copy(rkey[:], []byte(rkeystr))
		return rkey, nil
	})()

	finishReseal := make(chan struct{})
	startedReseal := make(chan struct{})

	forceResealCalls := 0
	defer devicestate.MockBootForceReseal(func(keyForRole map[string]keys.EncryptionKey, unlocker boot.Unlocker) error {
		forceResealCalls++
		defer unlocker()()
		startedReseal <- struct{}{}
		<-finishReseal
		_, hasDataKey := keyForRole[gadget.SystemData]
		_, hasSaveKey := keyForRole[gadget.SystemSave]
		c.Assert(hasDataKey, Equals, true)
		c.Assert(hasSaveKey, Equals, true)
		return nil
	})()

	restartRequestCalls := 0
	defer devicestate.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Check(t, Equals, restart.RestartSystemNow)
		c.Check(rebootInfo, IsNil)
		restartRequestCalls++
	})()

	s.state.Lock()
	defer s.state.Unlock()
	chg := devicestate.Reseal(s.state, reboot)

	s.state.Unlock()
	s.se.Ensure()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	c.Check(chg.Err(), IsNil)

	s.state.Unlock()
	select {
	case <-startedReseal:
	case <-chg.Ready():
	}
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	c.Check(chg.Err(), IsNil)

	c.Check(forceResealCalls, Equals, 1)
	c.Check(restartRequestCalls, Equals, 0)

	finishReseal <- struct{}{}

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	c.Check(forceResealCalls, Equals, 1)
	if reboot {
		c.Check(restartRequestCalls, Equals, 1)
	} else {
		c.Check(restartRequestCalls, Equals, 0)
	}
}

func (s *deviceMgrResealSuite) TestResealRebootHappy(c *C) {
	s.testResealHappy(c, true)
}

func (s *deviceMgrResealSuite) TestResealNoRebootHappy(c *C) {
	s.testResealHappy(c, false)
}

func (s *deviceMgrResealSuite) TestResealError(c *C) {
	mockSystemRecoveryKeys(c, false)
	mockSnapFDEFile(c, "marker", nil)

	rkeystr, err := hex.DecodeString("e1f01302c5d43726a9b85b4a8d9c7f6e")
	c.Assert(err, IsNil)
	defer devicestate.MockSecbootEnsureRecoveryKey(func(keyFile string, rkeyDevs []secboot.RecoveryKeyDevice) (keys.RecoveryKey, error) {
		var rkey keys.RecoveryKey
		copy(rkey[:], []byte(rkeystr))
		return rkey, nil
	})()

	finishReseal := make(chan struct{})
	startedReseal := make(chan struct{})

	forceResealCalls := 0
	defer devicestate.MockBootForceReseal(func(keyForRole map[string]keys.EncryptionKey, unlocker boot.Unlocker) error {
		forceResealCalls++
		defer unlocker()()
		startedReseal <- struct{}{}
		<-finishReseal
		return fmt.Errorf("some error")
	})()

	restartRequestCalls := 0
	defer devicestate.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Check(t, Equals, restart.RestartSystemNow)
		c.Check(rebootInfo, IsNil)
		restartRequestCalls++
	})()

	s.state.Lock()
	defer s.state.Unlock()

	const reboot = true
	chg := devicestate.Reseal(s.state, reboot)

	s.state.Unlock()
	s.se.Ensure()
	select {
	case <-startedReseal:
	case <-chg.Ready():
	}
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	c.Assert(chg.Err(), IsNil)

	c.Check(forceResealCalls, Equals, 1)
	c.Check(restartRequestCalls, Equals, 0)

	finishReseal <- struct{}{}

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks.*Reseal device against boot parameters \(some error\)`)

	c.Check(forceResealCalls, Equals, 1)
	c.Check(restartRequestCalls, Equals, 0)
}
