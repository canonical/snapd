// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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
	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type Store struct{}

func (Store) SnapInfo(store.SnapSpec, *auth.UserState) (*snap.Info, error) {
	panic("Store.SnapInfo not expected")
}

func (Store) Find(*store.Search, *auth.UserState) ([]*snap.Info, error) {
	panic("Store.Find not expected")
}

func (Store) LookupRefresh(*store.RefreshCandidate, *auth.UserState) (*snap.Info, error) {
	panic("Store.LookupRefresh not expected")
}

func (Store) ListRefresh([]*store.RefreshCandidate, *auth.UserState) ([]*snap.Info, error) {
	panic("Store.ListRefresh not expected")
}

func (Store) Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState) error {
	panic("Store.Download not expected")
}

func (Store) SuggestedCurrency() string {
	panic("Store.SuggestedCurrency not expected")
}

func (Store) Buy(*store.BuyOptions, *auth.UserState) (*store.BuyResult, error) {
	panic("Store.Buy not expected")
}

func (Store) ReadyToBuy(*auth.UserState) error {
	panic("Store.ReadyToBuy not expected")
}

func (Store) Sections(*auth.UserState) ([]string, error) {
	panic("Store.Sections not expected")
}

func (Store) Assertion(assertType *asserts.AssertionType, key []string, _ *auth.UserState) (asserts.Assertion, error) {
	panic("Store.Assertion not expected")
}
