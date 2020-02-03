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
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var (
	tempFile = ioutil.TempFile
)

const (
	encryptionKeySize = 32
	recoveryKeySize   = 16
)

type EncryptionKey [encryptionKeySize]byte

func NewEncryptionKey() (EncryptionKey, error) {
	var key EncryptionKey
	// rand.Read() is protected against short reads
	_, err := rand.Read(key[:])
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Store writes the LUKS key in the location specified by filename.
func (key EncryptionKey) Store(filename string) error {
	if err := ioutil.WriteFile(filename, key[:], 0600); err != nil {
		return err
	}

	return nil
}

type RecoveryKey [recoveryKeySize]byte

func NewRecoveryKey() (RecoveryKey, error) {
	var key RecoveryKey
	// rand.Read() is protected against short reads
	_, err := rand.Read(key[:])
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Store writes the recovery key in the location specified by filename.
func (key RecoveryKey) Store(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}

	groups := make([]string, 8)

	// The recovery key is used as 8 groups of 5 base-10 digits, with each
	// 5 digits being converted to a 2-byte number to make a 16-byte key.
	r := bytes.NewReader(key[:])
	for i := 0; i < 8; i++ {
		var val uint16
		if err := binary.Read(r, binary.LittleEndian, &val); err != nil {
			return err
		}
		groups[i] = fmt.Sprintf("%05d", val)
	}

	if err := ioutil.WriteFile(filename, []byte(strings.Join(groups, " ")), 0600); err != nil {
		return err
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

	if err := cryptsetupFormat(key, name+"-enc", part.Node); err != nil {
		return nil, fmt.Errorf("cannot format encrypted device: %v", err)
	}

	if err := cryptsetupOpen(key, part.Node, name); err != nil {
		return nil, fmt.Errorf("cannot open encrypted device on %s: %s", part.Node, err)
	}

	return dev, nil
}

func (dev *EncryptedDevice) AddRecoveryKey(key EncryptionKey, rkey RecoveryKey) error {
	return cryptsetupAddKey(key, rkey, dev.parent.Node)
}

func (dev *EncryptedDevice) Close() error {
	return cryptsetupClose(dev.name)
}

func cryptsetupFormat(key EncryptionKey, label, node string) error {
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
		// read key from stdin
		"--key-file", "-",
		// use Argon2i for PBKDF
		"--pbkdf", "argon2i", "--iter-time", "1",
		// set LUKS2 label
		"--label", label,
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

func cryptsetupAddKey(key EncryptionKey, rkey RecoveryKey, node string) error {
	// create a named pipe to pass the recovery key
	fpath := filepath.Join(dirs.SnapRunDir, "tmp-rkey")
	if err := syscall.Mkfifo(fpath, 0600); err != nil {
		return fmt.Errorf("cannot create named pipe: %v", err)
	}
	defer os.RemoveAll(fpath)

	// add a new key to slot 1 reading the passphrase from the named pipe
	args := []string{
		// add a new key
		"luksAddKey",
		// the encrypted device
		node,
		// batch processing, no password verification
		"-q",
		// read existing key from stdin
		"--key-file", "-",
		// store it in keyslot 1
		"--key-slot", "1",
		// the named pipe
		fpath,
	}

	cmd := exec.Command("cryptsetup", args...)
	cmd.Stdin = bytes.NewReader(key[:])
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	// open the named pipe and write the recovery key
	file, err := os.OpenFile(fpath, os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot open recovery key pipe: %v", err)
	}
	n, err := file.Write(rkey[:])
	if n != recoveryKeySize {
		file.Close()
		return fmt.Errorf("cannot write recovery key: short write (%d bytes written)", n)
	}
	if err != nil {
		cmd.Process.Kill()
		file.Close()
		return fmt.Errorf("cannot write recovery key: %v", err)
	}
	if err := file.Close(); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("cannot close recovery key pipe: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("cannot add recovery key: %v", err)
	}

	return nil
}
