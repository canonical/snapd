// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"
	"io/ioutil"
	"os/exec"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
)

var (
	tempFile = ioutil.TempFile
)

var (
	secbootFormatEncryptedDevice = secboot.FormatEncryptedDevice
	secbootAddRecoveryKey        = secboot.AddRecoveryKey
)

// encryptedDevice represents a LUKS-backed encrypted block device.
type encryptedDevice struct {
	parent *gadget.OnDiskStructure
	name   string
	Node   string
}

// newEncryptedDevice creates an encrypted device in the existing partition using the
// specified key.
func newEncryptedDevice(part *gadget.OnDiskStructure, key secboot.EncryptionKey, name string) (*encryptedDevice, error) {
	dev := &encryptedDevice{
		parent: part,
		name:   name,
		// A new block device is used to access the encrypted data. Note that
		// you can't open an encrypted device under different names and a name
		// can't be used in more than one device at the same time.
		Node: fmt.Sprintf("/dev/mapper/%s", name),
	}

	if err := secbootFormatEncryptedDevice(key, name+"-enc", part.Node); err != nil {
		return nil, fmt.Errorf("cannot format encrypted device: %v", err)
	}

	if err := cryptsetupOpen(key, part.Node, name); err != nil {
		return nil, fmt.Errorf("cannot open encrypted device on %s: %s", part.Node, err)
	}

	return dev, nil
}

func (dev *encryptedDevice) AddRecoveryKey(key secboot.EncryptionKey, rkey secboot.RecoveryKey) error {
	return secbootAddRecoveryKey(key, rkey, dev.parent.Node)
}

func (dev *encryptedDevice) Close() error {
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
