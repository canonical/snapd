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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/cmd/snap-fde-keymgr/fdekeymgr"
)

var osStdin io.Reader = os.Stdin

type commonMultiDeviceMixin struct {
	Devices        []string `long:"devices" description:"encrypted devices (can be more than one)" required:"yes"`
	Authorizations []string `long:"authorizations" description:"authorization sources (one for each device, either 'keyring' or 'file:<key-file>')" required:"yes"`
}

type cmdAddRecoveryKey struct {
	commonMultiDeviceMixin
	KeyFile string `long:"key-file" description:"path for generated recovery key file" required:"yes"`
}

type cmdRemoveRecoveryKey struct {
	commonMultiDeviceMixin
	KeyFiles []string `long:"key-files" description:"path to recovery key files to be removed" required:"yes"`
}

type cmdChangeEncryptionKey struct {
	Device     string `long:"device" description:"encrypted device" required:"yes"`
	Stage      bool   `long:"stage" description:"stage the new key"`
	Transition bool   `long:"transition" description:"replace the old key, unstage the new"`
}

type options struct {
	CmdAddRecoveryKey      cmdAddRecoveryKey      `command:"add-recovery-key"`
	CmdRemoveRecoveryKey   cmdRemoveRecoveryKey   `command:"remove-recovery-key"`
	CmdChangeEncryptionKey cmdChangeEncryptionKey `command:"change-encryption-key"`
}

func (c *cmdAddRecoveryKey) Execute(args []string) error {
	return fdekeymgr.AddRecoveryKey(c.Devices, c.Authorizations, c.KeyFile)
}

func (c *cmdRemoveRecoveryKey) Execute(args []string) error {
	return fdekeymgr.RemoveRecoveryKeys(c.Authorizations, c.Devices, c.KeyFiles)
}

type newKey struct {
	Key []byte `json:"key"`
}

var fdeKeymgrChangeEncryptionKey = fdekeymgr.ChangeEncryptionKey

func (c *cmdChangeEncryptionKey) Execute(args []string) error {
	var newEncryptionKeyData newKey
	dec := json.NewDecoder(osStdin)
	if err := dec.Decode(&newEncryptionKeyData); err != nil {
		return fmt.Errorf("cannot obtain new encryption key: %v", err)
	}
	return fdeKeymgrChangeEncryptionKey(c.Device, c.Stage, c.Transition, newEncryptionKeyData.Key)
}

func run(osArgs1 []string) error {
	var opts options
	p := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	if _, err := p.ParseArgs(osArgs1); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
