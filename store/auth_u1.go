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

package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/macaroon.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snapdenv"
)

var (
	developerAPIBase = storeDeveloperURL()
	// macaroonACLAPI points to Developer API endpoint to get an ACL macaroon
	MacaroonACLAPI   = developerAPIBase + "dev/api/acl/"
	ubuntuoneAPIBase = authURL()
	// UbuntuoneLocation is the Ubuntuone location as defined in the store macaroon
	UbuntuoneLocation = authLocation()
	// UbuntuoneDischargeAPI points to SSO endpoint to discharge a macaroon
	UbuntuoneDischargeAPI = ubuntuoneAPIBase + "/tokens/discharge"
	// UbuntuoneRefreshDischargeAPI points to SSO endpoint to refresh a discharge macaroon
	UbuntuoneRefreshDischargeAPI = ubuntuoneAPIBase + "/tokens/refresh"
)

// UserAuthorizer authorizes requests using user credentials managed via
// the DeviceAndAuthContext.
type UserAuthorizer struct{}

func (a UserAuthorizer) Authorize(r *http.Request, _ DeviceAndAuthContext, user *auth.UserState, _ *AuthorizeOptions) error {
	// only set user authentication if user logged in to the store
	if !user.HasStoreAuth() {
		return nil
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `Macaroon root="%s"`, user.StoreMacaroon)

	// deserialize root macaroon (we need its signature to do the discharge binding)
	root := mylog.Check2(auth.MacaroonDeserialize(user.StoreMacaroon))

	for _, d := range user.StoreDischarges {
		// prepare discharge for request
		discharge := mylog.Check2(auth.MacaroonDeserialize(d))

		discharge.Bind(root.Signature())

		serializedDischarge := mylog.Check2(auth.MacaroonSerialize(discharge))

		fmt.Fprintf(&buf, `, discharge="%s"`, serializedDischarge)
	}
	r.Header.Set("Authorization", buf.String())
	return nil
}

func (a UserAuthorizer) CanAuthorizeForUser(user *auth.UserState) bool {
	return user.HasStoreAuth()
}

func (a UserAuthorizer) RefreshAuth(need AuthRefreshNeed, dauthCtx DeviceAndAuthContext, user *auth.UserState, client *http.Client) error {
	if user == nil || !need.User {
		return nil
	}
	// refresh user
	return a.RefreshUser(user, dauthCtx, client)
}

type UserAuthUpdater interface {
	UpdateUserAuth(user *auth.UserState, discharges []string) (actual *auth.UserState, err error)
}

// RefreshUser will refresh user discharge macaroon and update state via the UserAuthUpdater.
func (a UserAuthorizer) RefreshUser(user *auth.UserState, upd UserAuthUpdater, client *http.Client) error {
	if upd == nil {
		return fmt.Errorf("user credentials need to be refreshed but update in place only supported in snapd")
	}
	newDischarges := mylog.Check2(refreshDischarges(client, user))

	curUser := mylog.Check2(upd.UpdateUserAuth(user, newDischarges))

	// update in place
	*user = *curUser

	return nil
}

// refreshDischarges will request refreshed discharge macaroons for the user
func refreshDischarges(httpClient *http.Client, user *auth.UserState) ([]string, error) {
	newDischarges := make([]string, len(user.StoreDischarges))
	for i, d := range user.StoreDischarges {
		discharge := mylog.Check2(auth.MacaroonDeserialize(d))

		if discharge.Location() != UbuntuoneLocation {
			newDischarges[i] = d
			continue
		}

		refreshedDischarge := mylog.Check2(refreshDischargeMacaroon(httpClient, d))

		newDischarges[i] = refreshedDischarge
	}
	return newDischarges, nil
}

// lower-level helpers

type ssoMsg struct {
	Code    string                `json:"code"`
	Message string                `json:"message"`
	Extra   map[string]stringList `json:"extra"`
}

// returns true if the http status code is in the "success" range (2xx)
func httpStatusCodeSuccess(httpStatusCode int) bool {
	return httpStatusCode/100 == 2
}

// returns true if the http status code is in the "client-error" range (4xx)
func httpStatusCodeClientError(httpStatusCode int) bool {
	return httpStatusCode/100 == 4
}

// loginCaveatID returns the 3rd party caveat from the macaroon to be discharged by Ubuntuone
func loginCaveatID(m *macaroon.Macaroon) (string, error) {
	caveatID := ""
	for _, caveat := range m.Caveats() {
		if caveat.Location == UbuntuoneLocation {
			caveatID = caveat.Id
			break
		}
	}
	if caveatID == "" {
		return "", fmt.Errorf("missing login caveat")
	}
	return caveatID, nil
}

// requestStoreMacaroon requests a macaroon for accessing package data from the ubuntu store.
func requestStoreMacaroon(httpClient *http.Client) (string, error) {
	const errorPrefix = "cannot get snap access permission from store: "

	data := map[string]interface{}{
		"permissions": []string{"package_access", "package_purchase"},
	}

	macaroonJSONData := mylog.Check2(json.Marshal(data))

	var responseData struct {
		Macaroon string `json:"macaroon"`
	}

	headers := map[string]string{
		"User-Agent":   snapdenv.UserAgent(),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	resp := mylog.Check2(retryPostRequestDecodeJSON(httpClient, MacaroonACLAPI, headers, macaroonJSONData, &responseData, nil))

	// check return code, error on anything !200
	if resp.StatusCode != 200 {
		return "", fmt.Errorf(errorPrefix+"store server returned status %d", resp.StatusCode)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty macaroon returned")
	}
	return responseData.Macaroon, nil
}

func requestDischargeMacaroon(httpClient *http.Client, endpoint string, data map[string]string) (string, error) {
	const errorPrefix = "cannot authenticate to snap store: "

	dischargeJSONData := mylog.Check2(json.Marshal(data))

	var responseData struct {
		Macaroon string `json:"discharge_macaroon"`
	}
	var msg ssoMsg

	headers := map[string]string{
		"User-Agent":   snapdenv.UserAgent(),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	resp := mylog.Check2(retryPostRequestDecodeJSON(httpClient, endpoint, headers, dischargeJSONData, &responseData, &msg))

	// check return code, error on 4xx and anything !200
	switch {
	case httpStatusCodeClientError(resp.StatusCode):
		switch msg.Code {
		case "TWOFACTOR_REQUIRED":
			return "", ErrAuthenticationNeeds2fa
		case "TWOFACTOR_FAILURE":
			return "", Err2faFailed
		case "INVALID_CREDENTIALS":
			return "", ErrInvalidCredentials
		case "INVALID_DATA":
			return "", InvalidAuthDataError(msg.Extra)
		case "PASSWORD_POLICY_ERROR":
			return "", PasswordPolicyError(msg.Extra)
		}

		if msg.Message != "" {
			return "", fmt.Errorf(errorPrefix+"%v", msg.Message)
		}
		fallthrough

	case !httpStatusCodeSuccess(resp.StatusCode):
		return "", fmt.Errorf(errorPrefix+"server returned status %d", resp.StatusCode)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty macaroon returned")
	}
	return responseData.Macaroon, nil
}

// dischargeAuthCaveat returns a macaroon with the store auth caveat discharged.
func dischargeAuthCaveat(httpClient *http.Client, caveat, username, password, otp string) (string, error) {
	data := map[string]string{
		"email":     username,
		"password":  password,
		"caveat_id": caveat,
	}
	if otp != "" {
		data["otp"] = otp
	}

	return requestDischargeMacaroon(httpClient, UbuntuoneDischargeAPI, data)
}

// refreshDischargeMacaroon returns a soft-refreshed discharge macaroon.
func refreshDischargeMacaroon(httpClient *http.Client, discharge string) (string, error) {
	data := map[string]string{
		"discharge_macaroon": discharge,
	}

	return requestDischargeMacaroon(httpClient, UbuntuoneRefreshDischargeAPI, data)
}
