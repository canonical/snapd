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
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
)

// AuthState represents current authenticated users as tracked in state
type AuthState struct {
	LastID int          `json:"last-id"`
	Users  []UserState  `json:"users"`
	Device *DeviceState `json:"device,omitempty"`
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
	Macaroon        string   `json:"macaroon,omitempty"`
	Discharges      []string `json:"discharges,omitempty"`
	StoreMacaroon   string   `json:"store-macaroon,omitempty"`
	StoreDischarges []string `json:"store-discharges,omitempty"`
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

	sort.Strings(discharges)
	authStateData.LastID++
	authenticatedUser := UserState{
		ID:              authStateData.LastID,
		Username:        username,
		Macaroon:        macaroon,
		Discharges:      discharges,
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

	// SerialProof produces a serial-proof with the given nonce. (DEPRECATED)
	SerialProof(nonce string) (*asserts.SerialProof, error)
}

var (
	// ErrNoSerial indicates that a device serial is not set yet.
	ErrNoSerial = errors.New("no device serial yet")
	// ErrConflict indicates there was a conflict trying to update state auth data.
	ErrConflict = errors.New("updating conflict")
)

// An AuthContext exposes authorization data and handles its updates.
type AuthContext interface {
	Device() (*DeviceState, error)

	UpdateDeviceAuth(device *DeviceState, sessionMacaroon string) (actual *DeviceState, err error)

	UpdateUserAuth(user *UserState, discharges []string) (actual *UserState, err error)

	StoreID(fallback string) (string, error)

	Serial() ([]byte, error)                  // DEPRECATED
	SerialProof(nonce string) ([]byte, error) // DEPRECATED

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

// UpdateDeviceAuth updates the device auth details in state if the state
// values haven't changed since device was read out of state, otherwise
// it returns ErrConflict, in both cases it returns the actualized
// device state values.
func (ac *authContext) UpdateDeviceAuth(device *DeviceState, newSessionMacaroon string) (actual *DeviceState, err error) {
	ac.state.Lock()
	defer ac.state.Unlock()

	cur, err := Device(ac.state)
	if err != nil {
		return nil, err
	}

	if cur.SessionMacaroon != device.SessionMacaroon {
		return cur, ErrConflict
	}

	// not conflicting, update
	cur.SessionMacaroon = newSessionMacaroon
	if err := SetDevice(ac.state, cur); err != nil {
		return nil, fmt.Errorf("internal error: cannot update just read device state: %v", err)
	}

	return cur, nil
}

// UpdateUserAuth updates the user auth details in state if the state
// values haven't changed since user was read out of state, otherwise
// it returns ErrConflict, in both cases it returns the actualized
// user state values.
func (ac *authContext) UpdateUserAuth(user *UserState, newDischarges []string) (actual *UserState, err error) {
	ac.state.Lock()
	defer ac.state.Unlock()

	cur, err := User(ac.state, user.ID)
	if err != nil {
		return nil, err
	}

	if len(cur.StoreDischarges) != len(user.StoreDischarges) {
		return cur, ErrConflict
	}

	for i, oldDischarge := range user.StoreDischarges {
		if cur.StoreDischarges[i] != oldDischarge {
			return cur, ErrConflict
		}
	}

	// not conflicting, update
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

// Serial returns the encoded device serial assertion.
func (ac *authContext) Serial() ([]byte, error) {
	if ac.deviceAsserts == nil {
		return nil, state.ErrNoState
	}
	serial, err := ac.deviceAsserts.Serial()
	if err != nil {
		return nil, err
	}
	return asserts.Encode(serial), nil
}

// SerialProof produces a serial-proof with the given nonce.
func (ac *authContext) SerialProof(nonce string) ([]byte, error) {
	if ac.deviceAsserts == nil {
		return nil, state.ErrNoState
	}
	proof, err := ac.deviceAsserts.SerialProof(nonce)
	if err != nil {
		return nil, err
	}
	return asserts.Encode(proof), nil
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
