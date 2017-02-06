// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package devicestate

import (
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
)

func MockKeyLength(n int) (restore func()) {
	oldKeyLength := keyLength
	keyLength = n
	return func() {
		keyLength = oldKeyLength
	}
}

func MockRequestIDURL(url string) (restore func()) {
	oldURL := requestIDURL
	requestIDURL = url
	return func() {
		requestIDURL = oldURL
	}
}

func MockSerialRequestURL(url string) (restore func()) {
	oldURL := serialRequestURL
	serialRequestURL = url
	return func() {
		serialRequestURL = oldURL
	}
}

func MockRetryInterval(interval time.Duration) (restore func()) {
	old := retryInterval
	retryInterval = interval
	return func() {
		retryInterval = old
	}
}

func (m *DeviceManager) KeypairManager() asserts.KeypairManager {
	return m.keypairMgr
}

func MockRepeatRequestSerial(label string) (restore func()) {
	old := repeatRequestSerial
	repeatRequestSerial = label
	return func() {
		repeatRequestSerial = old
	}
}

func (m *DeviceManager) EnsureSeedYaml() error {
	return m.ensureSeedYaml()
}

var PopulateStateFromSeedImpl = populateStateFromSeedImpl

func MockPopulateStateFromSeed(f func(*state.State) ([]*state.TaskSet, error)) (restore func()) {
	old := populateStateFromSeed
	populateStateFromSeed = f
	return func() {
		populateStateFromSeed = old
	}
}

func (m *DeviceManager) EnsureBootOk() error {
	return m.ensureBootOk()
}

func (m *DeviceManager) SetBootOkRan(b bool) {
	m.bootOkRan = b
}

var (
	ImportAssertionsFromSeed = importAssertionsFromSeed
	CheckGadgetOrKernel      = checkGadgetOrKernel
	CanAutoRefresh           = canAutoRefresh
)
