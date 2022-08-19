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
	"net/http"
	"os"

	"github.com/mvo5/goconfigparser"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
)

type authData struct {
	Macaroon   string
	Discharges []string
}

func getAuthorizer() (store.Authorizer, error) {
	authFn := os.Getenv("UBUNTU_STORE_AUTH_DATA_FILENAME")
	if authFn == "" {
		return nil, nil
	}

	data, err := ioutil.ReadFile(authFn)
	if err != nil {
		return nil, fmt.Errorf("cannot read auth file %q: %v", authFn, err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("invalid auth file %q: empty", authFn)
	}

	what := fmt.Sprintf("file %q", authFn)
	creds, err := parseAuthData(data, what, parseAuthJSON, parseSnapcraftLoginFile)
	if err != nil {
		return nil, err
	}

	return &UbuntuOneCreds{User: auth.UserState{
		StoreMacaroon:   creds.Macaroon,
		StoreDischarges: creds.Discharges,
	}}, nil
}

func parseAuthData(data []byte, what string, parsers ...func(data []byte, what string) (parsed *authData, likely bool, err error)) (*authData, error) {
	var firstErr error
	for _, p := range parsers {
		parsed, likely, err := p(data, what)
		if err == nil {
			return parsed, nil
		}
		if likely && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		return nil, fmt.Errorf("invalid auth %s: not a recognizable format", what)
	}
	return nil, firstErr
}

func parseAuthJSON(data []byte, what string) (*authData, bool, error) {
	var creds struct {
		Macaroon   string   `json:"macaroon"`
		Discharges []string `json:"discharges"`
	}
	err := json.Unmarshal(data, &creds)
	if err != nil {
		likely := data[0] == '{'
		return nil, likely, fmt.Errorf("cannot decode auth %s: %v", what, err)
	}
	if creds.Macaroon == "" || len(creds.Discharges) == 0 {
		return nil, true, fmt.Errorf("invalid auth %s: missing fields", what)
	}
	return &authData{
		Macaroon:   creds.Macaroon,
		Discharges: creds.Discharges,
	}, false, nil
}

func snapcraftLoginSection() string {
	if snapdenv.UseStagingStore() {
		return "login.staging.ubuntu.com"
	}
	return "login.ubuntu.com"
}

func parseSnapcraftLoginFile(data []byte, what string) (*authData, bool, error) {
	errPrefix := fmt.Sprintf("invalid snapcraft login %s", what)

	cfg := goconfigparser.New()
	// XXX this seems to almost always succeed
	if err := cfg.ReadString(string(data)); err != nil {
		likely := data[0] == '['
		return nil, likely, fmt.Errorf("%s: %v", errPrefix, err)
	}
	sec := snapcraftLoginSection()
	macaroon, err := cfg.Get(sec, "macaroon")
	if err != nil {
		return nil, true, fmt.Errorf("%s: %s", errPrefix, err)
	}
	unboundDischarge, err := cfg.Get(sec, "unbound_discharge")
	if err != nil {
		return nil, true, fmt.Errorf("%s: %v", errPrefix, err)
	}
	if macaroon == "" || unboundDischarge == "" {
		return nil, true, fmt.Errorf("%s: empty fields", errPrefix)
	}
	return &authData{
		Macaroon:   macaroon,
		Discharges: []string{unboundDischarge},
	}, false, nil
}

// UbuntuOneCreds can authorize requests using the implicitly carried
// SSO/U1 user credentials.
type UbuntuOneCreds struct {
	User auth.UserState
}

// expected interfaces
var _ store.Authorizer = (*UbuntuOneCreds)(nil)
var _ store.RefreshingAuthorizer = (*UbuntuOneCreds)(nil)

func (c *UbuntuOneCreds) Authorize(r *http.Request, _ store.DeviceAndAuthContext, user *auth.UserState, _ *store.AuthorizeOptions) error {
	return store.UserAuthorizer{}.Authorize(r, nil, &c.User, nil)
}

func (c *UbuntuOneCreds) HasAuth(_ *auth.UserState) bool {
	return true
}

func (c *UbuntuOneCreds) RefreshAuth(_ store.AuthRefreshNeed, _ store.DeviceAndAuthContext, user *auth.UserState, client *http.Client) error {
	return store.UserAuthorizer{}.RefreshUser(&c.User, c, client)
}

func (c *UbuntuOneCreds) UpdateUserAuth(user *auth.UserState, discharges []string) (*auth.UserState, error) {
	user.StoreDischarges = discharges
	return user, nil
}
