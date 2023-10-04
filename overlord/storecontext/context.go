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

// Package storecontext supplies a pluggable implementation of store.DeviceAndAuthContext.
package storecontext

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
)

// A Backend exposes device information and device identity
// assertions, signing session requests and proxy store assertion.
// Methods can return state.ErrNoState if the underlying needed
// information is not (yet) available. They can also assume the state
// lock is held.
type Backend interface {
	DeviceBackend

	DeviceSessionRequestSigner

	StoreOptions
}

// A DeviceBackend exposes device information and device identity
// assertions.
// Methods can return state.ErrNoState if the underlying needed
// information is not (yet) available. They can also assume the state
// lock is held.
type DeviceBackend interface {
	// Device returns current device state.
	Device() (*auth.DeviceState, error)
	// SetDevice sets the device details in the state.
	SetDevice(device *auth.DeviceState) error

	// Model returns the device model assertion.
	Model() (*asserts.Model, error)
	// Serial returns the device serial assertion.
	Serial() (*asserts.Serial, error)
}

type DeviceSessionRequestSigner interface {
	// SignDeviceSessionRequest produces a signed device-session-request with for given serial assertion and nonce.
	SignDeviceSessionRequest(serial *asserts.Serial, nonce string) (*asserts.DeviceSessionRequest, error)
}

type StoreOptions interface {
	// ProxyStore returns the store assertion for the proxy store if one is set.
	ProxyStore() (*asserts.Store, error)

	// StoreOffline returns a string indicating whether the store should have
	// network access or not
	StoreOffline() (bool, error)
}

// storeContext implements store.DeviceAndAuthContext.
type storeContext struct {
	state *state.State

	deviceBackend    DeviceBackend
	sessionReqSigner DeviceSessionRequestSigner
	storeOptions     StoreOptions
}

var _ store.DeviceAndAuthContext = (*storeContext)(nil)

// New returns a store.DeviceAndAuthContext using the given full-featured Backend.
func New(st *state.State, b Backend) store.DeviceAndAuthContext {
	if b == nil {
		panic("store context backend cannot be nil")
	}
	return NewComposed(st, b, b, b)
}

// NewComposed returns a store.DeviceAndAuthContext using the given backends.
func NewComposed(st *state.State, devb DeviceBackend, srqs DeviceSessionRequestSigner, storeOptions StoreOptions) store.DeviceAndAuthContext {
	if devb == nil || srqs == nil || storeOptions == nil {
		panic("store context composable backends cannot be nil")
	}
	return &storeContext{
		state:            st,
		deviceBackend:    devb,
		sessionReqSigner: srqs,
		storeOptions:     storeOptions,
	}
}

// Device returns current device state.
func (sc *storeContext) Device() (*auth.DeviceState, error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	return sc.deviceBackend.Device()
}

// UpdateDeviceAuth updates the device auth details in state.
// The last update wins but other device details are left unchanged.
// It returns the updated device state value.
func (sc *storeContext) UpdateDeviceAuth(device *auth.DeviceState, newSessionMacaroon string) (actual *auth.DeviceState, err error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	cur, err := sc.deviceBackend.Device()
	if err != nil {
		return nil, err
	}

	// because of remodeling now more than one place (the global store)
	// can be trying to set sessions, don't update if the original session
	// doesn't match
	if cur.SessionMacaroon != device.SessionMacaroon {
		// nothing to do
		return cur, nil
	}

	cur.SessionMacaroon = newSessionMacaroon
	if err := sc.deviceBackend.SetDevice(cur); err != nil {
		return nil, fmt.Errorf("internal error: cannot update just read device state: %v", err)
	}

	return cur, nil
}

// UpdateUserAuth updates the user auth details in state.
// The last update wins but other user details are left unchanged.
// It returns the updated user state value.
func (sc *storeContext) UpdateUserAuth(user *auth.UserState, newDischarges []string) (actual *auth.UserState, err error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	cur, err := auth.User(sc.state, user.ID)
	if err != nil {
		return nil, err
	}

	// just do it, last update wins
	cur.StoreDischarges = newDischarges
	if err := auth.UpdateUser(sc.state, cur); err != nil {
		return nil, fmt.Errorf("internal error: cannot update just read user state: %v", err)
	}

	return cur, nil
}

// StoreID returns the store set in the model assertion, if mod != nil
// and it's not the generic classic model, or the override from the
// UBUNTU_STORE_ID envvar.
func StoreID(mod *asserts.Model) string {
	if mod != nil && mod.Ref().Unique() != sysdb.GenericClassicModel().Ref().Unique() {
		return mod.Store()
	}
	return os.Getenv("UBUNTU_STORE_ID")
}

// StoreID returns the store id according to system state or
// the fallback one if the state has none set (yet).
func (sc *storeContext) StoreID(fallback string) (string, error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	mod, err := sc.deviceBackend.Model()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}

	storeID := StoreID(mod)
	if storeID != "" {
		return storeID, nil
	}

	return fallback, nil
}

type DeviceSessionRequestParams = store.DeviceSessionRequestParams

// DeviceSessionRequestParams produces a device-session-request with the given nonce, together with other required parameters, the device serial and model assertions. It returns store.ErrNoSerial if the device serial is not yet initialized.
func (sc *storeContext) DeviceSessionRequestParams(nonce string) (*DeviceSessionRequestParams, error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	params, err := sc.deviceSessionRequestParams(nonce)
	if errors.Is(err, state.ErrNoState) {
		return nil, store.ErrNoSerial
	}

	return params, err
}

func (sc *storeContext) deviceSessionRequestParams(nonce string) (*DeviceSessionRequestParams, error) {
	model, err := sc.deviceBackend.Model()
	if err != nil {
		return nil, err
	}

	serial, err := sc.deviceBackend.Serial()
	if err != nil {
		return nil, err
	}

	deviceSessionReq, err := sc.sessionReqSigner.SignDeviceSessionRequest(serial, nonce)
	if err != nil {
		return nil, err
	}

	return &DeviceSessionRequestParams{
		Request: deviceSessionReq,
		Serial:  serial,
		Model:   model,
	}, nil
}

// ProxyStoreParams returns the id and URL of the proxy store if one is set. Returns the defaultURL otherwise and id = "".
func (sc *storeContext) ProxyStoreParams(defaultURL *url.URL) (proxyStoreID string, proxySroreURL *url.URL, err error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	sto, err := sc.storeOptions.ProxyStore()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", nil, err
	}

	if sto != nil {
		return sto.Store(), sto.URL(), nil
	}

	return "", defaultURL, nil
}

func (sc *storeContext) StoreOffline() (bool, error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	offline, err := sc.storeOptions.StoreOffline()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}

	return offline, nil
}

// CloudInfo returns the cloud instance information (if available).
func (sc *storeContext) CloudInfo() (*auth.CloudInfo, error) {
	sc.state.Lock()
	defer sc.state.Unlock()

	tr := config.NewTransaction(sc.state)
	var cloudInfo auth.CloudInfo
	err := tr.Get("core", "cloud", &cloudInfo)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}

	if cloudInfo.Name != "" {
		return &cloudInfo, nil
	}

	return nil, nil
}
