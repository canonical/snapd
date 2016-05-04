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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

var (
	myappsAPIBase = myappsURL()
	// MyAppsPackageAccessAPI points to MyApps endpoint to get a package access macaroon
	MyAppsPackageAccessAPI = myappsAPIBase + "api/2.0/acl/package_access/"
	ubuntuoneAPIBase       = authURL()
	// UbuntuoneDischargeAPI points to SSO endpoint to discharge a macaroon
	UbuntuoneDischargeAPI = ubuntuoneAPIBase + "/tokens/discharge"
)

// Authenticator interface to set required authorization headers for requests to the store
type Authenticator interface {
	Authenticate(r *http.Request)
}

type ssoMsg struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// returns true if the http status code is in the "success" range (2xx)
func httpStatusCodeSuccess(httpStatusCode int) bool {
	return httpStatusCode/100 == 2
}

// returns true if the http status code is in the "client-error" range (4xx)
func httpStatusCodeClientError(httpStatusCode int) bool {
	return httpStatusCode/100 == 4
}

// RequestPackageAccessMacaroon requests a macaroon for accessing package data from the ubuntu store.
func RequestPackageAccessMacaroon() (string, error) {
	const errorPrefix = "cannot get package access macaroon from store: "

	emptyJSONData := "{}"
	req, err := http.NewRequest("POST", MyAppsPackageAccessAPI, strings.NewReader(emptyJSONData))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
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

// DischargeAuthCaveat returns a macaroon with the store auth caveat discharged.
func DischargeAuthCaveat(username, password, macaroon, otp string) (string, error) {
	const errorPrefix = "cannot get discharge macaroon from store: "

	data := map[string]string{
		"email":    username,
		"password": password,
		"macaroon": macaroon,
	}
	if otp != "" {
		data["otp"] = otp
	}
	dischargeJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	req, err := http.NewRequest("POST", UbuntuoneDischargeAPI, strings.NewReader(string(dischargeJSONData)))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
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
		if msg.Code == "TWOFACTOR_REQUIRED" {
			return "", ErrAuthenticationNeeds2fa
		}
		if msg.Code == "TWOFACTOR_FAILURE" {
			return "", Err2faFailed
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
