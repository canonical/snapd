// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootFormatEncryptedDevice = secboot.FormatEncryptedDevice
	secbootAddRecoveryKey        = secboot.AddRecoveryKey
)

// encryptedDeviceCryptsetup represents a encrypted block device.
type encryptedDevice interface {
	Node() string
	AddRecoveryKey(key secboot.EncryptionKey, rkey secboot.RecoveryKey) error
	Close() error
}

// encryptedDeviceLUKS represents a LUKS-backed encrypted block device.
type encryptedDeviceLUKS struct {
	parent *gadget.OnDiskStructure
	name   string
	node   string
}

// expected interface is implemented
var _ = encryptedDevice(&encryptedDeviceLUKS{})

// newEncryptedDeviceLUKS creates an encrypted device in the existing
// partition using the specified key with the LUKS backend.
func newEncryptedDeviceLUKS(part *gadget.OnDiskStructure, key secboot.EncryptionKey, name string) (encryptedDevice, error) {
	dev := &encryptedDeviceLUKS{
		parent: part,
		name:   name,
		// A new block device is used to access the encrypted data. Note that
		// you can't open an encrypted device under different names and a name
		// can't be used in more than one device at the same time.
		node: fmt.Sprintf("/dev/mapper/%s", name),
	}

	if err := secbootFormatEncryptedDevice(key, name+"-enc", part.Node); err != nil {
		return nil, fmt.Errorf("cannot format encrypted device: %v", err)
	}

	if err := cryptsetupOpen(key, part.Node, name); err != nil {
		return nil, fmt.Errorf("cannot open encrypted device on %s: %s", part.Node, err)
	}

	return dev, nil
}

func (dev *encryptedDeviceLUKS) AddRecoveryKey(key secboot.EncryptionKey, rkey secboot.RecoveryKey) error {
	return secbootAddRecoveryKey(key, rkey, dev.parent.Node)
}

func (dev *encryptedDeviceLUKS) Node() string {
	return dev.node
}

func (dev *encryptedDeviceLUKS) Close() error {
	return cryptsetupClose(dev.name)
}

func cryptsetupOpen(key secboot.EncryptionKey, node, name string) error {
	cmd := exec.Command("cryptsetup", "open", "--key-file", "-", node, name)
	cmd.Stdin = bytes.NewReader(key[:])
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func cryptsetupClose(name string) error {
	if output, err := exec.Command("cryptsetup", "close", name).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

// encryptedDeviceWithSetupHook represents a block device that is setup using
// the "device-setup" hook.
type encryptedDeviceWithSetupHook struct {
	parent *gadget.OnDiskStructure
	name   string
	node   string
}

// sanity
var _ = encryptedDevice(&encryptedDeviceWithSetupHook{})

const headerMagic = "snapd_ice"

// newEncryptedDeviceWithSetupHook creates an encrypted device in the
// existing partition using the specified key using the fde-setup hook
func newEncryptedDeviceWithSetupHook(part *gadget.OnDiskStructure, key secboot.EncryptionKey, name string) (encryptedDevice, error) {
	// for roles requiring encryption, the filesystem label is always set to
	// either the implicit value or a value that has been validated
	if part.Name != name || part.Label != name {
		return nil, fmt.Errorf("cannot use partition name %q for an encrypted structure with %v role and filesystem with label %q",
			name, part.Role, part.Label)
	}

	// 1. create linear mapper device with 1Mb of reserved space
	uuid := ""
	offset := fde.DeviceSetupHookPartitionOffset
	sizeMinusOffset := uint64(part.Size) - offset
	mapperDevice, err := disks.CreateLinearMapperDevice(part.Node, name, uuid, offset, sizeMinusOffset)
	if err != nil {
		return nil, err
	}

	// 1.5 - write out header to the 1Mb reserved space
	// TODO: come up with a better format than just JSON for the primary data


	// TODO: reading/writing this header should be abstracted out to somewhere
	// like kernel/fde package

	// the primary data to be encoded
	headerData := map[string]interface{}{
		// TODO: what else do we need to store here?
		"version": 1,
	}

	buf := &bytes.Buffer{}

	// write the header
	buf.WriteString(headerMagic)

	// encode the primary header data
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(headerData); err != nil {
		return nil, fmt.Errorf("cannot serialize header data: %v", err)
	}

	// ensure it fits into the reserved space
	headerPlusPrimarySize := uint64(buf.Len())
	if headerPlusPrimarySize > fde.DeviceSetupHookPartitionOffset {
		return nil, fmt.Errorf("header serialization (size: %d) does not fit into reserved space of %d bytes", headerPlusPrimarySize, fde.DeviceSetupHookPartitionOffset)
	}

	// pad the rest with all 0's
	zerosToPad := fde.DeviceSetupHookPartitionOffset - headerPlusPrimarySize
	// golang will always allocate the zero value - so this is guaranteed to be
	// an array of zeros
	padding := make([]byte, zerosToPad)
	buf.Write(padding)

	// double confirm that it is the right size
	if uint64(buf.Len()) != fde.DeviceSetupHookPartitionOffset {
		return nil, fmt.Errorf("internal error: header should have been %d bytes, but instead ended up being %d bytes after padding", fde.DeviceSetupHookPartitionOffset, buf.Len())
	}

	// Write it to the partition node - note that O_SYNC is important here to
	// make sure we get our data written out to the physical media before we go
	// to turn on encryption below. Without this option, essentially the data
	// would be cached and not written to the physical media until after
	// encryption has been enabled, thus making our header encrypted and
	// unintelligible from the initramfs before we turn on encryption. This
	// property of being able to read the header unencrypted before encryption
	// is turned on is important for being able to eventually transition to
	// using LUKS for ICE encryption.
	f, err := os.OpenFile(part.Node, os.O_WRONLY|os.O_SYNC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open partition %s for writing header: %v", part.Node, err)
	}

	closeDoer := &sync.Once{}
	close := func() { f.Close() }
	defer func() { closeDoer.Do(close) }()

	// don't use buf var after this line
	data := buf.Bytes()

	bytesWritten, err := f.Write(data)
	if err != nil {
		return nil, fmt.Errorf("cannot write header to partition %s: %v", part.Node, err)
	}

	if uint64(bytesWritten) != fde.DeviceSetupHookPartitionOffset {
		return nil, fmt.Errorf("cannot write header to partition %s: partial write of %d bytes, expected to write %d bytes", part.Node, bytesWritten, fde.DeviceSetupHookPartitionOffset)
	}

	// manually close early
	closeDoer.Do(close)

	logger.Debugf("wrote header file to %s with first 20 bytes of content as %s", part.Node, data[:20])

	// TODO: we probably don't need this anymore

	// now for extra paranoia, re-read the bytes we just wrote to the partition
	// to make sure no funny business is going on with the data we are writing
	// to the partition

	partFAgain, err := os.Open(part.Node)
	if err != nil {
		logger.Noticef("cannot open node %s: %v", part.Node, err)
		return nil, fmt.Errorf("error debugging inline cryptography encrypted partition: %v", err)
	}
	closeAgainDoer := &sync.Once{}
	closeAgain := func() { partFAgain.Close() }
	defer func() { closeAgainDoer.Do(closeAgain) }()

	bufAgain := make([]byte, fde.DeviceSetupHookPartitionOffset)
	n, err := partFAgain.Read(bufAgain)
	if err != nil {
		logger.Noticef("cannot read 1M bytes from node %s: %v", part.Node, err)
		return nil, fmt.Errorf("error debugging inline cryptography encrypted partition: %v", err)
	}
	if n != int(fde.DeviceSetupHookPartitionOffset) {
		logger.Noticef("incomplete read of %d bytes from node %s (expected %d bytes)", n, part.Node, fde.DeviceSetupHookPartitionOffset)
		return nil, fmt.Errorf("incomplete read of %d bytes from node %s (expected %d bytes)", n, part.Node, fde.DeviceSetupHookPartitionOffset)
	}

	closeAgainDoer.Do(closeAgain)

	if !bytes.Equal(data, bufAgain) {
		return nil, fmt.Errorf("did not read back same data from partition node that we wrote to it, orig[:20]: %s, again[:20]: %s", string(data[:20]), string(bufAgain[:20]))
	}

	// finally put the data we wrote to the header some place in /run so that
	// when we go to gather disk traits after all partitions are setup and we
	// can no longer see the first 1 Mb of the physical disk, we can check in
	// /run for that data instead the same as what we do during run mode (since
	// the initramfs will put that data into /run for userspace before setting
	// up the encryption)

	// TODO: get the disk major/minor directly from the gadget on disk structure
	// rather than what we do here to re-find the disk using the partition
	// device node here - we can trust that the major/minor won't change between
	// the initramfs for a structure that was successfully decrypted and
	// userspace when we go to check for a gadget asset update
	disk, err := disks.DiskFromPartitionDeviceNode(part.Node)
	if err != nil {
		return nil, fmt.Errorf("cannot get disk from partition device node")
	}

	// TODO: this should live in dirs instead, maybe even be a method on the
	// disks.Partition object instead
	headerCopyInRun := filepath.Join(dirs.GlobalRootDir, "/run/snapd/disks", disk.Dev(), strconv.Itoa(part.DiskIndex))

	// TODO: make this dir private or not?
	if err := os.MkdirAll(filepath.Dir(headerCopyInRun), 0600); err != nil {
		return nil, fmt.Errorf("cannot create /run dir for disks header backup: %v", err)
	}
	if err := ioutil.WriteFile(headerCopyInRun, bufAgain, 0600); err != nil {
		return nil, fmt.Errorf("cannot write header copy to /run: %v", err)
	}

	logger.Debugf("wrote /run copy of header to %s", headerCopyInRun)

	// 2. run fde-setup "device-setup" on it
	// TODO: We may need a different way to run the fde-setup hook
	//       here. The hook right now runs with a locked state. But
	//       when this runs the state will be unlocked but our hook
	//       mechanism needs a locked state. This means we either need
	//       something like "boot.RunFDE*Device*SetupHook" or we run
	//       the entire install with the state locked (which may not
	//       be as terrible as it sounds as this is a rare situation).
	runHook := boot.RunFDESetupHook
	params := &fde.DeviceSetupParams{
		Key:           key,
		Device:        mapperDevice,
		PartitionName: name,
	}
	if err := fde.DeviceSetup(runHook, params); err != nil {
		return nil, err
	}

	return &encryptedDeviceWithSetupHook{
		parent: part,
		name:   name,
		node:   mapperDevice,
	}, nil
}

func (dev *encryptedDeviceWithSetupHook) Close() error {
	if output, err := exec.Command("dmsetup", "remove", dev.name).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func (dev *encryptedDeviceWithSetupHook) Node() string {
	return dev.node
}

func (dev *encryptedDeviceWithSetupHook) AddRecoveryKey(key secboot.EncryptionKey, rkey secboot.RecoveryKey) error {
	return fmt.Errorf("recovery keys are not supported on devices that use the device-setup hook")
}
