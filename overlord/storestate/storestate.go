// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package storestate

import (
	"fmt"
	"net/url"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var storeNew = store.New

// StoreState holds the state for the store in the system.
type StoreState struct {
	// API is the store's base API address.
	API string `json:"api"`
}

// API returns the store's explicit address.
func API(st *state.State) string {
	var storeState StoreState

	err := st.Get("store", &storeState)
	if err != nil {
		return ""
	}

	return storeState.API
}

// updateAPI writes the store's address to persistent state.
func updateAPI(st *state.State, api string) {
	var storeState StoreState
	st.Get("store", &storeState)
	storeState.API = api
	st.Set("store", &storeState)
}

// A StoreService can find, list available updates and download snaps.
type StoreService interface {
	SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error)
	Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error)
	LookupRefresh(*store.RefreshCandidate, *auth.UserState) (*snap.Info, error)
	ListRefresh([]*store.RefreshCandidate, *auth.UserState) ([]*snap.Info, error)
	Sections(user *auth.UserState) ([]string, error)
	Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState) error

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)

	SuggestedCurrency() string
	Buy(options *store.BuyOptions, user *auth.UserState) (*store.BuyResult, error)
	ReadyToBuy(*auth.UserState) error
}

// SetupStore configures the system's initial store.
func SetupStore(st *state.State, authContext auth.AuthContext) error {
	storeConfig, err := initialStoreConfig(st)
	if err != nil {
		return err
	}
	sto := storeNew(storeConfig, authContext)
	replaceAuthContext(st, authContext)
	ReplaceStore(st, sto)
	return nil
}

// SetupStoreAPI replaces the API of the store used by the system. If the API
// URL is nil the store is reverted to the system's default.
func SetupStoreAPI(state *state.State, api *url.URL) error {
	apiState := ""
	config := store.DefaultConfig()
	if api != nil {
		apiState = api.String()
		err := config.SetAPI(api)
		if err != nil {
			return err
		}
	}
	authContext := authContext(state)
	store := store.New(config, authContext)
	ReplaceStore(state, store)
	updateAPI(state, apiState)
	return nil
}

func initialStoreConfig(st *state.State) (*store.Config, error) {
	config := store.DefaultConfig()
	apiState := API(st)
	if apiState != "" {
		api, err := url.Parse(apiState)
		if err != nil {
			return nil, fmt.Errorf("invalid store API URL: %s", err)
		}
		err = config.SetAPI(api)
		if err != nil {
			return nil, err
		}
	}
	return config, nil
}

type cachedAuthContextKey struct{}

func replaceAuthContext(state *state.State, authContext auth.AuthContext) {
	state.Cache(cachedAuthContextKey{}, authContext)
}

func authContext(state *state.State) auth.AuthContext {
	cached := state.Cached(cachedAuthContextKey{})
	if cached != nil {
		return cached.(auth.AuthContext)
	}
	panic("internal error: needing the auth context before managers have initialized it")
}

type cachedStoreKey struct{}

// ReplaceStore replaces the store used by the system.
func ReplaceStore(state *state.State, store StoreService) {
	state.Cache(cachedStoreKey{}, store)
}

func cachedStore(st *state.State) StoreService {
	ubuntuStore := st.Cached(cachedStoreKey{})
	if ubuntuStore == nil {
		return nil
	}
	return ubuntuStore.(StoreService)
}

// the store implementation has the interface consumed here
var _ StoreService = (*store.Store)(nil)

// Store returns the store service used by the system.
func Store(st *state.State) StoreService {
	if cachedStore := cachedStore(st); cachedStore != nil {
		return cachedStore
	}
	panic("internal error: needing the store before managers have initialized it")
}
