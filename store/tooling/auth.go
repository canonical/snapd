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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/mvo5/goconfigparser"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
)

type authData struct {
	// Simple
	Scheme string
	Value  string

	// U1/SSO
	Macaroon   string
	Discharges []string
}

type parseAuthFunc func(data []byte, what string) (parsed *authData, likely bool, err error)

func getAuthorizer() (store.Authorizer, error) {
	var data []byte
	var what string
	parsers := []parseAuthFunc{parseAuthBase64JSON, parseAuthJSON, parseSnapcraftLoginFile}
	if envStr := os.Getenv("UBUNTU_STORE_AUTH"); envStr != "" {
		data = []byte(envStr)
		what = "credentials from UBUNTU_STORE_AUTH"
		parsers = []parseAuthFunc{parseAuthBase64JSON}
	} else {
		authFn := os.Getenv("UBUNTU_STORE_AUTH_DATA_FILENAME")
		if authFn == "" {
			return nil, nil
		}

		data = mylog.Check2(os.ReadFile(authFn))

		data = bytes.TrimSpace(data)
		what = fmt.Sprintf("file %q", authFn)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("invalid auth %s: empty", what)
	}

	creds := mylog.Check2(parseAuthData(data, what, parsers...))

	if creds.Scheme != "" {
		return &SimpleCreds{
			Scheme: creds.Scheme,
			Value:  creds.Value,
		}, nil
	}

	return &UbuntuOneCreds{User: auth.UserState{
		StoreMacaroon:   creds.Macaroon,
		StoreDischarges: creds.Discharges,
	}}, nil
}

func parseAuthData(data []byte, what string, parsers ...parseAuthFunc) (*authData, error) {
	var firstErr error
	for _, p := range parsers {
		parsed, likely := mylog.Check3(p(data, what))
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
	mylog.Check(json.Unmarshal(data, &creds))

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

// parseSnapcraftLoginFile parses the content of snapcraft <v7 exported
// login credentials files.
func parseSnapcraftLoginFile(data []byte, what string) (*authData, bool, error) {
	errPrefix := fmt.Sprintf("invalid snapcraft login %s", what)

	cfg := goconfigparser.New()
	mylog.Check(
		// XXX this seems to almost always succeed
		cfg.ReadString(string(data)))

	sec := snapcraftLoginSection()
	macaroon := mylog.Check2(cfg.Get(sec, "macaroon"))

	unboundDischarge := mylog.Check2(cfg.Get(sec, "unbound_discharge"))

	if macaroon == "" || unboundDischarge == "" {
		return nil, true, fmt.Errorf("%s: empty fields", errPrefix)
	}
	return &authData{
		Macaroon:   macaroon,
		Discharges: []string{unboundDischarge},
	}, false, nil
}

// parseAuthBase64JSON parses snapcraft v7+ base64-encoded auth credential data.
func parseAuthBase64JSON(data []byte, what string) (*authData, bool, error) {
	jsonData := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	dataLen := mylog.Check2(base64.StdEncoding.Decode(jsonData, data))

	var m map[string]interface{}
	mylog.Check(json.Unmarshal(jsonData[:dataLen], &m))

	r, _ := m["r"].(string)
	d, _ := m["d"].(string)
	t, _ := m["t"].(string)
	switch {
	case t == "u1-macaroon":
		v, _ := m["v"].(map[string]interface{})
		r, _ = v["r"].(string)
		d, _ = v["d"].(string)
		if r == "" || d == "" {
			break
		}
		fallthrough
	case r != "" && d != "":
		return &authData{
			Macaroon:   r,
			Discharges: []string{d},
		}, false, nil
	case t == "macaroon":
		v, _ := m["v"].(string)
		if v != "" {
			return &authData{
				Scheme: "Macaroon",
				Value:  v,
			}, false, nil
		}
	case t == "bearer":
		v, _ := m["v"].(string)
		if v != "" {
			return &authData{
				Scheme: "Bearer",
				Value:  v,
			}, false, nil
		}
	}
	return nil, true, fmt.Errorf("cannot recognize unmarshalled base64-decoded auth %s: no known field combination set", what)
}

// UbuntuOneCreds can authorize requests using the implicitly carried
// SSO/U1 user credentials.
type UbuntuOneCreds struct {
	User auth.UserState
}

// expected interfaces
var (
	_ store.Authorizer           = (*UbuntuOneCreds)(nil)
	_ store.RefreshingAuthorizer = (*UbuntuOneCreds)(nil)
)

func (c *UbuntuOneCreds) Authorize(r *http.Request, _ store.DeviceAndAuthContext, _ *auth.UserState, _ *store.AuthorizeOptions) error {
	return store.UserAuthorizer{}.Authorize(r, nil, &c.User, nil)
}

func (c *UbuntuOneCreds) CanAuthorizeForUser(_ *auth.UserState) bool {
	// UbuntuOneCreds carries a UserState with auth data by construction
	// so we can authorize using that
	return true
}

func (c *UbuntuOneCreds) RefreshAuth(_ store.AuthRefreshNeed, _ store.DeviceAndAuthContext, user *auth.UserState, client *http.Client) error {
	return store.UserAuthorizer{}.RefreshUser(&c.User, c, client)
}

func (c *UbuntuOneCreds) UpdateUserAuth(user *auth.UserState, discharges []string) (*auth.UserState, error) {
	user.StoreDischarges = discharges
	return user, nil
}

// SimpleCreds can authorize requests using simply scheme/auth value.
type SimpleCreds struct {
	Scheme string
	Value  string
}

func (c *SimpleCreds) Authorize(r *http.Request, _ store.DeviceAndAuthContext, user *auth.UserState, _ *store.AuthorizeOptions) error {
	r.Header.Set("Authorization", fmt.Sprintf("%s %s", c.Scheme, c.Value))
	return nil
}

func (c *SimpleCreds) CanAuthorizeForUser(_ *auth.UserState) bool {
	// SimpleCreds can authorize with the implicit auth data it carries
	// on behalf of the user they were generated for
	return true
}
