// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	if n < 1024 {
		panic("key length must be >= 1024")
	}

	oldKeyLength := keyLength
	keyLength = n
	return func() {
		keyLength = oldKeyLength
	}
}

func MockBaseStoreURL(url string) (restore func()) {
	oldURL := baseStoreURL
	baseStoreURL = url
	return func() {
		baseStoreURL = oldURL
	}
}

func MockRetryInterval(interval time.Duration) (restore func()) {
	old := retryInterval
	retryInterval = interval
	return func() {
		retryInterval = old
	}
}

func MockMaxTentatives(max int) (restore func()) {
	old := maxTentatives
	maxTentatives = max
	return func() {
		maxTentatives = old
	}
}

func KeypairManager(m *DeviceManager) asserts.KeypairManager {
	return m.keypairMgr
}

func EnsureOperationalShouldBackoff(m *DeviceManager, now time.Time) bool {
	return m.ensureOperationalShouldBackoff(now)
}

func BecomeOperationalBackoff(m *DeviceManager) time.Duration {
	return m.becomeOperationalBackoff
}

func SetLastBecomeOperationalAttempt(m *DeviceManager, t time.Time) {
	m.lastBecomeOperationalAttempt = t
}

func MockRepeatRequestSerial(label string) (restore func()) {
	old := repeatRequestSerial
	repeatRequestSerial = label
	return func() {
		repeatRequestSerial = old
	}
}

func EnsureSeedYaml(m *DeviceManager) error {
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

func EnsureBootOk(m *DeviceManager) error {
	return m.ensureBootOk()
}

func SetBootOkRan(m *DeviceManager, b bool) {
	m.bootOkRan = b
}

var (
	ImportAssertionsFromSeed = importAssertionsFromSeed
	CheckGadgetOrKernel      = checkGadgetOrKernel
	CanAutoRefresh           = canAutoRefresh

	IncEnsureOperationalAttempts = incEnsureOperationalAttempts
	EnsureOperationalAttempts    = ensureOperationalAttempts
)
