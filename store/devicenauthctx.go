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

package store

import (
	"errors"
	"net/url"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
)

// A DeviceAndAuthContext mediates access to device and auth information for the store.
type DeviceAndAuthContext interface {
	Device() (*auth.DeviceState, error)

	UpdateDeviceAuth(device *auth.DeviceState, sessionMacaroon string) (actual *auth.DeviceState, err error)

	UpdateUserAuth(user *auth.UserState, discharges []string) (actual *auth.UserState, err error)

	StoreID(fallback string) (string, error)

	DeviceSessionRequestParams(nonce string) (*DeviceSessionRequestParams, error)
	ProxyStoreParams(defaultURL *url.URL) (proxyStoreID string, proxySroreURL *url.URL, err error)

	CloudInfo() (*auth.CloudInfo, error)

	StoreOffline() (bool, error)
}

// DeviceSessionRequestParams gathers the assertions and information to be sent to request a device session.
type DeviceSessionRequestParams struct {
	Request *asserts.DeviceSessionRequest
	Serial  *asserts.Serial
	Model   *asserts.Model
}

func (p *DeviceSessionRequestParams) EncodedRequest() string {
	return string(asserts.Encode(p.Request))
}

func (p *DeviceSessionRequestParams) EncodedSerial() string {
	return string(asserts.Encode(p.Serial))
}

func (p *DeviceSessionRequestParams) EncodedModel() string {
	return string(asserts.Encode(p.Model))
}

var (
	// ErrNoSerial indicates that a device serial is not set yet.
	ErrNoSerial = errors.New("no device serial yet")
)
