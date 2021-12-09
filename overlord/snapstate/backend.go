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
	"context"
	"io"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
)

// A StoreService can find, list available updates and download snaps.
type StoreService interface {
	EnsureDeviceSession() (*auth.DeviceState, error)

	SnapInfo(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (*snap.Info, error)
	SnapExists(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (naming.SnapRef, *channel.Channel, error)
	Find(ctx context.Context, search *store.Search, user *auth.UserState) ([]*snap.Info, error)

	SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error)

	Sections(ctx context.Context, user *auth.UserState) ([]string, error)
	WriteCatalogs(ctx context.Context, names io.Writer, adder store.SnapAdder) error

	Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState, *store.DownloadOptions) error
	DownloadStream(context.Context, string, *snap.DownloadInfo, int64, *auth.UserState) (r io.ReadCloser, status int, err error)

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)
	SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error)
	DownloadAssertions([]string, *asserts.Batch, *auth.UserState) error

	SuggestedCurrency() string
	Buy(options *client.BuyOptions, user *auth.UserState) (*client.BuyResult, error)
	ReadyToBuy(*auth.UserState) error
	ConnectivityCheck() (map[string]bool, error)
	CreateCohorts(context.Context, []string) (map[string]string, error)

	LoginUser(username, password, otp string) (string, string, error)
	UserInfo(email string) (userinfo *store.User, err error)
}

type managerBackend interface {
	// install related
	SetupSnap(snapFilePath, instanceName string, si *snap.SideInfo, dev boot.Device, opts *backend.SetupSnapOptions, meter progress.Meter) (snap.Type, *backend.InstallRecord, error)
	CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter, opts *dirs.SnapDirOptions) error
	LinkSnap(info *snap.Info, dev boot.Device, linkCtx backend.LinkContext, tm timings.Measurer) (rebootRequired bool, err error)
	StartServices(svcs []*snap.AppInfo, disabledSvcs []string, meter progress.Meter, tm timings.Measurer) error
	StopServices(svcs []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter, tm timings.Measurer) error
	ServicesEnableState(info *snap.Info, meter progress.Meter) (map[string]bool, error)
	QueryDisabledServices(info *snap.Info, pb progress.Meter) ([]string, error)

	// the undoers for install
	UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, installRecord *backend.InstallRecord, dev boot.Device, meter progress.Meter) error
	UndoCopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter, opts *dirs.SnapDirOptions) error
	// cleanup
	ClearTrashedData(oldSnap *snap.Info)

	// remove related
	UnlinkSnap(info *snap.Info, linkCtx backend.LinkContext, meter progress.Meter) error
	RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, installRecord *backend.InstallRecord, dev boot.Device, meter progress.Meter) error
	RemoveSnapDir(s snap.PlaceInfo, hasOtherInstances bool) error
	RemoveSnapData(info *snap.Info, opts *dirs.SnapDirOptions) error
	RemoveSnapCommonData(info *snap.Info, opts *dirs.SnapDirOptions) error
	RemoveSnapDataDir(info *snap.Info, hasOtherInstances bool) error
	RemoveSnapMountUnits(s snap.PlaceInfo, meter progress.Meter) error
	DiscardSnapNamespace(snapName string) error
	RemoveSnapInhibitLock(snapName string) error

	// alias related
	UpdateAliases(add []*backend.Alias, remove []*backend.Alias) error
	RemoveSnapAliases(snapName string) error

	// testing helpers
	CurrentInfo(cur *snap.Info)
	Candidate(sideInfo *snap.SideInfo)

	// refresh related
	RunInhibitSnapForUnlink(info *snap.Info, hint runinhibit.Hint, decision func() error) (*osutil.FileLock, error)
	// (not a backend method because doInstall cannot access the backend)
	// WithSnapLock(info *snap.Info, action func() error) error

	// ~/.snap/data migration related
	HideSnapData(snapName string) error
	UndoHideSnapData(snapName string) error
}
