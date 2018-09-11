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

package snapstate

import (
	"io"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// A StoreService can find, list available updates and download snaps.
type StoreService interface {
	SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error)
	Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error)

	SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error)

	Sections(ctx context.Context, user *auth.UserState) ([]string, error)
	WriteCatalogs(ctx context.Context, names io.Writer, adder store.SnapAdder) error

	Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState, *store.DownloadOptions) error

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)

	SuggestedCurrency() string
	Buy(options *store.BuyOptions, user *auth.UserState) (*store.BuyResult, error)
	ReadyToBuy(*auth.UserState) error
	ConnectivityCheck() (map[string]bool, error)
}

type managerBackend interface {
	// install releated
	SetupSnap(snapFilePath, instanceName string, si *snap.SideInfo, meter progress.Meter) (snap.Type, error)
	CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error
	LinkSnap(info *snap.Info, model *asserts.Model) error
	StartServices(svcs []*snap.AppInfo, meter progress.Meter) error
	StopServices(svcs []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter) error

	// the undoers for install
	UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error
	UndoCopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter) error
	// cleanup
	ClearTrashedData(oldSnap *snap.Info)

	// remove related
	UnlinkSnap(info *snap.Info, meter progress.Meter) error
	RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error
	RemoveSnapDir(s snap.PlaceInfo, otherInstances bool) error
	RemoveSnapData(info *snap.Info) error
	RemoveSnapCommonData(info *snap.Info) error
	RemoveSnapDataDir(info *snap.Info, otherInstances bool) error
	DiscardSnapNamespace(snapName string) error

	// alias related
	UpdateAliases(add []*backend.Alias, remove []*backend.Alias) error
	RemoveSnapAliases(snapName string) error

	// testing helpers
	CurrentInfo(cur *snap.Info)
	Candidate(sideInfo *snap.SideInfo)
}
