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
	"bytes"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/ubuntu-core/snappy/overlord/state"
)

// AuthState represents current authenticated users as tracked in state
type AuthState struct {
	LastID int         `json:"last-id"`
	Users  []UserState `json:"users"`
}

// UserState represents an authenticated user
type UserState struct {
	ID         int      `json:"id"`
	Username   string   `json:"username,omitempty"`
	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

// NewUser tracks a new authenticated user and saves its details in the state
func NewUser(st *state.State, username, macaroon string, discharges []string) (*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
		authStateData = AuthState{}
	} else if err != nil {
		return nil, err
	}

	authStateData.LastID++
	authenticatedUser := UserState{
		ID:         authStateData.LastID,
		Username:   username,
		Macaroon:   macaroon,
		Discharges: discharges,
	}
	authStateData.Users = append(authStateData.Users, authenticatedUser)

	st.Set("auth", authStateData)

	return &authenticatedUser, nil
}

// User returns a user from the state given its ID
func User(st *state.State, id int) (*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)

	if err != nil {
		return nil, err
	}

	for _, user := range authStateData.Users {
		if user.ID == id {
			return &user, nil
		}
	}
	return nil, fmt.Errorf("invalid user")
}

// GetUserFromRequest extracts user information from request and return the respective user in state, if valid
func GetUserFromRequest(st *state.State, req *http.Request) (*UserState, error) {
	// extract macaroons data from request
	header := req.Header.Get("Authorization")
	if header == "" {
		return nil, nil
	}

	const errorMsg = "unauthorized"
	auth := strings.SplitN(header, " ", 2)
	if len(auth) != 2 || auth[0] != "Macaroon" {
		return nil, fmt.Errorf(errorMsg)
	}

	var macaroon string
	var discharges []string
	for _, field := range strings.Split(auth[1], ",") {
		field := strings.TrimSpace(field)
		if strings.HasPrefix(field, `root="`) {
			macaroon = field[6 : len(field)-1]
		}
		if strings.HasPrefix(field, `discharge="`) {
			discharges = append(discharges, field[11:len(field)-1])
		}
	}

	if macaroon == "" || len(discharges) == 0 {
		return nil, fmt.Errorf(errorMsg)
	}

	var authStateData AuthState
	err := st.Get("auth", &authStateData)

	if err != nil {
		return nil, fmt.Errorf(errorMsg)
	}

	for _, user := range authStateData.Users {
		if user.Macaroon == macaroon && reflect.DeepEqual(discharges, user.Discharges) {
			return &user, nil
		}
	}
	return nil, fmt.Errorf(errorMsg)
}

// Authenticator returns MacaroonAuthenticator for current authenticated user represented by UserState
func (us *UserState) Authenticator() *MacaroonAuthenticator {
	return newMacaroonAuthenticator(us.Macaroon, us.Discharges)
}

// MacaroonAuthenticator is a store authenticator based on macaroons
type MacaroonAuthenticator struct {
	Macaroon   string
	Discharges []string
}

func newMacaroonAuthenticator(macaroon string, discharges []string) *MacaroonAuthenticator {
	return &MacaroonAuthenticator{
		Macaroon:   macaroon,
		Discharges: discharges,
	}
}

// Authenticate will add the store expected Authorization header for macaroons
func (ma *MacaroonAuthenticator) Authenticate(r *http.Request) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `Macaroon root="%s"`, ma.Macaroon)
	for _, discharge := range ma.Discharges {
		fmt.Fprintf(&buf, `, discharge="%s"`, discharge)
	}
	r.Header.Set("Authorization", buf.String())
}
