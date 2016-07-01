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
	"net/http"
	"strings"

	"github.com/snapcore/snapd/asserts"
)

// TODO: Most things in this file should really be on a struct that can
// be mocked and have its attributes tweaked, rather than replacing
// package globals.
var (
	myappsAPIBase = myappsURL()
	// MyAppsPackageAccessAPI points to MyApps endpoint to get a package access macaroon
	MyAppsPackageAccessAPI = myappsAPIBase + "api/2.0/acl/package_access/"
	DeviceIdentityAPI      = myappsAPIBase + "identity/api/v1"
	ubuntuoneAPIBase       = authURL()
	// UbuntuoneLocation is the Ubuntuone location as defined in the store macaroon
	UbuntuoneLocation = authLocation()
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
	req.Header.Set("User-Agent", userAgent)
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
	req.Header.Set("User-Agent", userAgent)
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

func requestDeviceNonce() ([]byte, error) {
	const errorPrefix = "cannot get device identity nonce from store: "

	req, err := http.NewRequest("POST", DeviceIdentityAPI+"/nonces", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf(errorPrefix+"store server returned status %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	var responseData struct {
		Nonce string `json:"nonce"`
	}
	if err := dec.Decode(&responseData); err != nil {
		return nil, fmt.Errorf(errorPrefix+"%v", err)
	}

	if responseData.Nonce == "" {
		return nil, fmt.Errorf(errorPrefix + "no nonce returned")
	}
	return []byte(responseData.Nonce), nil
}

func requestDeviceMacaroon(serialAssertion []byte, nonce []byte, signature []byte) (string, error) {
	const errorPrefix = "cannot get device session macaroon from store: "

	type deviceMacaroonRequest struct {
		SerialAssertion string `json:"serial-assertion"`
		Nonce           string `json:"nonce"`
		Signature       string `json:"signature"`
	}

	jsonData, err := json.Marshal(deviceMacaroonRequest{
		SerialAssertion: string(serialAssertion),
		Nonce:           string(nonce),
		Signature:       string(signature),
	})

	req, err := http.NewRequest("POST", DeviceIdentityAPI+"/sessions", bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// TODO: We should return a more useful error message if
		// possible. The store's device macaroon APIs produce
		// error objects in a different format from the user
		// macaroon APIs, so they should probably be assimilated
		// first.
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
		return "", fmt.Errorf(errorPrefix + "no macaroon returned")
	}
	return responseData.Macaroon, nil
}

type signNonceFunc func(deviceKeyEncoded []byte, nonce []byte) ([]byte, error)

// AcquireDeviceMacaroon obtains a macaroon for a device session by requesting and signing a nonce.
func AcquireDeviceMacaroon(serialAssertionEncoded []byte, signNonceFunc signNonceFunc) (string, error) {
	const errorPrefix = "cannot get device session macaroon from store: "

	// Extract the public key from the assertion, to pass to the
	// nonce signing function.
	assert, err := asserts.Decode(serialAssertionEncoded)
	if err != nil {
		return "", err
	}
	if assert.Type() != asserts.SerialType {
		return "", fmt.Errorf("serial assertion is actually %s", assert.Type())
	}
	serial := assert.(*asserts.Serial)
	deviceKeyEncoded, err := asserts.EncodePublicKey(serial.DeviceKey())
	if err != nil {
		return "", err
	}

	// Get a nonce from the store, sign it, and then exchange it for
	// a macaroon.
	nonce, err := requestDeviceNonce()
	if err != nil {
		return "", err
	}

	signature, err := signNonceFunc(deviceKeyEncoded, nonce)
	if err != nil {
		return "", err
	}

	macaroon, err := requestDeviceMacaroon(serialAssertionEncoded, nonce, signature)
	if err != nil {
		return "", err
	}

	return macaroon, nil
}
