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
	"github.com/snapcore/snapd/overlord/state"
)

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
