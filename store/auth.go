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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/macaroon.v1"
)

var (
	myappsAPIBase = myappsURL()
	// MyAppsMacaroonACLAPI points to MyApps endpoint to get a ACL macaroon
	MyAppsMacaroonACLAPI = myappsAPIBase + "dev/api/acl/"
	ubuntuoneAPIBase     = authURL()
	// UbuntuoneLocation is the Ubuntuone location as defined in the store macaroon
	UbuntuoneLocation = authLocation()
	// UbuntuoneDischargeAPI points to SSO endpoint to discharge a macaroon
	UbuntuoneDischargeAPI = ubuntuoneAPIBase + "/tokens/discharge"
	// UbuntuoneRefreshDischargeAPI points to SSO endpoint to refresh a discharge macaroon
	UbuntuoneRefreshDischargeAPI = ubuntuoneAPIBase + "/tokens/refresh"
)

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

// LoginCaveatID returns the 3rd party caveat from the macaroon to be discharged by Ubuntuone
func LoginCaveatID(m *macaroon.Macaroon) (string, error) {
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

// RequestStoreMacaroon requests a macaroon for accessing package data from the ubuntu store.
func RequestStoreMacaroon() (string, error) {
	const errorPrefix = "cannot get snap access permission from store: "

	data := map[string]interface{}{
		"permissions": []string{"package_access", "package_purchase"},
	}
	macaroonJSONData, err := json.Marshal(data)

	req, err := http.NewRequest("POST", MyAppsMacaroonACLAPI, strings.NewReader(string(macaroonJSONData)))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

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

func requestDischargeMacaroon(endpoint string, data map[string]string) (string, error) {
	const errorPrefix = "cannot authenticate on snap store: "

	dischargeJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(dischargeJSONData)))
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

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

// DischargeAuthCaveat returns a macaroon with the store auth caveat discharged.
func DischargeAuthCaveat(caveat, username, password, otp string) (string, error) {
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

// RefreshDischargeMacaroon returns a soft-refreshed discharge macaroon.
func RefreshDischargeMacaroon(discharge string) (string, error) {
	data := map[string]string{
		"discharge_macaroon": discharge,
	}

	return requestDischargeMacaroon(UbuntuoneRefreshDischargeAPI, data)
}
