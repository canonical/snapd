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
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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
	uids []int
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
	cli.uids = append(cli.uids, uids...)
	return cli
}

type Error struct {
	Kind    string      `json:"kind"`
	Value   interface{} `json:"value"`
	Message string      `json:"message"`
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

func (client *Client) sendRequest(ctx context.Context, socket string, method, urlpath string, query url.Values, headers map[string]string, body []byte) *response {
	uidStr := filepath.Base(filepath.Dir(socket))
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		// Ignore directories that do not
		// appear to be valid XDG runtime dirs
		// (i.e. /run/user/NNNN).
		return nil
	}
	response := &response{uid: uid}

	u := url.URL{
		Scheme:   "http",
		Host:     uidStr,
		Path:     urlpath,
		RawQuery: query.Encode(),
	}
	req, err := http.NewRequest(method, u.String(), bytes.NewBuffer(body))
	if err != nil {
		response.err = fmt.Errorf("internal error: %v", err)
		return response
	}
	req = req.WithContext(ctx)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	httpResp, err := client.doer.Do(req)
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

// doMany sends the given request to all active user sessions or a subset of them
// defined by optional client.uids field. Please be careful when using this
// method, because it is not aware of the physical user who triggered the request
// and blindly forwards it to all logged in users. Some of them might not have
// the right to see the request (let alone to respond to it).
func (client *Client) doMany(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body []byte) ([]*response, error) {
	sockets, err := filepath.Glob(filepath.Join(dirs.XdgRuntimeDirGlob, "snapd-session-agent.socket"))
	if err != nil {
		return nil, err
	}
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		responses []*response
	)

	var uids map[string]bool
	if len(client.uids) > 0 {
		uids = make(map[string]bool)
		for _, uid := range client.uids {
			uids[fmt.Sprintf("%d", uid)] = true
		}
	}

	for _, socket := range sockets {
		// filter out sockets based on uids
		if len(uids) > 0 {
			// XXX: alternatively we could Stat() the socket and
			// and check Uid field of stat.Sys().(*syscall.Stat_t), but it's
			// more annyoing to unit-test.
			userPart := filepath.Base(filepath.Dir(socket))
			if !uids[userPart] {
				continue
			}
		}
		wg.Add(1)
		go func(socket string) {
			defer wg.Done()
			response := client.sendRequest(ctx, socket, method, urlpath, query, headers, body)
			if response != nil {
				mu.Lock()
				defer mu.Unlock()
				responses = append(responses, response)
			}
		}(socket)
	}
	wg.Wait()
	return responses, nil
}

func decodeInto(reader io.Reader, v interface{}) error {
	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		r := dec.Buffered()
		buf, err1 := ioutil.ReadAll(r)
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

type ServiceFailure struct {
	Uid     int
	Service string
	Error   string
}

func decodeServiceErrors(uid int, errorValue map[string]interface{}, kind string) ([]ServiceFailure, error) {
	if errorValue[kind] == nil {
		return nil, nil
	}
	errors, ok := errorValue[kind].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("cannot decode %s failures: expected a map, got %T", kind, errorValue[kind])
	}
	var failures []ServiceFailure
	var err error
	for service, reason := range errors {
		if reasonString, ok := reason.(string); ok {
			failures = append(failures, ServiceFailure{
				Uid:     uid,
				Service: service,
				Error:   reasonString,
			})
		} else if err == nil {
			err = fmt.Errorf("cannot decode %s failure for %q: expected string, but got %T", kind, service, reason)
		}
	}
	return failures, err
}

func (client *Client) serviceControlCall(ctx context.Context, action string, services []string) (startFailures, stopFailures []ServiceFailure, err error) {
	headers := map[string]string{"Content-Type": "application/json"}
	reqBody, err := json.Marshal(map[string]interface{}{
		"action":   action,
		"services": services,
	})
	if err != nil {
		return nil, nil, err
	}
	responses, err := client.doMany(ctx, "POST", "/v1/service-control", nil, headers, reqBody)
	if err != nil {
		return nil, nil, err
	}
	for _, resp := range responses {
		if agentErr, ok := resp.err.(*Error); ok && agentErr.Kind == "service-control" {
			if errorValue, ok := agentErr.Value.(map[string]interface{}); ok {
				if failures, err := decodeServiceErrors(resp.uid, errorValue, "restart-errors"); err == nil && len(failures) > 0 {
					startFailures = append(startFailures, failures...)
				} else {
					failures, _ := decodeServiceErrors(resp.uid, errorValue, "start-errors")
					startFailures = append(startFailures, failures...)
					failures, _ = decodeServiceErrors(resp.uid, errorValue, "stop-errors")
					stopFailures = append(stopFailures, failures...)
				}
			}
		}
		if resp.err != nil && err == nil {
			err = resp.err
		}
	}
	return startFailures, stopFailures, err
}

func (client *Client) ServicesDaemonReload(ctx context.Context) error {
	_, _, err := client.serviceControlCall(ctx, "daemon-reload", nil)
	return err
}

func (client *Client) ServicesStart(ctx context.Context, services []string) (startFailures, stopFailures []ServiceFailure, err error) {
	return client.serviceControlCall(ctx, "start", services)
}

func (client *Client) ServicesStop(ctx context.Context, services []string) (stopFailures []ServiceFailure, err error) {
	_, stopFailures, err = client.serviceControlCall(ctx, "stop", services)
	return stopFailures, err
}

func (client *Client) ServicesRestart(ctx context.Context, services []string) (restartFailures []ServiceFailure, err error) {
	restartFailures, _, err = client.serviceControlCall(ctx, "restart", services)
	return restartFailures, err
}

func (client *Client) ServicesReloadOrRestart(ctx context.Context, services []string) (restartFailures []ServiceFailure, err error) {
	restartFailures, _, err = client.serviceControlCall(ctx, "reload-or-restart", services)
	return restartFailures, err
}

// ServiceUnitStatus is a JSON encoding representing systemd.UnitStatus for a user service.
type ServiceUnitStatus struct {
	Daemon           string   `json:"daemon"`
	Id               string   `json:"id"`
	Name             string   `json:"name"`
	Names            []string `json:"names"`
	Enabled          bool     `json:"enabled"`
	Active           bool     `json:"active"`
	Installed        bool     `json:"installed"`
	NeedDaemonReload bool     `json:"needs-reload"`
}

func (client *Client) ServiceStatus(ctx context.Context, services []string) (map[int][]ServiceUnitStatus, map[int][]ServiceFailure, error) {
	q := make(url.Values)
	q.Add("services", strings.Join(services, ","))

	responses, err := client.doMany(ctx, "GET", "/v1/service-status", q, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	var respErr error
	stss := make(map[int][]ServiceUnitStatus)
	failures := make(map[int][]ServiceFailure)
	for _, resp := range responses {
		// Parse status errors which were a result of failure to retrieve status of services
		if agentErr, ok := resp.err.(*Error); ok && agentErr.Kind == "service-status" {
			if errorValue, ok := agentErr.Value.(map[string]interface{}); ok {
				if fs, err := decodeServiceErrors(resp.uid, errorValue, "status-errors"); err == nil && len(fs) > 0 {
					failures[resp.uid] = append(failures[resp.uid], fs...)
				}
			}
			continue
		}

		// The response was an error, store the first error
		if resp.err != nil && respErr == nil {
			respErr = resp.err
			continue
		}

		var si []ServiceUnitStatus
		if err := json.Unmarshal(resp.Result, &si); err != nil && respErr == nil {
			respErr = err
			continue
		}
		stss[resp.uid] = si
	}
	return stss, failures, respErr
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
