// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"io"
	"io/ioutil"
	"net/http"

	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/httputil"
)

var (
	myappsAPIBase = myappsURL()
	// MyAppsMacaroonACLAPI points to MyApps endpoint to get a ACL macaroon
	MyAppsMacaroonACLAPI = myappsAPIBase + "dev/api/acl/"
	// MyAppsDeviceNonceAPI points to MyApps endpoint to get a nonce
	MyAppsDeviceNonceAPI = myappsAPIBase + "identity/api/v1/nonces"
	// MyAppsDeviceSessionAPI points to MyApps endpoint to get a device session
	MyAppsDeviceSessionAPI = myappsAPIBase + "identity/api/v1/sessions"
	ubuntuoneAPIBase       = authURL()
	// UbuntuoneLocation is the Ubuntuone location as defined in the store macaroon
	UbuntuoneLocation = authLocation()
	// UbuntuoneDischargeAPI points to SSO endpoint to discharge a macaroon
	UbuntuoneDischargeAPI = ubuntuoneAPIBase + "/tokens/discharge"
	// UbuntuoneRefreshDischargeAPI points to SSO endpoint to refresh a discharge macaroon
	UbuntuoneRefreshDischargeAPI = ubuntuoneAPIBase + "/tokens/refresh"
)

type ssoMsg struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Extra   map[string][]string `json:"extra"`
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
func requestStoreMacaroon() (string, error) {
	const errorPrefix = "cannot get snap access permission from store: "

	data := map[string]interface{}{
		"permissions": []string{"package_access", "package_purchase"},
	}
	macaroonJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	req, err := http.NewRequest("POST", MyAppsMacaroonACLAPI, bytes.NewReader(macaroonJSONData))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	defer resp.Body.Close()

	// check return code, error on anything !200
	if resp.StatusCode != 200 {
		return "", fmt.Errorf(errorPrefix+"store server returned status %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	var responseData struct {
		Macaroon string `json:"macaroon"`
	}
	if err := dec.Decode(&responseData); err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty macaroon returned")
	}
	return responseData.Macaroon, nil
}

func requestDischargeMacaroon(endpoint string, data map[string]string) (string, error) {
	const errorPrefix = "cannot authenticate to snap store: "

	dischargeJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(dischargeJSONData))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	defer resp.Body.Close()

	// check return code, error on 4xx and anything !200
	switch {
	case httpStatusCodeClientError(resp.StatusCode):
		// get error details
		var msg ssoMsg
		dec := json.NewDecoder(resp.Body)

		if err := dec.Decode(&msg); err != nil {
			return "", fmt.Errorf(errorPrefix+"%v", err)
		}
		switch msg.Code {
		case "TWOFACTOR_REQUIRED":
			return "", ErrAuthenticationNeeds2fa
		case "TWOFACTOR_FAILURE":
			return "", Err2faFailed
		case "INVALID_DATA":
			return "", ErrInvalidAuthData(msg.Extra)
		}

		if msg.Message != "" {
			return "", fmt.Errorf(errorPrefix+"%v", msg.Message)
		}
		fallthrough

	case !httpStatusCodeSuccess(resp.StatusCode):
		return "", fmt.Errorf(errorPrefix+"server returned status %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	var responseData struct {
		Macaroon string `json:"discharge_macaroon"`
	}
	if err := dec.Decode(&responseData); err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty macaroon returned")
	}
	return responseData.Macaroon, nil
}

// dischargeAuthCaveat returns a macaroon with the store auth caveat discharged.
func dischargeAuthCaveat(caveat, username, password, otp string) (string, error) {
	data := map[string]string{
		"email":     username,
		"password":  password,
		"caveat_id": caveat,
	}
	if otp != "" {
		data["otp"] = otp
	}

	return requestDischargeMacaroon(UbuntuoneDischargeAPI, data)
}

// refreshDischargeMacaroon returns a soft-refreshed discharge macaroon.
func refreshDischargeMacaroon(discharge string) (string, error) {
	data := map[string]string{
		"discharge_macaroon": discharge,
	}

	return requestDischargeMacaroon(UbuntuoneRefreshDischargeAPI, data)
}

// requestStoreDeviceNonce requests a nonce for device authentication against the store.
func requestStoreDeviceNonce() (string, error) {
	const errorPrefix = "cannot get nonce from store: "

	req, err := http.NewRequest("POST", MyAppsDeviceNonceAPI, nil)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	defer resp.Body.Close()

	// check return code, error on anything !200
	if resp.StatusCode != 200 {
		return "", fmt.Errorf(errorPrefix+"store server returned status %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	var responseData struct {
		Nonce string `json:"nonce"`
	}
	if err := dec.Decode(&responseData); err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	if responseData.Nonce == "" {
		return "", fmt.Errorf(errorPrefix + "empty nonce returned")
	}
	return responseData.Nonce, nil
}

// requestDeviceSession requests a device session macaroon from the store.
func requestDeviceSession(serialAssertion, sessionRequest, previousSession string) (string, error) {
	const errorPrefix = "cannot get device session from store: "

	data := map[string]string{
		"serial-assertion":       serialAssertion,
		"device-session-request": sessionRequest,
	}
	deviceJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	req, err := http.NewRequest("POST", MyAppsDeviceSessionAPI, bytes.NewReader(deviceJSONData))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if previousSession != "" {
		req.Header.Set("X-Device-Authorization", fmt.Sprintf(`Macaroon root="%s"`, previousSession))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	defer resp.Body.Close()

	// check return code, error on anything !200
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 1e6)) // do our best to read the body
		return "", fmt.Errorf(errorPrefix+"store server returned status %d and body %q", resp.StatusCode, body)
	}

	dec := json.NewDecoder(resp.Body)
	var responseData struct {
		Macaroon string `json:"macaroon"`
	}
	if err := dec.Decode(&responseData); err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty session returned")
	}
	return responseData.Macaroon, nil
}
