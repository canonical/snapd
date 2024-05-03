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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
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

func (client *Client) sendRequest(ctx context.Context, uid int, method, urlpath string, query url.Values, headers map[string]string, body []byte) *response {
	response := &response{uid: uid}

	u := url.URL{
		Scheme:   "http",
		Host:     fmt.Sprintf("%d", uid),
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

func (client *Client) uidIsValidAsTarget(uid int) bool {
	// if uids are provided (i.e there must be entries, otherwise
	// no list is there), then there must be an entry
	if len(client.uids) > 0 {
		return client.uids[uid]
	}
	return true
}

func (client *Client) sessionTargets() ([]int, error) {
	sockets, err := filepath.Glob(filepath.Join(dirs.XdgRuntimeDirGlob, "snapd-session-agent.socket"))
	if err != nil {
		return nil, err
	}

	uids := make([]int, 0, len(client.uids))
	for _, sock := range sockets {
		uidStr := filepath.Base(filepath.Dir(sock))
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			// Ignore directories that do not
			// appear to be valid XDG runtime dirs
			// (i.e. /run/user/NNNN).
			continue
		}
		if client.uidIsValidAsTarget(uid) {
			uids = append(uids, uid)
		}
	}
	return uids, nil
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

func decodeInto(reader io.Reader, v interface{}) error {
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

// ServiceInstruction is the json representation of possible arguments
// for the user session rest api to control services. Arguments allowed for
// start/stop/restart are all listed here, and closely reflect possible arguments
// for similar options in the wrappers package.
type ServiceInstruction struct {
	Action   string   `json:"action"`
	Services []string `json:"services,omitempty"`

	// StartServices options
	Enable bool `json:"enable,omitempty"`

	// StopServices options
	Disable bool `json:"disable,omitempty"`

	// RestartServices options
	Reload bool `json:"reload,omitempty"`
}

func (client *Client) decodeControlResponses(responses []*response) (startFailures, stopFailures []ServiceFailure, err error) {
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

func (client *Client) serviceControlCall(ctx context.Context, inst *ServiceInstruction) (startFailures, stopFailures []ServiceFailure, err error) {
	headers := map[string]string{"Content-Type": "application/json"}
	reqBody, err := json.Marshal(inst)
	if err != nil {
		return nil, nil, err
	}
	responses, err := client.doMany(ctx, "POST", "/v1/service-control", nil, headers, reqBody)
	if err != nil {
		return nil, nil, err
	}
	return client.decodeControlResponses(responses)
}

func (client *Client) ServicesDaemonReload(ctx context.Context) error {
	_, _, err := client.serviceControlCall(ctx, &ServiceInstruction{
		Action: "daemon-reload",
	})
	return err
}

func filterDisabledServices(all, disabled []string) []string {
	var filtered []string
ServiceLoop:
	for _, svc := range all {
		for _, disabledSvc := range disabled {
			if strings.Contains(svc, disabledSvc) {
				continue ServiceLoop
			}
		}
		filtered = append(filtered, svc)
	}
	return filtered
}

type ClientServicesStartOptions struct {
	// Enable determines whether the service should be enabled before
	// its being started.
	Enable bool
	// DisabledServices is a list of services per-uid that can be provided
	// which will then be ignored for the start or enable operation.
	DisabledServices map[int][]string
}

// ServicesStop attempts to start the services in `services`.
// If the enable flag is provided, then services listed will also be
// enabled.
// If the map of disabled services is set, then on a per-uid basis the services
// listed in `services` can be filtered out.
func (client *Client) ServicesStart(ctx context.Context, services []string, opts ClientServicesStartOptions) (startFailures, stopFailures []ServiceFailure, err error) {
	// If no disabled services lists are provided, then we do not need to filter out services
	// per-user. In this case lets
	if len(opts.DisabledServices) == 0 {
		return client.serviceControlCall(ctx, &ServiceInstruction{
			Action:   "start",
			Services: services,
			Enable:   opts.Enable,
		})
	}

	// Otherwise we do a bit of manual request building based on the uids we need to filter
	// services out for.
	uids, err := client.sessionTargets()
	if err != nil {
		return nil, nil, err
	}
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		responses []*response
	)

	for _, uid := range uids {
		headers := map[string]string{"Content-Type": "application/json"}
		filtered := filterDisabledServices(services, opts.DisabledServices[uid])
		if len(filtered) == 0 {
			// Save an expensive call
			continue
		}
		reqBody, err := json.Marshal(&ServiceInstruction{
			Action:   "start",
			Services: filtered,
			Enable:   opts.Enable,
		})
		if err != nil {
			return nil, nil, err
		}
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			response := client.sendRequest(ctx, uid, "POST", "/v1/service-control", nil, headers, reqBody)
			if response != nil {
				mu.Lock()
				defer mu.Unlock()
				responses = append(responses, response)
			}
		}(uid)
	}
	wg.Wait()
	return client.decodeControlResponses(responses)
}

// ServicesStop attempts to stop the services in `services`.
// If the disable flag is set then the services listed also will
// be disabled.
func (client *Client) ServicesStop(ctx context.Context, services []string, disable bool) (stopFailures []ServiceFailure, err error) {
	_, stopFailures, err = client.serviceControlCall(ctx, &ServiceInstruction{
		Action:   "stop",
		Services: services,
		Disable:  disable,
	})
	return stopFailures, err
}

// ServicesRestart attempts to restart or reload active services in `services`.
// If the reload flag is set then "systemctl reload-or-restart" is attempted.
func (client *Client) ServicesRestart(ctx context.Context, services []string, reload bool) (restartFailures []ServiceFailure, err error) {
	restartFailures, _, err = client.serviceControlCall(ctx, &ServiceInstruction{
		Action:   "restart",
		Services: services,
		Reload:   reload,
	})
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

func (us *ServiceUnitStatus) SystemdUnitStatus() *systemd.UnitStatus {
	return &systemd.UnitStatus{
		Daemon:           us.Daemon,
		Id:               us.Id,
		Name:             us.Name,
		Names:            us.Names,
		Enabled:          us.Enabled,
		Active:           us.Active,
		Installed:        us.Installed,
		NeedDaemonReload: us.NeedDaemonReload,
	}
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
