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

package snapstate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// A StoreService can find, list available updates and download snaps.
type StoreService interface {
	Snap(name, channel string, devmode bool, user *auth.UserState) (*snap.Info, error)
	Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error)
	ListRefresh([]*store.RefreshCandidate, *auth.UserState) ([]*snap.Info, error)

	Download(string, *snap.DownloadInfo, progress.Meter, *auth.UserState) (string, error)

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)

	SuggestedCurrency() string
	Buy(options *store.BuyOptions, user *auth.UserState) (*store.BuyResult, error)
	PaymentMethods(*auth.UserState) (*store.PaymentInformation, error)
}

type managerBackend interface {
	// install releated
	SetupSnap(snapFilePath string, si *snap.SideInfo, meter progress.Meter) error
	CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error
	LinkSnap(info *snap.Info) error
	// the undoers for install
	UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error
	UndoCopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error

	// remove releated
	UnlinkSnap(info *snap.Info, meter progress.Meter) error
	RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error
	RemoveSnapData(info *snap.Info) error
	RemoveSnapCommonData(info *snap.Info) error

	// testing helpers
	CurrentInfo(cur *snap.Info)
	Candidate(sideInfo *snap.SideInfo)
}
