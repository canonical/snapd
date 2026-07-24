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
	"os"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/systemd/fdstore"
	"github.com/snapcore/snapd/testutil"
)

type MemfdSecretState = secretState

func (s *secretState) Capacity() uint64 {
	return s.capacity()
}

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

func MockFdstoreAdd(f func(name fdstore.FdName, f *os.File) error) (restore func()) {
	return testutil.Mock(&fdstoreAdd, f)
}

func MockFdstoreGet(f func(name fdstore.FdName) (*os.File, error)) (restore func()) {
	return testutil.Mock(&fdstoreGet, f)
}

func MockUnixMmap(f func(fd int, offset int64, length int, prot int, flags int) ([]byte, error)) (restore func()) {
	return testutil.Mock(&unixMmap, f)
}

func MockUnixMunmap(f func(b []byte) error) (restore func()) {
	return testutil.Mock(&unixMunmap, f)
}

func MockUnixMemfdSecret(f func(flags int) (fd int, err error)) (restore func()) {
	return testutil.Mock(&unixMemfdSecret, f)
}

func MockUnixMemfdCreate(f func(name string, flags int) (fd int, err error)) (restore func()) {
	return testutil.Mock(&unixMemfdCreate, f)
}
