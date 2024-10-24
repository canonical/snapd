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

// KeyDataWriter is an interface used by KeyData to write the data to
// persistent storage in an atomic way.
// The interface is compatible with identically named interface
// from github.com/canonical/secboot.
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
	AddKey(slotName string, newKey []byte) error
	//GetTokenWriter returns a keydata writer that writes to the token.
	GetTokenWriter(slotName string) (KeyDataWriter, error)
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

func (m *MockBootstrappedContainer) AddKey(slotName string, newKey []byte) error {
	if m.BootstrapKeyRemoved {
		return fmt.Errorf("internal error: key resetter was a already finished")
	}

	_, ok := m.Slots[slotName]
	if ok {
		return fmt.Errorf("slot already taken")
	}
	m.Slots[slotName] = newKey

	return nil
}

func (m *MockBootstrappedContainer) GetTokenWriter(slotName string) (KeyDataWriter, error) {
	return &mockKeyDataWriter{m: m, slotName: slotName}, nil
}

func (l *MockBootstrappedContainer) RemoveBootstrapKey() error {
	l.BootstrapKeyRemoved = true
	return nil
}
