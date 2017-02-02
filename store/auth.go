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
	"time"

	"gopkg.in/macaroon.v1"
	"gopkg.in/retry.v1"

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

// retryPostRequestDecodeJSON calls retryPostRequest and decodes the response into either success or failure.
func retryPostRequestDecodeJSON(endpoint string, headers map[string]string, data []byte, success interface{}, failure interface{}) (resp *http.Response, err error) {
	return retryPostRequest(endpoint, headers, data, func(ok bool, resp *http.Response) error {
		result := success
		if !ok {
			result = failure
		}
		if result != nil {
			return json.NewDecoder(resp.Body).Decode(result)
		}
		return nil
	})
}

// retryPostRequest calls doRequest and decodes the response in a retry loop.
func retryPostRequest(endpoint string, headers map[string]string, data []byte, decode func(ok bool, resp *http.Response) error) (resp *http.Response, err error) {
	var attempt *retry.Attempt
	startTime := time.Now()
	for attempt = retry.Start(defaultRetryStrategy, nil); attempt.Next(); {
		maybeLogRetryAttempt(endpoint, attempt, startTime)

		req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(data))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err = httpClient.Do(req)
		if err != nil {
			if shouldRetryError(attempt, err) {
				continue
			}
			break
		}

		if shouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		} else {
			ok := (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)
			// always decode on success; decode failures only if body is not empty
			if !ok && resp.ContentLength == 0 {
				resp.Body.Close()
				break
			}
			err = decode(ok, resp)
			resp.Body.Close()
			if err != nil {
				if shouldRetryError(attempt, err) {
					continue
				} else {
					return nil, err
				}
			}
		}
		// break out from retry loop
		break
	}
	maybeLogRetrySummary(startTime, endpoint, attempt, resp, err)

	return resp, err
}

// requestStoreMacaroon requests a macaroon for accessing package data from the ubuntu store.
func requestStoreMacaroon() (string, error) {
	const errorPrefix = "cannot get snap access permission from store: "

	data := map[string]interface{}{
		"permissions": []string{"package_access", "package_purchase"},
	}

	var err error
	macaroonJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	var responseData struct {
		Macaroon string `json:"macaroon"`
	}

	headers := map[string]string{
		"User-Agent":   httputil.UserAgent(),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	resp, err := retryPostRequestDecodeJSON(MyAppsMacaroonACLAPI, headers, macaroonJSONData, &responseData, nil)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	// check return code, error on anything !200
	if resp.StatusCode != 200 {
		return "", fmt.Errorf(errorPrefix+"store server returned status %d", resp.StatusCode)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty macaroon returned")
	}
	return responseData.Macaroon, nil
}

func requestDischargeMacaroon(endpoint string, data map[string]string) (string, error) {
	const errorPrefix = "cannot authenticate to snap store: "

	var err error
	dischargeJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	var responseData struct {
		Macaroon string `json:"discharge_macaroon"`
	}
	var msg ssoMsg

	headers := map[string]string{
		"User-Agent":   httputil.UserAgent(),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	resp, err := retryPostRequestDecodeJSON(endpoint, headers, dischargeJSONData, &responseData, &msg)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	// check return code, error on 4xx and anything !200
	switch {
	case httpStatusCodeClientError(resp.StatusCode):
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

	var responseData struct {
		Nonce string `json:"nonce"`
	}

	headers := map[string]string{
		"User-Agent": httputil.UserAgent(),
		"Accept":     "application/json",
	}
	resp, err := retryPostRequestDecodeJSON(MyAppsDeviceNonceAPI, headers, nil, &responseData, nil)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	// check return code, error on anything !200
	if resp.StatusCode != 200 {
		return "", fmt.Errorf(errorPrefix+"store server returned status %d", resp.StatusCode)
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
	var err error
	deviceJSONData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	var responseData struct {
		Macaroon string `json:"macaroon"`
	}

	headers := map[string]string{
		"User-Agent":   httputil.UserAgent(),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if previousSession != "" {
		headers["X-Device-Authorization"] = fmt.Sprintf(`Macaroon root="%s"`, previousSession)
	}

	_, err = retryPostRequest(MyAppsDeviceSessionAPI, headers, deviceJSONData, func(ok bool, resp *http.Response) error {
		if ok {
			return json.NewDecoder(resp.Body).Decode(&responseData)
		}
		body, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 1e6)) // do our best to read the body
		return fmt.Errorf("store server returned status %d and body %q", resp.StatusCode, body)
	})
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty session returned")
	}

	return responseData.Macaroon, nil
}
