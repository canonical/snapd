// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package storetest

import (
	"context"
	"io"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
)

// Store implements a snapstate.StoreService where every single method panics.
//
// Embed in your own fakeStore to avoid having to keep up with that interface's
// evolution when it's unrelated to your code.
type Store struct{}

// ensure we conform
var _ snapstate.StoreService = Store{}

func (Store) EnsureDeviceSession() (*auth.DeviceState, error) {
	panic("Store.EnsureDeviceSession not expected")
}

func (Store) SnapInfo(context.Context, store.SnapSpec, *auth.UserState) (*snap.Info, error) {
	panic("Store.SnapInfo not expected")
}

func (Store) SnapExists(context.Context, store.SnapSpec, *auth.UserState) (naming.SnapRef, *channel.Channel, error) {
	panic("Store.SnapExists not expected")
}

func (Store) Find(context.Context, *store.Search, *auth.UserState) ([]*snap.Info, error) {
	panic("Store.Find not expected")
}

func (Store) SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, store.AssertionQuery, *auth.UserState, *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	panic("Store.SnapAction not expected")
}

func (Store) Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState, *store.DownloadOptions) error {
	panic("Store.Download not expected")
}

func (Store) DownloadStream(ctx context.Context, name string, downloadInfo *snap.DownloadInfo, resume int64, user *auth.UserState) (io.ReadCloser, int, error) {
	panic("Store.DownloadStream not expected")
}

func (Store) SuggestedCurrency() string {
	panic("Store.SuggestedCurrency not expected")
}

func (Store) Buy(*client.BuyOptions, *auth.UserState) (*client.BuyResult, error) {
	panic("Store.Buy not expected")
}

func (Store) ReadyToBuy(*auth.UserState) error {
	panic("Store.ReadyToBuy not expected")
}

func (Store) Sections(context.Context, *auth.UserState) ([]string, error) {
	panic("Store.Sections not expected")
}

func (Store) Assertion(*asserts.AssertionType, []string, *auth.UserState) (asserts.Assertion, error) {
	panic("Store.Assertion not expected")
}

func (Store) SeqFormingAssertion(*asserts.AssertionType, []string, int, *auth.UserState) (asserts.Assertion, error) {
	panic("Store.SeqFormingAssertion not expected")
}

func (Store) DownloadAssertions([]string, *asserts.Batch, *auth.UserState) error {
	panic("Store.DownloadAssertions not expected")
}

func (Store) WriteCatalogs(context.Context, io.Writer, store.SnapAdder) error {
	panic("fakeStore.WriteCatalogs not expected")
}

func (Store) ConnectivityCheck() (map[string]bool, error) {
	panic("ConnectivityCheck not expected")
}

func (Store) CreateCohorts(context.Context, []string) (map[string]string, error) {
	panic("CreateCohort not expected")
}

func (Store) LoginUser(username, password, otp string) (string, string, error) {
	panic("LoginUser not expected")
}

func (Store) UserInfo(email string) (userinfo *store.User, err error) {
	panic("UserInfo not expected")
}
