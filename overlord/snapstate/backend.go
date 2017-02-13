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
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"

	"golang.org/x/net/context"
)

// A StoreService can find, list available updates and download snaps.
type StoreService interface {
	SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error)
	Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error)
	ListRefresh([]*store.RefreshCandidate, *auth.UserState) ([]*snap.Info, error)
	Sections(user *auth.UserState) ([]string, error)
	Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState) error

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)

	SuggestedCurrency() string
	Buy(options *store.BuyOptions, user *auth.UserState) (*store.BuyResult, error)
	ReadyToBuy(*auth.UserState) error
}

type managerBackend interface {
	// install releated
	SetupSnap(snapFilePath string, si *snap.SideInfo, meter progress.Meter) error
	CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error
	LinkSnap(info *snap.Info) error
	StartSnapServices(info *snap.Info, meter progress.Meter) error
	StopSnapServices(info *snap.Info, meter progress.Meter) error

	// the undoers for install
	UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error
	UndoCopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error
	// cleanup
	ClearTrashedData(oldSnap *snap.Info)

	// remove related
	UnlinkSnap(info *snap.Info, meter progress.Meter) error
	RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error
	RemoveSnapData(info *snap.Info) error
	RemoveSnapCommonData(info *snap.Info) error
	DiscardSnapNamespace(snapName string) error

	// alias related
	MatchingAliases(aliases []*backend.Alias) ([]*backend.Alias, error)
	MissingAliases(aliases []*backend.Alias) ([]*backend.Alias, error)
	UpdateAliases(add []*backend.Alias, remove []*backend.Alias) error
	RemoveSnapAliases(snapName string) error

	// testing helpers
	CurrentInfo(cur *snap.Info)
	Candidate(sideInfo *snap.SideInfo)
}
