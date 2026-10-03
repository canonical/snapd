// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/dirs"
)

// dialSessionAgent connects to a user's session agent
//
// The host portion of the address is interpreted as the numeric user
// ID of the target user.
func dialSessionAgent(network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(dirs.XdgRuntimeDirBase, host, "snapd-session-agent.socket")
	return net.Dial("unix", socket)
}

type Client struct {
	doer *http.Client
	uids map[int]bool
}

func New() *Client {
	transport := &http.Transport{Dial: dialSessionAgent, DisableKeepAlives: true}
	return &Client{
		doer: &http.Client{Transport: transport},
	}
}

// NewForUids creates a Client that sends requests to a specific list of uids
// only.
func NewForUids(uids ...int) *Client {
	cli := New()
	cli.uids = make(map[int]bool, len(uids))
	for _, uid := range uids {
		cli.uids[uid] = true
	}
	return cli
}

type Error struct {
	Kind    string `json:"kind"`
	Value   any    `json:"value"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

type response struct {
	// Not from JSON
	uid        int
	err        error
	statusCode int

	Result json.RawMessage `json:"result"`
	Type   string          `json:"type"`
}

func (resp *response) checkError() {
	if resp.Type != "error" {
		return
	}
	var resultErr Error
	err := json.Unmarshal(resp.Result, &resultErr)
	if err != nil || resultErr.Message == "" {
		resp.err = fmt.Errorf("server error: %q", http.StatusText(resp.statusCode))
	} else {
		resp.err = &resultErr
	}
}

// sendOneRaw performs a request for a single UID, returns the response and/or
// an error.
func (client *Client) sendOneRaw(ctx context.Context, uid int, method, urlpath string, query url.Values, headers map[string]string, body []byte) (*http.Response, error) {
	u := url.URL{
		Scheme:   "http",
		Host:     fmt.Sprintf("%d", uid),
		Path:     urlpath,
		RawQuery: query.Encode(),
	}
	req, err := http.NewRequest(method, u.String(), bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("internal error: %v", err)
	}
	req = req.WithContext(ctx)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return client.doer.Do(req)
}

func (client *Client) sendRequest(ctx context.Context, uid int, method, urlpath string, query url.Values, headers map[string]string, body []byte) *response {
	response := &response{uid: uid}

	httpResp, err := client.sendOneRaw(ctx, uid, method, urlpath, query, headers, body)
	if err != nil {
		response.err = err
		return response
	}

	defer httpResp.Body.Close()
	response.statusCode = httpResp.StatusCode
	response.err = decodeInto(httpResp.Body, &response)
	response.checkError()
	return response
}

// DebugOneRaw allows to make raw queries to the API with the intention of using
// it from the debug code.
func (client *Client) DebugOneRaw(ctx context.Context, uid int, method, urlpath string, query url.Values, headers map[string]string, body []byte) (*http.Response, error) {
	return client.sendOneRaw(ctx, uid, method, urlpath, query, headers, body)
}

func (client *Client) uidIsValidAsTarget(uid int) bool {
	// if uids are provided (i.e there must be entries, otherwise
	// no list is there), then there must be an entry
	if len(client.uids) > 0 {
		return client.uids[uid]
	}
	return true
}

func (client *Client) sessionTargets() ([]int, error) {
	uids, err := clientutil.AvailableUserSessions()
	if err != nil {
		return nil, err
	}

	filtered := make([]int, 0, len(client.uids))
	for _, uid := range uids {
		if client.uidIsValidAsTarget(uid) {
			filtered = append(filtered, uid)
		}
	}
	return filtered, nil
}

// doMany sends the given request to all active user sessions or a subset of them
// defined by optional client.uids field. Please be careful when using this
// method, because it is not aware of the physical user who triggered the request
// and blindly forwards it to all logged in users. Some of them might not have
// the right to see the request (let alone to respond to it).
func (client *Client) doMany(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body []byte) ([]*response, error) {
	uids, err := client.sessionTargets()
	if err != nil {
		return nil, err
	}
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		responses []*response
	)

	for _, uid := range uids {
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			response := client.sendRequest(ctx, uid, method, urlpath, query, headers, body)
			if response != nil {
				mu.Lock()
				defer mu.Unlock()
				responses = append(responses, response)
			}
		}(uid)
	}
	wg.Wait()
	return responses, nil
}

func decodeInto(reader io.Reader, v any) error {
	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		r := dec.Buffered()
		buf, err1 := io.ReadAll(r)
		if err1 != nil {
			buf = []byte(fmt.Sprintf("error reading buffered response body: %s", err1))
		}
		return fmt.Errorf("cannot decode %q: %s", buf, err)
	}
	return nil
}

type SessionInfo struct {
	Version string `json:"version"`
}

func (client *Client) SessionInfo(ctx context.Context) (info map[int]SessionInfo, err error) {
	responses, err := client.doMany(ctx, "GET", "/v1/session-info", nil, nil, nil)
	if err != nil {
		return nil, err
	}

	info = make(map[int]SessionInfo)
	for _, resp := range responses {
		if resp.err != nil {
			if err == nil {
				err = resp.err
			}
			continue
		}
		var si SessionInfo
		if decodeErr := json.Unmarshal(resp.Result, &si); decodeErr != nil {
			if err == nil {
				err = decodeErr
			}
			continue
		}
		info[resp.uid] = si
	}
	return info, err
}

// PendingSnapRefreshInfo holds information about pending snap refresh provided to userd.
type PendingSnapRefreshInfo struct {
	InstanceName        string        `json:"instance-name"`
	TimeRemaining       time.Duration `json:"time-remaining,omitempty"`
	BusyAppName         string        `json:"busy-app-name,omitempty"`
	BusyAppDesktopEntry string        `json:"busy-app-desktop-entry,omitempty"`
}

// PendingRefreshNotification broadcasts information about a refresh.
func (client *Client) PendingRefreshNotification(ctx context.Context, refreshInfo *PendingSnapRefreshInfo) error {
	headers := map[string]string{"Content-Type": "application/json"}
	reqBody, err := json.Marshal(refreshInfo)
	if err != nil {
		return err
	}
	_, err = client.doMany(ctx, "POST", "/v1/notifications/pending-refresh", nil, headers, reqBody)
	return err
}

// FinishedSnapRefreshInfo holds information about a finished refresh provided to userd.
type FinishedSnapRefreshInfo struct {
	InstanceName string `json:"instance-name"`
}

// FinishRefreshNotification closes notification about a snap refresh.
func (client *Client) FinishRefreshNotification(ctx context.Context, closeInfo *FinishedSnapRefreshInfo) error {
	headers := map[string]string{"Content-Type": "application/json"}
	reqBody, err := json.Marshal(closeInfo)
	if err != nil {
		return err
	}
	_, err = client.doMany(ctx, "POST", "/v1/notifications/finish-refresh", nil, headers, reqBody)
	return err
}
