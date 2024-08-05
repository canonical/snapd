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

type KeyDataWriter interface {
	// TODO: this will typically have a function that takes a key
	// data as input
}

// BootstrappedContainer is an abstraction for an encrypted container
// along with a key that is able to enroll other keys.  This key is
// meant to be an initial key that is removed after all required keys
// are enrolled, by calling RemoveBootstrapKey.
type BootstrappedContainer interface {
	//AddKey adds a key "newKey" to "slotName"
	//If "token", the a KeyDataWriter is returned to write key data to the token of the new key slot
	AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error)
	//RemoveBootstrapKey removes the bootstrap key
	RemoveBootstrapKey() error
}

func createBootstrappedContainerMockImpl(key DiskUnlockKey, devicePath string) BootstrappedContainer {
	panic("trying to create a bootstrapped container in a non-secboot build")
}

var CreateBootstrappedContainer = createBootstrappedContainerMockImpl

type MockBootstrappedContainer struct {
	BootstrapKeyRemoved bool
	Slots               map[string][]byte
}

func CreateMockBootstrappedContainer() *MockBootstrappedContainer {
	osutil.MustBeTestBinary("CreateMockBootstrappedContainer can be only called from tests")
	return &MockBootstrappedContainer{Slots: make(map[string][]byte)}
}

func (m *MockBootstrappedContainer) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	if m.BootstrapKeyRemoved {
		return nil, fmt.Errorf("internal error: key resetter was a already finished")
	}

	if token {
		return nil, fmt.Errorf("not implemented")
	} else {
		_, ok := m.Slots[slotName]
		if ok {
			return nil, fmt.Errorf("slot already taken")
		}
		m.Slots[slotName] = newKey
		return nil, nil
	}
}

func (l *MockBootstrappedContainer) RemoveBootstrapKey() error {
	l.BootstrapKeyRemoved = true
	return nil
}
