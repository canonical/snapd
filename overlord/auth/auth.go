// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/overlord/state"
)

// AuthState represents current authenticated users as tracked in state
type AuthState struct {
	LastID      int          `json:"last-id"`
	Users       []UserState  `json:"users"`
	Device      *DeviceState `json:"device,omitempty"`
	MacaroonKey []byte       `json:"macaroon-key,omitempty"`
}

// DeviceState represents the device's identity and store credentials
type DeviceState struct {
	// Brand refers to the brand-id
	Brand  string `json:"brand,omitempty"`
	Model  string `json:"model,omitempty"`
	Serial string `json:"serial,omitempty"`

	KeyID string `json:"key-id,omitempty"`

	SessionMacaroon string `json:"session-macaroon,omitempty"`
}

// UserState represents an authenticated user
type UserState struct {
	ID              int      `json:"id"`
	Username        string   `json:"username,omitempty"`
	Email           string   `json:"email,omitempty"`
	Macaroon        string   `json:"macaroon,omitempty"`
	Discharges      []string `json:"discharges,omitempty"`
	StoreMacaroon   string   `json:"store-macaroon,omitempty"`
	StoreDischarges []string `json:"store-discharges,omitempty"`
}

// identificationOnly returns a *UserState with only the
// identification information from u.
func (u *UserState) identificationOnly() *UserState {
	return &UserState{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
	}
}

// HasStoreAuth returns true if the user has store authorization.
func (u *UserState) HasStoreAuth() bool {
	if u == nil {
		return false
	}
	return u.StoreMacaroon != ""
}

// MacaroonSerialize returns a store-compatible serialized representation of the given macaroon
func MacaroonSerialize(m *macaroon.Macaroon) (string, error) {
	marshalled, err := m.MarshalBinary()
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(marshalled)
	return encoded, nil
}

// MacaroonDeserialize returns a deserialized macaroon from a given store-compatible serialization
func MacaroonDeserialize(serializedMacaroon string) (*macaroon.Macaroon, error) {
	var m macaroon.Macaroon
	decoded, err := base64.RawURLEncoding.DecodeString(serializedMacaroon)
	if err != nil {
		return nil, err
	}
	err = m.UnmarshalBinary(decoded)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// generateMacaroonKey generates a random key to sign snapd macaroons
func generateMacaroonKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

const snapdMacaroonLocation = "snapd"

// newUserMacaroon returns a snapd macaroon for the given username
func newUserMacaroon(macaroonKey []byte, userID int) (string, error) {
	userMacaroon, err := macaroon.New(macaroonKey, strconv.Itoa(userID), snapdMacaroonLocation)
	if err != nil {
		return "", fmt.Errorf("cannot create macaroon for snapd user: %s", err)
	}

	serializedMacaroon, err := MacaroonSerialize(userMacaroon)
	if err != nil {
		return "", fmt.Errorf("cannot serialize macaroon for snapd user: %s", err)
	}

	return serializedMacaroon, nil
}

// TODO: possibly move users' related functions to a userstate package

type NewUserData struct {
	// Username is the name of the user on the system
	Username string
	// Email is the email associated with the user
	Email string
	// Macaroon is the store associated macaroon for the user
	Macaroon string
	// Discharges
	Discharges []string
}

// NewUser tracks a new authenticated user and saves its details in the state
func NewUser(st *state.State, userData NewUserData) (*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if errors.Is(err, state.ErrNoState) {
		authStateData = AuthState{}
	} else if err != nil {
		return nil, err
	}

	if authStateData.MacaroonKey == nil {
		authStateData.MacaroonKey, err = generateMacaroonKey()
		if err != nil {
			return nil, err
		}
	}

	authStateData.LastID++

	localMacaroon, err := newUserMacaroon(authStateData.MacaroonKey, authStateData.LastID)
	if err != nil {
		return nil, err
	}

	sort.Strings(userData.Discharges)
	authenticatedUser := UserState{
		ID:              authStateData.LastID,
		Username:        userData.Username,
		Email:           userData.Email,
		Macaroon:        localMacaroon,
		Discharges:      nil,
		StoreMacaroon:   userData.Macaroon,
		StoreDischarges: userData.Discharges,
	}
	authStateData.Users = append(authStateData.Users, authenticatedUser)

	st.Set("auth", authStateData)

	return &authenticatedUser, nil
}

var ErrInvalidUser = errors.New("invalid user")

// RemoveUser removes a user from the state given its ID.
func RemoveUser(st *state.State, userID int) (removed *UserState, err error) {
	return removeUser(st, func(u *UserState) bool { return u.ID == userID })
}

// RemoveUserByUsername removes a user from the state given its username. Returns a *UserState with the identification information for them.
func RemoveUserByUsername(st *state.State, username string) (removed *UserState, err error) {
	return removeUser(st, func(u *UserState) bool { return u.Username == username })
}

// removeUser removes the first user matching given predicate.
func removeUser(st *state.State, p func(*UserState) bool) (*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if errors.Is(err, state.ErrNoState) {
		return nil, ErrInvalidUser
	}
	if err != nil {
		return nil, err
	}

	for i := range authStateData.Users {
		u := &authStateData.Users[i]
		if p(u) {
			removed := u.identificationOnly()
			// delete without preserving order
			n := len(authStateData.Users) - 1
			authStateData.Users[i] = authStateData.Users[n]
			authStateData.Users[n] = UserState{}
			authStateData.Users = authStateData.Users[:n]
			st.Set("auth", authStateData)
			return removed, nil
		}
	}

	return nil, ErrInvalidUser
}

func Users(st *state.State) ([]*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if errors.Is(err, state.ErrNoState) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	users := make([]*UserState, len(authStateData.Users))
	for i := range authStateData.Users {
		users[i] = &authStateData.Users[i]
	}
	return users, nil
}

// User returns a user from the state given its ID.
func User(st *state.State, id int) (*UserState, error) {
	return findUser(st, func(u *UserState) bool { return u.ID == id })
}

// UserByUsername returns a user from the state given its username.
func UserByUsername(st *state.State, username string) (*UserState, error) {
	return findUser(st, func(u *UserState) bool { return u.Username == username })
}

// findUser finds the first user matching given predicate.
func findUser(st *state.State, p func(*UserState) bool) (*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if errors.Is(err, state.ErrNoState) {
		return nil, ErrInvalidUser
	}
	if err != nil {
		return nil, err
	}

	for i := range authStateData.Users {
		u := &authStateData.Users[i]
		if p(u) {
			return u, nil
		}
	}
	return nil, ErrInvalidUser
}

// UpdateUser updates user in state
func UpdateUser(st *state.State, user *UserState) error {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if errors.Is(err, state.ErrNoState) {
		return ErrInvalidUser
	}
	if err != nil {
		return err
	}

	for i := range authStateData.Users {
		if authStateData.Users[i].ID == user.ID {
			authStateData.Users[i] = *user
			st.Set("auth", authStateData)
			return nil
		}
	}

	return ErrInvalidUser
}

var ErrInvalidAuth = fmt.Errorf("invalid authentication")

// CheckMacaroon returns the UserState for the given macaroon/discharges credentials
func CheckMacaroon(st *state.State, macaroon string, discharges []string) (*UserState, error) {
	var authStateData AuthState
	err := st.Get("auth", &authStateData)
	if err != nil {
		return nil, ErrInvalidAuth
	}

	snapdMacaroon, err := MacaroonDeserialize(macaroon)
	if err != nil {
		return nil, ErrInvalidAuth
	}
	// attempt snapd macaroon verification
	if snapdMacaroon.Location() == snapdMacaroonLocation {
		// no caveats to check so far
		check := func(caveat string) error { return nil }
		// ignoring discharges, unused for snapd macaroons atm
		err = snapdMacaroon.Verify(authStateData.MacaroonKey, check, nil)
		if err != nil {
			return nil, ErrInvalidAuth
		}
		macaroonID := snapdMacaroon.Id()
		userID, err := strconv.Atoi(macaroonID)
		if err != nil {
			return nil, ErrInvalidAuth
		}
		user, err := User(st, userID)
		if err != nil {
			return nil, ErrInvalidAuth
		}
		if macaroon != user.Macaroon {
			return nil, ErrInvalidAuth
		}
		return user, nil
	}

	// if macaroon is not a snapd macaroon, fallback to previous token-style check
NextUser:
	for _, user := range authStateData.Users {
		if user.Macaroon != macaroon {
			continue
		}
		if len(user.Discharges) != len(discharges) {
			continue
		}
		// sort discharges (stored users' discharges are already sorted)
		sort.Strings(discharges)
		for i, d := range user.Discharges {
			if d != discharges[i] {
				continue NextUser
			}
		}
		return &user, nil
	}
	return nil, ErrInvalidAuth
}

// CloudInfo reflects cloud information for the system (as captured in the core configuration).
type CloudInfo struct {
	Name             string `json:"name"`
	Region           string `json:"region,omitempty"`
	AvailabilityZone string `json:"availability-zone,omitempty"`
}

type ensureContextKey struct{}

// EnsureContextTODO returns a provisional context marked as
// pertaining to an Ensure loop.
// TODO: see Overlord.Loop to replace it with a proper context passed to all Ensures.
func EnsureContextTODO() context.Context {
	ctx := context.TODO()
	return context.WithValue(ctx, ensureContextKey{}, struct{}{})
}

// IsEnsureContext returns whether context was marked as pertaining to an Ensure loop.
func IsEnsureContext(ctx context.Context) bool {
	return ctx.Value(ensureContextKey{}) != nil
}
