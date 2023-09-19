// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2023 Canonical Ltd
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

package main

import (
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

var (
	Run = run
)

func MockGetKeypairManager(f func() (signtool.KeypairManager, error)) (restore func()) {
	r := testutil.Backup(&getKeypairManager)
	getKeypairManager = f
	return r
}

func MockOsGetuid(f func() int) (restore func()) {
	r := testutil.Backup(&osGetuid)
	osGetuid = f
	return r
}

func MockPreseedCore20(f func(opts *preseed.CoreOptions) error) (restore func()) {
	r := testutil.Backup(&preseedCore20)
	preseedCore20 = f
	return r
}

func MockPreseedClassic(f func(dir string) error) (restore func()) {
	r := testutil.Backup(&preseedClassic)
	preseedClassic = f
	return r
}

func MockPreseedClassicReset(f func(dir string) error) (restore func()) {
	r := testutil.Backup(&preseedClassicReset)
	preseedClassicReset = f
	return r
}

func MockResetPreseededChroot(f func(dir string) error) (restore func()) {
	r := testutil.Backup(&preseedResetPreseededChroot)
	preseedResetPreseededChroot = f
	return r
}

func MockStoreNew(f func(*store.Config, store.DeviceAndAuthContext) *store.Store) (restore func()) {
	storeNewOrig := storeNew
	storeNew = f
	return func() {
		storeNew = storeNewOrig
	}
}
