// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package boottest

// MockBootloader mocks the bootloader interface and records all
// set/get calls.
type MockBootloader struct {
	BootVars map[string]string

	name    string
	bootdir string
}

func NewMockBootloader(name, bootdir string) *MockBootloader {
	return &MockBootloader{
		name:    name,
		bootdir: bootdir,

		BootVars: make(map[string]string),
	}
}

func (b *MockBootloader) SetBootVar(key, value string) error {
	b.BootVars[key] = value
	return nil
}

func (b *MockBootloader) GetBootVar(key string) (string, error) {
	return b.BootVars[key], nil
}

func (b *MockBootloader) Dir() string {
	return b.bootdir
}

func (b *MockBootloader) Name() string {
	return b.name
}

func (b *MockBootloader) configFile() string {
	return "/boot/mock/mock.cfg"
}
