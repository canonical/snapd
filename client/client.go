// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/ubuntu-core/snappy/dirs"
)

func unixDialer(_, _ string) (net.Conn, error) {
	return net.Dial("unix", dirs.SnapdSocket)
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// A Client knows how to talk to the snappy daemon
type Client struct {
	doer doer
}

// New returns a new instance of Client
func New() *Client {
	tr := &http.Transport{Dial: unixDialer}

	return &Client{
		doer: &http.Client{Transport: tr},
	}
}

// raw performs a request and returns the resulting http.Response and
// error you usually only need to call this directly if you expect the
// response to not be JSON, otherwise you'd call Do(...) instead.
func (client *Client) raw(method, path string, body io.Reader) (*http.Response, error) {
	// fake a url to keep http.Client happy
	u := url.URL{
		Scheme: "http",
		Host:   "localhost",
		Path:   path,
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}

	return client.doer.Do(req)
}

// do performs a request and decodes the resulting json into the given
// value. It's low-level, for testing/experimenting only; you should
// usually use a higher level interface that builds on this.
func (client *Client) do(method, path string, body io.Reader, v interface{}) error {
	rsp, err := client.raw(method, path, body)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()

	dec := json.NewDecoder(rsp.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}

	return nil
}

// A response produced by the REST API will usually fit in this
// (exceptions are the icons/ endpoints obvs)
type response struct {
	Result     json.RawMessage `json:"result"`
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	Type       string          `json:"type"`
}

// SysInfo holds system information
type SysInfo struct {
	Flavor           string `json:"flavor"`
	Release          string `json:"release"`
	DefaultChannel   string `json:"default_channel"`
	APICompatibility string `json:"api_compat"`
	Store            string `json:"store,omitempty"`
}

// SysInfo gets system information from the REST API.
func (client *Client) SysInfo() (*SysInfo, error) {
	var rsp response
	if err := client.do("GET", "/1.0", nil, &rsp); err != nil {
		return nil, err
	}
	if rsp.Type == "error" {
		// TODO: handle structured errors
		return nil, fmt.Errorf("failed with %q", rsp.Status)
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("unexpected result type %q", rsp.Type)
	}

	var sysInfo SysInfo
	if err := json.Unmarshal(rsp.Result, &sysInfo); err != nil {
		return nil, fmt.Errorf("bad sysinfo result %q: %v", rsp.Result, err)
	}

	return &sysInfo, nil
}

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name string `json:"name"`
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string `json:"label"`
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	Type string `json:"type"`
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Capabilities returns the capabilities currently available for snaps to consume.
func (client *Client) Capabilities() (map[string]Capability, error) {
	var rsp response
	if err := client.do("GET", "/1.0/capabilities", nil, &rsp); err != nil {
		return nil, err
	}
	if rsp.Type == "error" {
		// TODO: handle structured errors
		return nil, fmt.Errorf("cannot obtain capabilities: %s", rsp.Status)
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("cannot obtain capabilities: expected sync response, got %s", rsp.Type)
	}
	var result map[string]map[string]Capability
	if err := json.Unmarshal(rsp.Result, &result); err != nil {
		return nil, fmt.Errorf("cannot obtain capabilities: failed to unmarshal response: %v", err)
	}
	return result["capabilities"], nil
}
