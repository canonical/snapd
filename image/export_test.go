// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package image

import (
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

func MockToolingStore(sto Store) *ToolingStore {
	return &ToolingStore{sto: sto}
}

var (
	DecodeModelAssertion = decodeModelAssertion
	MakeLabel            = makeLabel
	SetupSeed            = setupSeed
	InstallCloudConfig   = installCloudConfig
)

func (tsto *ToolingStore) User() *auth.UserState {
	return tsto.user
}

func ToolingStoreContext() store.DeviceAndAuthContext {
	return toolingStoreContext{}
}

func (opts *DownloadSnapOptions) Validate() error {
	return opts.validate()
}

var (
	ErrRevisionAndCohort = errRevisionAndCohort
	ErrPathInBase        = errPathInBase

	WriteResolvedContent = writeResolvedContent
)

func MockWriteResolvedContent(f func(prepareImageDir string, info *gadget.Info, gadgetRoot, kernelRoot string) error) (restore func()) {
	oldWriteResolvedContent := writeResolvedContent
	writeResolvedContent = f
	return func() {
		writeResolvedContent = oldWriteResolvedContent
	}
}
