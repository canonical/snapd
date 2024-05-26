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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot/keymgr"
	"github.com/snapcore/snapd/secboot/keys"
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

var (
	keymgrAddRecoveryKeyToLUKSDevice              = keymgr.AddRecoveryKeyToLUKSDevice
	keymgrAddRecoveryKeyToLUKSDeviceUsingKey      = keymgr.AddRecoveryKeyToLUKSDeviceUsingKey
	keymgrRemoveRecoveryKeyFromLUKSDevice         = keymgr.RemoveRecoveryKeyFromLUKSDevice
	keymgrRemoveRecoveryKeyFromLUKSDeviceUsingKey = keymgr.RemoveRecoveryKeyFromLUKSDeviceUsingKey
	keymgrStageLUKSDeviceEncryptionKeyChange      = keymgr.StageLUKSDeviceEncryptionKeyChange
	keymgrTransitionLUKSDeviceEncryptionKeyChange = keymgr.TransitionLUKSDeviceEncryptionKeyChange
)

func validateAuthorizations(authorizations []string) error {
	for _, authz := range authorizations {
		switch {
		case authz == "keyring":
			// happy
		case strings.HasPrefix(authz, "file:"):
			// file must exist
			kf := authz[len("file:"):]
			if !osutil.FileExists(kf) {
				return fmt.Errorf("authorization file %v does not exist", kf)
			}
		default:
			return fmt.Errorf("unknown authorization method %q", authz)
		}
	}
	return nil
}

func writeIfNotExists(p string, data []byte) (alreadyExists bool, err error) {
	f := mylog.Check2(os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600))
	mylog.Check2(f.Write(data))

	return false, f.Close()
}

func (c *cmdAddRecoveryKey) Execute(args []string) error {
	recoveryKey := mylog.Check2(keys.NewRecoveryKey())

	if len(c.Authorizations) != len(c.Devices) {
		return fmt.Errorf("cannot add recovery keys: mismatch in the number of devices and authorizations")
	}
	mylog.Check(validateAuthorizations(c.Authorizations))

	// write the key to the file, if the file already exists it is possible
	// that we are being called again after an unexpected reboot or a
	// similar event
	alreadyExists := mylog.Check2(writeIfNotExists(c.KeyFile, recoveryKey[:]))

	if alreadyExists {
		// we already have the recovery key, read it back
		maybeKey := mylog.Check2(os.ReadFile(c.KeyFile))

		// TODO: verify that the size if non 0 and try again otherwise?
		if len(maybeKey) != len(recoveryKey) {
			return fmt.Errorf("cannot use existing recovery key of size %v", len(maybeKey))
		}
		copy(recoveryKey[:], maybeKey[:])
	}
	// add the recovery key to each device; keys are always added to the
	// same keyslot, so when the key existed on disk, assume that the key
	// was already added to the device in case we hit an error with keyslot
	// being already used
	for i, dev := range c.Devices {
		authz := c.Authorizations[i]
		switch {
		case authz == "keyring":
			mylog.Check(keymgrAddRecoveryKeyToLUKSDevice(recoveryKey, dev))

		case strings.HasPrefix(authz, "file:"):
			authzKey := mylog.Check2(os.ReadFile(authz[len("file:"):]))
			mylog.Check(keymgrAddRecoveryKeyToLUKSDeviceUsingKey(recoveryKey, authzKey, dev))

		}
	}
	return nil
}

func (c *cmdRemoveRecoveryKey) Execute(args []string) error {
	if len(c.Authorizations) != len(c.Devices) {
		return fmt.Errorf("cannot remove recovery keys: mismatch in the number of devices and authorizations")
	}
	mylog.Check(validateAuthorizations(c.Authorizations))

	for i, dev := range c.Devices {
		authz := c.Authorizations[i]
		switch {
		case authz == "keyring":
			mylog.Check(keymgrRemoveRecoveryKeyFromLUKSDevice(dev))

		case strings.HasPrefix(authz, "file:"):
			authzKey := mylog.Check2(os.ReadFile(authz[len("file:"):]))
			mylog.Check(keymgrRemoveRecoveryKeyFromLUKSDeviceUsingKey(authzKey, dev))

		}
	}
	var rmErrors []string
	for _, kf := range c.KeyFiles {
		if mylog.Check(os.Remove(kf)); err != nil && !os.IsNotExist(err) {
			rmErrors = append(rmErrors, err.Error())
		}
	}
	if len(rmErrors) != 0 {
		return fmt.Errorf("cannot remove key files:\n%s", strings.Join(rmErrors, "\n"))
	}
	return nil
}

type newKey struct {
	Key []byte `json:"key"`
}

func (c *cmdChangeEncryptionKey) Execute(args []string) error {
	if c.Stage && c.Transition {
		return fmt.Errorf("cannot both stage and transition the encryption key change")
	}
	if !c.Stage && !c.Transition {
		return fmt.Errorf("cannot change encryption key without stage or transition request")
	}

	var newEncryptionKeyData newKey
	dec := json.NewDecoder(osStdin)
	mylog.Check(dec.Decode(&newEncryptionKeyData))

	switch {
	case c.Stage:
		mylog.Check(
			// staging the key change authorizes the operation using a key
			// from the keyring
			keymgrStageLUKSDeviceEncryptionKeyChange(newEncryptionKeyData.Key, c.Device))

	case c.Transition:
		mylog.Check(
			// transitioning the key change authorizes the operation using
			// the currently provided key (which must have been staged
			// before hence the op will be authorized successfully)
			keymgrTransitionLUKSDeviceEncryptionKeyChange(newEncryptionKeyData.Key, c.Device))

	}
	return nil
}

func run(osArgs1 []string) error {
	var opts options
	p := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	mylog.Check2(p.ParseArgs(osArgs1))

	return nil
}

func main() {
	mylog.Check(run(os.Args[1:]))
}
