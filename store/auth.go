// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"net/http"
	"net/url"
	"sync"

	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snapdenv"
)

// An Authorizer can authorize a request using credentials directly or indirectly available.
type Authorizer interface {
	// Authorize authorizes the given request.
	// If implementing multiple kind of authorization at the same
	// time all they should be performed separately ignoring
	// errors, as the higher-level code might as well treat Authorize
	// as best-effort and only log any returned error.
	Authorize(r *http.Request, dauthCtx DeviceAndAuthContext, user *auth.UserState, opts *AuthorizeOptions) error

	// CanAuthorizeForUser should return true if the Authorizer
	// can authorize requests on behalf of a user, either
	// by the Authorizer using implicit data or by using auth data
	// carried by UserState, in which case the availability of
	// that explicit data in user should be checked.
	CanAuthorizeForUser(user *auth.UserState) bool
}

type AuthorizeOptions struct {
	// deviceAuth is set if device authorization should be
	// provided if available.
	deviceAuth bool

	apiLevel apiLevel
}

type RefreshingAuthorizer interface {
	Authorizer

	// Refresh transient authorization data.
	RefreshAuth(need AuthRefreshNeed, dauthCtx DeviceAndAuthContext, user *auth.UserState, client *http.Client) error
}

// AuthRefreshNeed represents which authorization data needs refreshing.
type AuthRefreshNeed struct {
	Device bool
	User   bool
}

func (rn *AuthRefreshNeed) needed() bool {
	return rn.Device || rn.User
}

// deviceAuthorizer authorizes requests using device or user credentials
// managed via the DeviceAndAuthContext.
// This is the default authorizer used by Store if a DeviceAndAuthContext
// is supplied.
type deviceAuthorizer struct {
	UserAuthorizer

	endpointURL func(p string, query url.Values) (*url.URL, error)

	sessionMu sync.Mutex
}

func (a *deviceAuthorizer) Authorize(r *http.Request, dauthCtx DeviceAndAuthContext, user *auth.UserState, opts *AuthorizeOptions) error {
	var firstError error
	if opts.deviceAuth {
		if device, err := dauthCtx.Device(); err == nil && device != nil && device.SessionMacaroon != "" {
			r.Header.Set(hdrSnapDeviceAuthorization[opts.apiLevel], fmt.Sprintf(`Macaroon root="%s"`, device.SessionMacaroon))
		} else if err != nil {
			firstError = err
		}
	}

	if err := a.UserAuthorizer.Authorize(r, dauthCtx, user, opts); err != nil && firstError == nil {
		firstError = err
	}
	return firstError
}

func dropAuthorization(r *http.Request, opts *AuthorizeOptions) {
	if opts.deviceAuth {
		r.Header.Del(hdrSnapDeviceAuthorization[opts.apiLevel])
	}
	r.Header.Del("Authorization")
}

func (a *deviceAuthorizer) EnsureDeviceSession(dauthCtx DeviceAndAuthContext, client *http.Client) error {
	if dauthCtx == nil {
		return fmt.Errorf("internal error: no authContext")
	}

	device, err := dauthCtx.Device()
	if err != nil {
		return err
	}

	if device.SessionMacaroon != "" {
		// we have already a session, nothing to do
		return nil
	}
	if device.Serial == "" {
		return ErrNoSerial
	}
	// we don't have a session yet but have a serial, try
	// to get a session
	return a.refreshDeviceSession(device, dauthCtx, client)
}

// refreshDeviceSession will set or refresh the device session in the state
func (a *deviceAuthorizer) refreshDeviceSession(device *auth.DeviceState, dauthCtx DeviceAndAuthContext, client *http.Client) error {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	// check that no other goroutine has already got a new session etc...
	device1, err := dauthCtx.Device()
	if err != nil {
		return err
	}
	// We can compare device with "device1" here because Device
	// and UpdateDeviceAuth (and the underlying SetDevice)
	// require/use the global state lock, so the reading/setting
	// values have a total order, and device1 cannot come before
	// device in that order. See also:
	// https://github.com/snapcore/snapd/pull/6716#discussion_r277025834
	if device != nil && *device1 != *device {
		// nothing to do
		return nil
	}

	nonceEndpoint, err := a.endpointURL(deviceNonceEndpPath, nil)
	if err != nil {
		return err
	}

	nonce, err := requestStoreDeviceNonce(client, nonceEndpoint.String())
	if err != nil {
		return err
	}

	devSessReqParams, err := dauthCtx.DeviceSessionRequestParams(nonce)
	if err != nil {
		return err
	}

	deviceSessionEndpoint, err := a.endpointURL(deviceSessionEndpPath, nil)
	if err != nil {
		return err
	}

	session, err := requestDeviceSession(client, deviceSessionEndpoint.String(), devSessReqParams, device1.SessionMacaroon)
	if err != nil {
		return err
	}

	if _, err := dauthCtx.UpdateDeviceAuth(device1, session); err != nil {
		return err
	}
	return nil
}

func (a *deviceAuthorizer) RefreshAuth(need AuthRefreshNeed, dauthCtx DeviceAndAuthContext, user *auth.UserState, client *http.Client) error {
	if need.User {
		if err := a.UserAuthorizer.RefreshAuth(need, dauthCtx, user, client); err != nil {
			return err
		}
	}
	if need.Device {
		// refresh device session
		if dauthCtx == nil {
			return fmt.Errorf("internal error: no device and auth context")
		}
		return a.refreshDeviceSession(nil, dauthCtx, client)
	}

	return nil
}

// lower-level helpers

// requestStoreDeviceNonce requests a nonce for device authentication against the store.
func requestStoreDeviceNonce(httpClient *http.Client, deviceNonceEndpoint string) (string, error) {
	const errorPrefix = "cannot get nonce from store: "

	var responseData struct {
		Nonce string `json:"nonce"`
	}

	headers := map[string]string{
		"User-Agent": snapdenv.UserAgent(),
		"Accept":     "application/json",
	}
	resp, err := retryPostRequestDecodeJSON(httpClient, deviceNonceEndpoint, headers, nil, &responseData, nil)
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

type deviceSessionRequestParamsEncoder interface {
	EncodedRequest() string
	EncodedSerial() string
	EncodedModel() string
}

// requestDeviceSession requests a device session macaroon from the store.
func requestDeviceSession(httpClient *http.Client, deviceSessionEndpoint string, paramsEncoder deviceSessionRequestParamsEncoder, previousSession string) (string, error) {
	const errorPrefix = "cannot get device session from store: "

	data := map[string]string{
		"device-session-request": paramsEncoder.EncodedRequest(),
		"serial-assertion":       paramsEncoder.EncodedSerial(),
		"model-assertion":        paramsEncoder.EncodedModel(),
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
		"User-Agent":   snapdenv.UserAgent(),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if previousSession != "" {
		headers["X-Device-Authorization"] = fmt.Sprintf(`Macaroon root="%s"`, previousSession)
	}

	_, err = retryPostRequest(httpClient, deviceSessionEndpoint, headers, deviceJSONData, func(resp *http.Response) error {
		if resp.StatusCode == 200 || resp.StatusCode == 202 {
			return json.NewDecoder(resp.Body).Decode(&responseData)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1e6)) // do our best to read the body
		return fmt.Errorf("store server returned status %d and body %q", resp.StatusCode, body)
	})
	if err != nil {
		return "", fmt.Errorf(errorPrefix+"%v", err)
	}
	// TODO: retry at least once on 400

	if responseData.Macaroon == "" {
		return "", fmt.Errorf(errorPrefix + "empty session returned")
	}

	return responseData.Macaroon, nil
}

// retryPostRequestDecodeJSON calls retryPostRequest and decodes the response into either success or failure.
func retryPostRequestDecodeJSON(httpClient *http.Client, endpoint string, headers map[string]string, data []byte, success interface{}, failure interface{}) (resp *http.Response, err error) {
	return retryPostRequest(httpClient, endpoint, headers, data, func(resp *http.Response) error {
		return decodeJSONBody(resp, success, failure)
	})
}

// retryPostRequest calls doRequest and decodes the response in a retry loop.
func retryPostRequest(httpClient *http.Client, endpoint string, headers map[string]string, data []byte, readResponseBody func(resp *http.Response) error) (*http.Response, error) {
	return httputil.RetryRequest(endpoint, func() (*http.Response, error) {
		req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(data))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		return httpClient.Do(req)
	}, readResponseBody, defaultRetryStrategy)
}

// a stringList is something that can be deserialized from a JSON
// []string or a string, like the values of the "extra" documents in
// error responses
type stringList []string

func (sish *stringList) UnmarshalJSON(bs []byte) error {
	var ss []string
	e1 := json.Unmarshal(bs, &ss)
	if e1 == nil {
		*sish = stringList(ss)
		return nil
	}

	var s string
	e2 := json.Unmarshal(bs, &s)
	if e2 == nil {
		*sish = stringList([]string{s})
		return nil
	}

	return e1
}
