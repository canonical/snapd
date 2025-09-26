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

package backend

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/secboot"
)

func MockSsecbootFindFreeHandle(f func() (uint32, error)) (restore func()) {
	old := secbootFindFreeHandle
	secbootFindFreeHandle = f
	return func() {
		secbootFindFreeHandle = old
	}
}

func MockSecbootRevokeOldKeys(f func(uk *secboot.UpdatedKeys, primaryKey []byte) error) (restore func()) {
	old := secbootRevokeOldKeys
	secbootRevokeOldKeys = f
	return func() {
		secbootRevokeOldKeys = old
	}
}

func MockBootIsResealNeeded(f func(pbc boot.PredictableBootChains, bootChainsFile string, expectReseal bool) (ok bool, nextCount int, err error)) (restore func()) {
	old := bootIsResealNeeded
	bootIsResealNeeded = f
	return func() {
		bootIsResealNeeded = old
	}
}

func MockSecbootPCRPolicyCounterHandles(f func(uk secboot.UpdatedKeys) []uint32) (restore func()) {
	old := secbootPCRPolicyCounterHandles
	secbootPCRPolicyCounterHandles = f
	return func() {
		secbootPCRPolicyCounterHandles = old
	}
}
