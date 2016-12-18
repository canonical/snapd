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
	"syscall"
	"time"

	"github.com/snapcore/snapd/dirs"
)

func unixDialer() func(string, string) (net.Conn, error) {
	// We have two sockets available: the SnapdSocket (which provides
	// administrative access), and the SnapSocket (which doesn't). Use the most
	// powerful one available (e.g. from within snaps, SnapdSocket is hidden by
	// apparmor unless the snap has the snapd-control interface).
	socketPath := dirs.SnapdSocket
	file, err := os.OpenFile(socketPath, os.O_RDWR, 0666)
	if err == nil {
		file.Close()
	} else if e, ok := err.(*os.PathError); ok && (e.Err == syscall.ENOENT || e.Err == syscall.EACCES) {
		socketPath = dirs.SnapSocket
	}

	return func(_, _ string) (net.Conn, error) {
		return net.Dial("unix", socketPath)
	}
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
				Transport: &http.Transport{Dial: unixDialer()},
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

type RequestError struct{ error }

func (e RequestError) Error() string {
	return fmt.Sprintf("cannot build request: %v", e.error)
}

type AuthorizationError struct{ error }

func (e AuthorizationError) Error() string {
	return fmt.Sprintf("cannot add authorization: %v", e.error)
}

type ConnectionError struct{ error }

func (e ConnectionError) Error() string {
	return fmt.Sprintf("cannot communicate with server: %v", e.error)
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
		return nil, RequestError{err}
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// set Authorization header if there are user's credentials
	err = client.setAuthorization(req)
	if err != nil {
		return nil, AuthorizationError{err}
	}

	rsp, err := client.doer.Do(req)
	if err != nil {
		return nil, ConnectionError{err}
	}

	return rsp, nil
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
		return err
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
	OnClassic   bool
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
		OnClassic:   sysInfo.OnClassic,
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
	ErrorKindTermsNotAccepted  = "terms-not-accepted"
	ErrorKindNoPaymentMethods  = "no-payment-methods"
	ErrorKindPaymentDeclined   = "payment-declined"

	ErrorKindSnapAlreadyInstalled = "snap-already-installed"
	ErrorKindSnapNotInstalled     = "snap-not-installed"
	ErrorKindNoUpdateAvailable    = "snap-no-update-available"
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
	Managed   bool      `json:"managed"`
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
		return nil, fmt.Errorf("cannot obtain system details: %v", err)
	}

	return &sysInfo, nil
}

// CreateUserResult holds the result of a user creation.
type CreateUserResult struct {
	Username string   `json:"username"`
	SSHKeys  []string `json:"ssh-keys"`
}

// CreateUserOptions holds options for creating a local system user.
//
// If Known is false, the provided email is used to query the store for
// username and SSH key details.
//
// If Known is true, the user will be created by looking through existing
// system-user assertions and looking for a matching email. If Email is
// empty then all such assertions are considered and multiple users may
// be created.
type CreateUserOptions struct {
	Email        string `json:"email,omitempty"`
	Sudoer       bool   `json:"sudoer,omitempty"`
	Known        bool   `json:"known,omitempty"`
	ForceManaged bool   `json:"force-managed,omitempty"`
}

// CreateUser creates a local system user. See CreateUserOptions for details.
func (client *Client) CreateUser(options *CreateUserOptions) (*CreateUserResult, error) {
	if options.Email == "" {
		return nil, fmt.Errorf("cannot create a user without providing an email")
	}

	var result CreateUserResult
	data, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}

	if _, err := client.doSync("POST", "/v2/create-user", nil, nil, bytes.NewReader(data), &result); err != nil {
		return nil, fmt.Errorf("while creating user: %v", err)
	}
	return &result, nil
}

// CreateUsers creates multiple local system users. See CreateUserOptions for details.
//
// Results may be provided even if there are errors.
func (client *Client) CreateUsers(options []*CreateUserOptions) ([]*CreateUserResult, error) {
	for _, opts := range options {
		if opts.Email == "" && !opts.Known {
			return nil, fmt.Errorf("cannot create user from store details without an email to query for")
		}
	}

	var results []*CreateUserResult
	var errs []error

	for _, opts := range options {
		data, err := json.Marshal(opts)
		if err != nil {
			return nil, err
		}

		if opts.Email == "" {
			var result []*CreateUserResult
			if _, err := client.doSync("POST", "/v2/create-user", nil, nil, bytes.NewReader(data), &result); err != nil {
				errs = append(errs, err)
			} else {
				results = append(results, result...)
			}
		} else {
			var result *CreateUserResult
			if _, err := client.doSync("POST", "/v2/create-user", nil, nil, bytes.NewReader(data), &result); err != nil {
				errs = append(errs, err)
			} else {
				results = append(results, result)
			}
		}
	}

	if len(errs) == 1 {
		return results, errs[0]
	}
	if len(errs) > 1 {
		var buf bytes.Buffer
		for _, err := range errs {
			fmt.Fprintf(&buf, "\n- %s", err)
		}
		return results, fmt.Errorf("while creating users:%s", buf.Bytes())
	}
	return results, nil
}

// Users returns the local users.
func (client *Client) Users() ([]*User, error) {
	var result []*User

	if _, err := client.doSync("GET", "/v2/users", nil, nil, nil, &result); err != nil {
		return nil, fmt.Errorf("while getting users: %v", err)
	}
	return result, nil
}
