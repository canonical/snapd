// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/snapcore/snapd/dirs"
)

func unixDialer(_, _ string) (net.Conn, error) {
	return net.Dial("unix", dirs.SnapdSocket)
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config allows to customize client behavior.
type Config struct {
	// BaseURL contains the base URL where snappy daemon is expected to be.
	// It can be empty for a default behavior of talking over a unix socket.
	BaseURL string
}

// A Client knows how to talk to the snappy daemon.
type Client struct {
	baseURL url.URL
	doer    doer
}

// New returns a new instance of Client
func New(config *Config) *Client {
	// By default talk over an UNIX socket.
	if config == nil || config.BaseURL == "" {
		return &Client{
			baseURL: url.URL{
				Scheme: "http",
				Host:   "localhost",
			},
			doer: &http.Client{
				Transport: &http.Transport{Dial: unixDialer},
			},
		}
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		panic(fmt.Sprintf("cannot parse server base URL: %q (%v)", config.BaseURL, err))
	}
	return &Client{
		baseURL: *baseURL,
		doer:    &http.Client{},
	}
}

func (client *Client) setAuthorization(req *http.Request) error {
	user, err := readAuthData()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `Macaroon root="%s"`, user.Macaroon)
	for _, discharge := range user.Discharges {
		fmt.Fprintf(&buf, `, discharge="%s"`, discharge)
	}
	req.Header.Set("Authorization", buf.String())
	return nil
}

// raw performs a request and returns the resulting http.Response and
// error you usually only need to call this directly if you expect the
// response to not be JSON, otherwise you'd call Do(...) instead.
func (client *Client) raw(method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	// fake a url to keep http.Client happy
	u := client.baseURL
	u.Path = path.Join(client.baseURL.Path, urlpath)
	u.RawQuery = query.Encode()
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// set Authorization header if there are user's credentials
	err = client.setAuthorization(req)
	if err != nil {
		return nil, err
	}

	return client.doer.Do(req)
}

var (
	doRetry   = 250 * time.Millisecond
	doTimeout = 5 * time.Second
)

// MockDoRetry mocks the delays used by the do retry loop.
func MockDoRetry(retry, timeout time.Duration) (restore func()) {
	oldRetry := doRetry
	oldTimeout := doTimeout
	doRetry = retry
	doTimeout = timeout
	return func() {
		doRetry = oldRetry
		doTimeout = oldTimeout
	}
}

// do performs a request and decodes the resulting json into the given
// value. It's low-level, for testing/experimenting only; you should
// usually use a higher level interface that builds on this.
func (client *Client) do(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) error {
	retry := time.NewTicker(doRetry)
	defer retry.Stop()
	timeout := time.After(doTimeout)
	var rsp *http.Response
	var err error
	for {
		rsp, err = client.raw(method, path, query, headers, body)
		if err == nil || method != "GET" {
			break
		}
		select {
		case <-retry.C:
			continue
		case <-timeout:
		}
		break
	}
	if err != nil {
		return fmt.Errorf("cannot communicate with server: %s", err)
	}
	defer rsp.Body.Close()

	if v != nil {
		dec := json.NewDecoder(rsp.Body)
		if err := dec.Decode(v); err != nil {
			r := dec.Buffered()
			buf, err1 := ioutil.ReadAll(r)
			if err1 != nil {
				buf = []byte(fmt.Sprintf("error reading buffered response body: %s", err1))
			}
			return fmt.Errorf("cannot decode %q: %s", buf, err)
		}
	}

	return nil
}

// doSync performs a request to the given path using the specified HTTP method.
// It expects a "sync" response from the API and on success decodes the JSON
// response payload into the given value.
func (client *Client) doSync(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) (*ResultInfo, error) {
	var rsp response
	if err := client.do(method, path, query, headers, body, &rsp); err != nil {
		return nil, err
	}
	if err := rsp.err(); err != nil {
		return nil, err
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("expected sync response, got %q", rsp.Type)
	}

	if v != nil {
		if err := json.Unmarshal(rsp.Result, v); err != nil {
			return nil, fmt.Errorf("cannot unmarshal: %v", err)
		}
	}

	return &rsp.ResultInfo, nil
}

func (client *Client) doAsync(method, path string, query url.Values, headers map[string]string, body io.Reader) (changeID string, err error) {
	var rsp response

	if err := client.do(method, path, query, headers, body, &rsp); err != nil {
		return "", err
	}
	if err := rsp.err(); err != nil {
		return "", err
	}
	if rsp.Type != "async" {
		return "", fmt.Errorf("expected async response for %q on %q, got %q", method, path, rsp.Type)
	}
	if rsp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("operation not accepted")
	}
	if rsp.Change == "" {
		return "", fmt.Errorf("async response without change reference")
	}

	return rsp.Change, nil
}

type ServerVersion struct {
	Version     string
	Series      string
	OSID        string
	OSVersionID string
}

func (client *Client) ServerVersion() (*ServerVersion, error) {
	sysInfo, err := client.SysInfo()
	if err != nil {
		return nil, err
	}

	return &ServerVersion{
		Version:     sysInfo.Version,
		Series:      sysInfo.Series,
		OSID:        sysInfo.OSRelease.ID,
		OSVersionID: sysInfo.OSRelease.VersionID,
	}, nil
}

// A response produced by the REST API will usually fit in this
// (exceptions are the icons/ endpoints obvs)
type response struct {
	Result     json.RawMessage `json:"result"`
	Status     string          `json:"status"`
	StatusCode int             `json:"status-code"`
	Type       string          `json:"type"`
	Change     string          `json:"change"`

	ResultInfo
}

// Error is the real value of response.Result when an error occurs.
type Error struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`

	StatusCode int
}

func (e *Error) Error() string {
	return e.Message
}

const (
	ErrorKindTwoFactorRequired = "two-factor-required"
	ErrorKindTwoFactorFailed   = "two-factor-failed"
	ErrorKindLoginRequired     = "login-required"
)

// IsTwoFactorError returns whether the given error is due to problems
// in two-factor authentication.
func IsTwoFactorError(err error) bool {
	e, ok := err.(*Error)
	if !ok || e == nil {
		return false
	}

	return e.Kind == ErrorKindTwoFactorFailed || e.Kind == ErrorKindTwoFactorRequired
}

// OSRelease contains information about the system extracted from /etc/os-release.
type OSRelease struct {
	ID        string `json:"id"`
	VersionID string `json:"version-id,omitempty"`
}

// SysInfo holds system information
type SysInfo struct {
	Series    string    `json:"series,omitempty"`
	Version   string    `json:"version,omitempty"`
	OSRelease OSRelease `json:"os-release"`
	OnClassic bool      `json:"on-classic"`
}

func (rsp *response) err() error {
	if rsp.Type != "error" {
		return nil
	}
	var resultErr Error
	err := json.Unmarshal(rsp.Result, &resultErr)
	if err != nil || resultErr.Message == "" {
		return fmt.Errorf("server error: %q", rsp.Status)
	}
	resultErr.StatusCode = rsp.StatusCode

	return &resultErr
}

func parseError(r *http.Response) error {
	var rsp response
	if r.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("server error: %q", r.Status)
	}

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&rsp); err != nil {
		return fmt.Errorf("cannot unmarshal error: %v", err)
	}

	err := rsp.err()
	if err == nil {
		return fmt.Errorf("server error: %q", r.Status)
	}
	return err
}

// SysInfo gets system information from the REST API.
func (client *Client) SysInfo() (*SysInfo, error) {
	var sysInfo SysInfo

	if _, err := client.doSync("GET", "/v2/system-info", nil, nil, nil, &sysInfo); err != nil {
		return nil, fmt.Errorf("bad sysinfo result: %v", err)
	}

	return &sysInfo, nil
}

// CreateUserResult holds the result of a user creation
type CreateUserResult struct {
	Username    string `json:"username" yaml:"username"`
	SSHKeyCount int    `json:"ssh-key-count" yaml:"ssh-key-count"`
}

// createUserRequest holds the user creation request
type CreateUserRequest struct {
	Email  string `json:"email"`
	Sudoer bool   `json:"sudoer"`
}

// CreateUser creates a user from the given mail address
func (client *Client) CreateUser(request *CreateUserRequest) (*CreateUserResult, error) {
	var createResult CreateUserResult
	b, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	if _, err := client.doSync("POST", "/v2/create-user", nil, nil, bytes.NewReader(b), &createResult); err != nil {
		return nil, fmt.Errorf("bad user result: %v", err)
	}

	return &createResult, nil
}
