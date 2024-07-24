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

package secboot

import (
	"fmt"

	"github.com/snapcore/snapd/osutil"
)

type DiskUnlockKey = []byte

type KeyDataWriter interface {
	// TODO: this will typically have a function that takes a key
	// data as input
}

// BootstrappedContainer is an abstraction for an encrypted container
// along with a key that is able to enroll other keys.  This key is
// meant to be an initial key that is removed after all required keys
// are enrolled, by calling RemoveBootstrapKey.
type BootstrappedContainer interface {
	//LegacyKeptKey is only temporary until we have moved to using multiple keys
	LegacyKeptKey() DiskUnlockKey
	//AddKey adds a key "newKey" to "slotName"
	//If "token", the a KeyDataWriter is returned to write key data to the token of the new key slot
	AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error)
	//RemoveBootstrapKey removes the bootstrap key
	RemoveBootstrapKey() error
}

type legacyBootstrappedContainer struct {
	key      DiskUnlockKey
	finished bool
}

func (l *legacyBootstrappedContainer) LegacyKeptKey() DiskUnlockKey {
	if l.finished {
		panic("internal error: trying to access installation key after being removed")
	}
	return l.key
}

func (l *legacyBootstrappedContainer) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	return nil, fmt.Errorf("not implemented")
}

func (l *legacyBootstrappedContainer) RemoveBootstrapKey() error {
	l.finished = true
	return nil
}

// CreateBootstrappedContainer creates a new BootstrappedContainer for a given device
// path and bootstrap unlock key. The unlock key must be valid to
// unlock the device.
// TODO: devicePath might use an abstraction for key slot container,
// instead of a path
// TODO: key should probably be optional and generated instead
func CreateBootstrappedContainer(key DiskUnlockKey, devicePath string) BootstrappedContainer {
	return &legacyBootstrappedContainer{
		key:      key,
		finished: false,
	}
}

type mockBootstrappedContainer struct {
	finished bool
}

func (l *mockBootstrappedContainer) LegacyKeptKey() DiskUnlockKey {
	panic("not implemented")
}

func (l *mockBootstrappedContainer) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	return nil, fmt.Errorf("not implemented")
}

func (l *mockBootstrappedContainer) RemoveBootstrapKey() error {
	l.finished = true
	return nil
}

func CreateMockBootstrappedContainer() BootstrappedContainer {
	osutil.MustBeTestBinary("CreateMockBootstrappedContainer can be only called from tests")
	return &mockBootstrappedContainer{
		finished: false,
	}
}
