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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"

	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/asserts"
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

// NewUser tracks a new authenticated user and saves its details in the state
func NewUser(st *state.State, username, email, macaroon string, discharges []string) (*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
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

	sort.Strings(discharges)
	authenticatedUser := UserState{
		ID:              authStateData.LastID,
		Username:        username,
		Email:           email,
		Macaroon:        localMacaroon,
		Discharges:      nil,
		StoreMacaroon:   macaroon,
		StoreDischarges: discharges,
	}
	authStateData.Users = append(authStateData.Users, authenticatedUser)

	st.Set("auth", authStateData)

	return &authenticatedUser, nil
}

// RemoveUser removes a user from the state given its ID
func RemoveUser(st *state.State, userID int) error {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if err != nil {
		return err
	}

	for i := range authStateData.Users {
		if authStateData.Users[i].ID == userID {
			// delete without preserving order
			n := len(authStateData.Users) - 1
			authStateData.Users[i] = authStateData.Users[n]
			authStateData.Users[n] = UserState{}
			authStateData.Users = authStateData.Users[:n]
			st.Set("auth", authStateData)
			return nil
		}
	}

	return fmt.Errorf("invalid user")
}

func Users(st *state.State) ([]*UserState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
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

// UpdateUser updates user in state
func UpdateUser(st *state.State, user *UserState) error {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
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

	return fmt.Errorf("invalid user")
}

// Device returns the device details from the state.
func Device(st *state.State) (*DeviceState, error) {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
		return &DeviceState{}, nil
	} else if err != nil {
		return nil, err
	}

	if authStateData.Device == nil {
		return &DeviceState{}, nil
	}

	return authStateData.Device, nil
}

// SetDevice updates the device details in the state.
func SetDevice(st *state.State, device *DeviceState) error {
	var authStateData AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
		authStateData = AuthState{}
	} else if err != nil {
		return err
	}

	authStateData.Device = device
	st.Set("auth", authStateData)

	return nil
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

// DeviceAssertions helps exposing the assertions about device identity.
// All methods should return state.ErrNoState if the underlying needed
// information is not (yet) available.
type DeviceAssertions interface {
	// Model returns the device model assertion.
	Model() (*asserts.Model, error)
	// Serial returns the device model assertion.
	Serial() (*asserts.Serial, error)

	// DeviceSessionRequest produces a device-session-request with the given nonce, it also returns the device serial assertion.
	DeviceSessionRequest(nonce string) (*asserts.DeviceSessionRequest, *asserts.Serial, error)
}

var (
	// ErrNoSerial indicates that a device serial is not set yet.
	ErrNoSerial = errors.New("no device serial yet")
)

// An AuthContext exposes authorization data and handles its updates.
type AuthContext interface {
	Device() (*DeviceState, error)

	UpdateDeviceAuth(device *DeviceState, sessionMacaroon string) (actual *DeviceState, err error)

	UpdateUserAuth(user *UserState, discharges []string) (actual *UserState, err error)

	StoreID(fallback string) (string, error)

	DeviceSessionRequest(nonce string) (devSessionRequest []byte, serial []byte, err error)
}

// authContext helps keeping track of auth data in the state and exposing it.
type authContext struct {
	state         *state.State
	deviceAsserts DeviceAssertions
}

// NewAuthContext returns an AuthContext for state.
func NewAuthContext(st *state.State, deviceAsserts DeviceAssertions) AuthContext {
	return &authContext{state: st, deviceAsserts: deviceAsserts}
}

// Device returns current device state.
func (ac *authContext) Device() (*DeviceState, error) {
	ac.state.Lock()
	defer ac.state.Unlock()

	return Device(ac.state)
}

// UpdateDeviceAuth updates the device auth details in state.
// The last update wins but other device details are left unchanged.
// It returns the updated device state value.
func (ac *authContext) UpdateDeviceAuth(device *DeviceState, newSessionMacaroon string) (actual *DeviceState, err error) {
	ac.state.Lock()
	defer ac.state.Unlock()

	cur, err := Device(ac.state)
	if err != nil {
		return nil, err
	}

	// just do it, last update wins
	cur.SessionMacaroon = newSessionMacaroon
	if err := SetDevice(ac.state, cur); err != nil {
		return nil, fmt.Errorf("internal error: cannot update just read device state: %v", err)
	}

	return cur, nil
}

// UpdateUserAuth updates the user auth details in state.
// The last update wins but other user details are left unchanged.
// It returns the updated user state value.
func (ac *authContext) UpdateUserAuth(user *UserState, newDischarges []string) (actual *UserState, err error) {
	ac.state.Lock()
	defer ac.state.Unlock()

	cur, err := User(ac.state, user.ID)
	if err != nil {
		return nil, err
	}

	// just do it, last update wins
	cur.StoreDischarges = newDischarges
	if err := UpdateUser(ac.state, cur); err != nil {
		return nil, fmt.Errorf("internal error: cannot update just read user state: %v", err)
	}

	return cur, nil
}

// StoreID returns the store id according to system state or
// the fallback one if the state has none set (yet).
func (ac *authContext) StoreID(fallback string) (string, error) {
	storeID := os.Getenv("UBUNTU_STORE_ID")
	if storeID != "" {
		return storeID, nil
	}
	if ac.deviceAsserts != nil {
		mod, err := ac.deviceAsserts.Model()
		if err != nil && err != state.ErrNoState {
			return "", err
		}
		if err == nil {
			storeID = mod.Store()
		}
	}
	if storeID != "" {
		return storeID, nil
	}
	return fallback, nil
}

// DeviceSessionRequest produces a device-session-request with the given nonce, it also returns the encoded device serial assertion. It returns ErrNoSerial if the device serial is not yet initialized.
func (ac *authContext) DeviceSessionRequest(nonce string) (deviceSessionRequest []byte, serial []byte, err error) {
	if ac.deviceAsserts == nil {
		return nil, nil, ErrNoSerial
	}
	req, ser, err := ac.deviceAsserts.DeviceSessionRequest(nonce)
	if err == state.ErrNoState {
		return nil, nil, ErrNoSerial
	}
	if err != nil {
		return nil, nil, err
	}
	return asserts.Encode(req), asserts.Encode(ser), nil
}
