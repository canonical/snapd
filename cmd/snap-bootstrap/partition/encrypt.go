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
package partition

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os/exec"

	"github.com/snapcore/snapd/osutil"
)

var (
	tempFile = ioutil.TempFile
)

// Our key is 32 bytes long
const (
	keySize = 32
)

type EncryptionKey [keySize]byte

func NewEncryptionKey() (EncryptionKey, error) {
	var key EncryptionKey
	// rand.Read() is protected against short reads
	_, err := rand.Read(key[:])
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Store writes the LUKS key in the location specified by filename.
func (key EncryptionKey) Store(filename string) error {
	// TODO:UC20: provision the TPM, generate and store the lockout authorization,
	//            and seal the key. Currently we're just storing the unprocessed data.
	if err := ioutil.WriteFile(filename, key[:], 0600); err != nil {
		return fmt.Errorf("cannot store key file: %v", err)
	}

	return nil
}

// EncryptedDevice represents a LUKS-backed encrypted block device.
type EncryptedDevice struct {
	parent *DeviceStructure
	name   string
	Node   string
}

// NewEncryptedDevice creates an encrypted device in the existing partition using the
// specified key.
func NewEncryptedDevice(part *DeviceStructure, key EncryptionKey, name string) (*EncryptedDevice, error) {
	dev := &EncryptedDevice{
		parent: part,
		name:   name,
		// A new block device is used to access the encrypted data. Note that
		// you can't open an encrypted device under different names and a name
		// can't be used in more than one device at the same time.
		Node: fmt.Sprintf("/dev/mapper/%s", name),
	}

	if err := cryptsetupFormat(key, part.Node); err != nil {
		return nil, fmt.Errorf("cannot format encrypted device: %v", err)
	}

	if err := cryptsetupOpen(key, part.Node, name); err != nil {
		return nil, fmt.Errorf("cannot open encrypted device on %s: %s", part.Node, err)
	}

	return dev, nil
}

func (dev *EncryptedDevice) Close() error {
	return cryptsetupClose(dev.name)
}

func cryptsetupFormat(key EncryptionKey, node string) error {
	// We use a keyfile with the same entropy as the derived key so we can
	// keep the KDF iteration count to a minimum. Longer processing will not
	// increase security in this case.
	args := []string{
		// batch processing, no password verification
		"-q",
		// formatting a new device
		"luksFormat",
		// use LUKS2
		"--type", "luks2",
		// key file read from stdin
		"--key-file", "-",
		// user Argon2 for PBKDF
		"--pbkdf", "argon2i", "--iter-time", "1",
		// device to format
		node,
	}
	cmd := exec.Command("cryptsetup", args...)
	cmd.Stdin = bytes.NewReader(key[:])
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func cryptsetupOpen(key EncryptionKey, node, name string) error {
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
