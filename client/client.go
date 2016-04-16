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
	"net"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/ubuntu-core/snappy/dirs"
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

// do performs a request and decodes the resulting json into the given
// value. It's low-level, for testing/experimenting only; you should
// usually use a higher level interface that builds on this.
func (client *Client) do(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) error {
	rsp, err := client.raw(method, path, query, headers, body)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()

	if v != nil {
		dec := json.NewDecoder(rsp.Body)
		if err := dec.Decode(v); err != nil {
			return err
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
		return nil, fmt.Errorf("cannot communicate with server: %s", err)
	}
	if err := rsp.err(); err != nil {
		return nil, err
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("expected sync response, got %q", rsp.Type)
	}

	if err := json.Unmarshal(rsp.Result, v); err != nil {
		return nil, fmt.Errorf("cannot unmarshal: %v", err)
	}

	return &rsp.ResultInfo, nil
}

func (client *Client) doAsync(method, path string, query url.Values, headers map[string]string, body io.Reader) (changeID string, err error) {
	var rsp response

	if err := client.do(method, path, query, headers, body, &rsp); err != nil {
		return "", fmt.Errorf("cannot communicate with server: %v", err)
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
}

func (e *Error) Error() string {
	return e.Message
}

// SysInfo holds system information
type SysInfo struct {
	Flavor           string `json:"flavor"`
	Release          string `json:"release"`
	DefaultChannel   string `json:"default-channel"`
	APICompatibility string `json:"api-compat"`
	Store            string `json:"store,omitempty"`
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
