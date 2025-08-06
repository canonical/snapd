// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package opteetest

// MockClient can be used to mock the implementation of the FDE TA client.
type MockClient struct {
	PresentFn    func() bool
	DecryptKeyFn func(input []byte, handle []byte) ([]byte, error)
	EncryptKeyFn func(input []byte) (handle []byte, sealed []byte, err error)
	LockFn       func() error
	VersionFn    func() (int, error)
}

func (m *MockClient) Present() bool {
	if m.PresentFn == nil {
		panic("unexpected call to Present")
	}
	return m.PresentFn()
}

func (m *MockClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	if m.DecryptKeyFn == nil {
		panic("unexpected call to DecryptKey")
	}
	return m.DecryptKeyFn(input, handle)
}

func (m *MockClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	if m.EncryptKeyFn == nil {
		panic("unexpected call to EncryptKey")
	}
	return m.EncryptKeyFn(input)
}

func (m *MockClient) Lock() error {
	if m.LockFn == nil {
		panic("unexpected call to Lock")
	}
	return m.LockFn()
}

func (m *MockClient) Version() (int, error) {
	if m.VersionFn == nil {
		panic("unexpected call to Version")
	}
	return m.VersionFn()
}
