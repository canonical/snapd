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
	"bytes"
	"fmt"
	"io"

	"github.com/snapcore/snapd/osutil"
)

type KeyDataWriter interface {
	io.Writer
	Commit() error
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
	Tokens              map[string][]byte
}

func CreateMockBootstrappedContainer() *MockBootstrappedContainer {
	osutil.MustBeTestBinary("CreateMockBootstrappedContainer can be only called from tests")
	return &MockBootstrappedContainer{Slots: make(map[string][]byte), Tokens: make(map[string][]byte)}
}

type mockKeyDataWriter struct {
	m        *MockBootstrappedContainer
	slotName string
	buf      bytes.Buffer
}

func (m *mockKeyDataWriter) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockKeyDataWriter) Commit() error {
	m.m.Tokens[m.slotName] = m.buf.Bytes()
	return nil
}

func (m *MockBootstrappedContainer) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	if m.BootstrapKeyRemoved {
		return nil, fmt.Errorf("internal error: key resetter was a already finished")
	}

	_, ok := m.Slots[slotName]
	if ok {
		return nil, fmt.Errorf("slot already taken")
	}
	m.Slots[slotName] = newKey

	if token {
		return &mockKeyDataWriter{m: m, slotName: slotName}, nil
	} else {
		return nil, nil
	}
}

func (l *MockBootstrappedContainer) RemoveBootstrapKey() error {
	l.BootstrapKeyRemoved = true
	return nil
}
