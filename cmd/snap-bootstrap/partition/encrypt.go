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
package partition

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/snapcore/snapd/osutil"
)

var (
	tempFile = ioutil.TempFile
)

// The LUKS master key is 64 bytes long
const (
	masterKeySize = 64
)

// EncryptionKey holds the LUKS master key.
type EncryptionKey [masterKeySize]byte

func NewEncryptionKey() (EncryptionKey, error) {
	var key EncryptionKey
	_, err := rand.Read(key[:])
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Store writes the LUKS master key in the location specified by filename.
func (key EncryptionKey) Store(filename string) error {
	// XXX: must provision the TPM, generate and store the lockout authorization,
	// and seal the key. Currently we're just storing the unprocessed data.
	if err := ioutil.WriteFile(filename, key[:], 0600); err != nil {
		return fmt.Errorf("cannot store key file: %v", err)
	}

	return nil
}

// EncryptedDevice represents a LUKS-backed encrypted block device.
type EncryptedDevice struct {
	// Node is the mapped, unencrypted device node
	Node string
	// encryptedNode is the parent, encrypted device node
	encryptedNode string
	name          string
}

func NewEncryptedDevice(part *DeviceStructure, name string) *EncryptedDevice {
	dev := &EncryptedDevice{
		// A new block device is used to access the encrypted data
		Node:          fmt.Sprintf("/dev/mapper/%s", name),
		encryptedNode: part.Node,
		name:          name,
	}

	return dev
}

// Encrypt creates an encrypted device in the existing partition using the
// specified key.
func (dev *EncryptedDevice) Encrypt(key EncryptionKey) error {
	tempKeyFile, err := tempFile("", "enc")
	if err != nil {
		return err
	}
	defer wipe(tempKeyFile.Name())

	// XXX: Ideally we shouldn't write this key, but cryptsetup
	// only reads the master key from a file.
	if _, err := tempKeyFile.Write(key[:]); err != nil {
		return fmt.Errorf("cannot create key file: %s", err)
	}

	if err := cryptsetupFormat(tempKeyFile.Name(), dev.encryptedNode); err != nil {
		return fmt.Errorf("cannot format encrypted device: %v", err)
	}
	if err := cryptsetupOpen(tempKeyFile.Name(), dev.encryptedNode, dev.name); err != nil {
		return fmt.Errorf("cannot open encrypted device on %s: %s", dev.encryptedNode, err)
	}

	return nil
}

func cryptsetupFormat(keyFile, node string) error {
	cmd := exec.Command("cryptsetup", "-q", "luksFormat", "--type", "luks2", "--pbkdf-memory", "100", "--master-key-file", keyFile, node)
	cmd.Stdin = bytes.NewReader([]byte("\n"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func cryptsetupOpen(keyFile, node, name string) error {
	if output, err := exec.Command("cryptsetup", "open", "--master-key-file", keyFile, node, name).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

// wipe overwrites a file with zeros and removes it. It is intended to be used only
// with small files.
// XXX: Better solution: have a custom cryptsetup util that reads master key from stdin
func wipe(name string) error {
	file, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}

	st, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}

	_, err = file.Write(make([]byte, st.Size()))
	if err != nil {
		file.Close()
		return err
	}
	file.Close()

	return os.Remove(name)
}
