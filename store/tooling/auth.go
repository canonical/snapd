// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package tooling

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"

	"github.com/mvo5/goconfigparser"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
)

type authData struct {
	Macaroon   string   `json:"macaroon"`
	Discharges []string `json:"discharges"`
}

func readAuthFile(authFn string) (*auth.UserState, error) {
	data, err := ioutil.ReadFile(authFn)
	if err != nil {
		return nil, fmt.Errorf("cannot read auth file %q: %v", authFn, err)
	}

	creds, err := parseAuthFile(authFn, data)
	if err != nil {
		// try snapcraft login format instead
		var err2 error
		creds, err2 = parseSnapcraftLoginFile(authFn, data)
		if err2 != nil {
			trimmed := bytes.TrimSpace(data)
			if len(trimmed) > 0 && trimmed[0] == '[' {
				return nil, err2
			}
			return nil, err
		}
	}

	return &auth.UserState{
		StoreMacaroon:   creds.Macaroon,
		StoreDischarges: creds.Discharges,
	}, nil
}

func parseAuthFile(authFn string, data []byte) (*authData, error) {
	var creds authData
	err := json.Unmarshal(data, &creds)
	if err != nil {
		return nil, fmt.Errorf("cannot decode auth file %q: %v", authFn, err)
	}
	if creds.Macaroon == "" || len(creds.Discharges) == 0 {
		return nil, fmt.Errorf("invalid auth file %q: missing fields", authFn)
	}
	return &creds, nil
}

func snapcraftLoginSection() string {
	if snapdenv.UseStagingStore() {
		return "login.staging.ubuntu.com"
	}
	return "login.ubuntu.com"
}

func parseSnapcraftLoginFile(authFn string, data []byte) (*authData, error) {
	errPrefix := fmt.Sprintf("invalid snapcraft login file %q", authFn)

	cfg := goconfigparser.New()
	if err := cfg.ReadString(string(data)); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}
	sec := snapcraftLoginSection()
	macaroon, err := cfg.Get(sec, "macaroon")
	if err != nil {
		return nil, fmt.Errorf("%s: %s", errPrefix, err)
	}
	unboundDischarge, err := cfg.Get(sec, "unbound_discharge")
	if err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}
	if macaroon == "" || unboundDischarge == "" {
		return nil, fmt.Errorf("invalid snapcraft login file %q: empty fields", authFn)
	}
	return &authData{
		Macaroon:   macaroon,
		Discharges: []string{unboundDischarge},
	}, nil
}

// toolingStoreContext implements trivially store.DeviceAndAuthContext
// except implementing UpdateUserAuth properly to be used to refresh a
// soft-expired user macaroon.
// XXX this will not be needed anymore
type toolingStoreContext struct{}

func (tac toolingStoreContext) CloudInfo() (*auth.CloudInfo, error) {
	return nil, nil
}

func (tac toolingStoreContext) Device() (*auth.DeviceState, error) {
	return &auth.DeviceState{}, nil
}

func (tac toolingStoreContext) DeviceSessionRequestParams(_ string) (*store.DeviceSessionRequestParams, error) {
	return nil, store.ErrNoSerial
}

func (tac toolingStoreContext) ProxyStoreParams(defaultURL *url.URL) (proxyStoreID string, proxySroreURL *url.URL, err error) {
	return "", defaultURL, nil
}

func (tac toolingStoreContext) StoreID(fallback string) (string, error) {
	return fallback, nil
}

func (tac toolingStoreContext) UpdateDeviceAuth(_ *auth.DeviceState, newSessionMacaroon string) (*auth.DeviceState, error) {
	return nil, fmt.Errorf("internal error: no device state in tools")
}

func (tac toolingStoreContext) UpdateUserAuth(user *auth.UserState, discharges []string) (*auth.UserState, error) {
	user.StoreDischarges = discharges
	return user, nil
}
