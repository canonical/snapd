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

package auth

import (
	"fmt"

	"github.com/ubuntu-core/snappy/overlord/state"
)

// AuthState represents current authenticated users as tracked in state
type AuthState struct {
	LastID int        `json:"lastId"`
	Users  []AuthUser `json:"users"`
}

// AuthUser represents an authenticated user
type AuthUser struct {
	ID         int      `json:"id"`
	Username   string   `json:"username,omitempty"`
	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

// NewUser tracks a new authenticated user and saves its details in the state
func NewUser(st *state.State, username, macaroon string, discharges []string) (*AuthUser, error) {
	authenticatedUser := AuthUser{
		ID:         1,
		Username:   username,
		Macaroon:   macaroon,
		Discharges: discharges,
	}

	// TODO Handle the multi-user case.
	authStateData := AuthState{
		LastID: 1,
		Users:  []AuthUser{authenticatedUser},
	}

	st.Lock()
	st.Set("auth", authStateData)
	st.Unlock()

	return &authenticatedUser, nil
}

// User returns a user from the state given its ID
func User(st *state.State, id int) (*AuthUser, error) {
	var authStateData AuthState

	st.Lock()
	err := st.Get("auth", &authStateData)
	st.Unlock()

	if err != nil {
		return nil, err
	}

	authenticatedUser := authStateData.Users[0]
	if authenticatedUser.ID != id {
		return nil, fmt.Errorf("invalid user")
	}
	return &authenticatedUser, nil
}
