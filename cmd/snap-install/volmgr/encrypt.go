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
package volmgr

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var (
	tempKeyFile = "/run/unlock.tmp"
)

type EncryptedPartition struct {
	device      string
	cryptDevice string
	key         []byte
}

func NewEncryptedPartition(device, cryptDevice string, key []byte) *EncryptedPartition {
	return &EncryptedPartition{
		device:      device,
		cryptDevice: cryptDevice,
		key:         key,
	}
}

// Create formats an encrypted partition on the target device.
func (e *EncryptedPartition) Create() error {
	// Ideally we shouldn't write this key, but cryptsetup only reads the
	// master key from a file.
	if err := ioutil.WriteFile(tempKeyFile, e.key, 0600); err != nil {
		return fmt.Errorf("can't create key file: %s", err)
	}
	defer wipe(tempKeyFile)

	logger.Noticef("Create encryted device on %s", e.device)
	cmd := exec.Command("cryptsetup", "-q", "luksFormat", "--type", "luks2", "--pbkdf-memory", "1000",
		"--master-key-file", tempKeyFile, e.device)
	cmd.Stdin = bytes.NewReader([]byte("\n"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot format encrypted device: %s", err))
	}

	if output, err := exec.Command("cryptsetup", "open", "--master-key-file", tempKeyFile, e.device,
		e.cryptDevice).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, fmt.Errorf("cannot open encrypted device on %s: %s", e.device, err))
	}

	return nil
}
