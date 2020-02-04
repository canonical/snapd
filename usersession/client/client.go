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
	"sync"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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
}

func New() *Client {
	transport := &http.Transport{Dial: dialSessionAgent, DisableKeepAlives: true}
	return &Client{
		doer: &http.Client{Transport: transport},
	}
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
	for _, socket := range sockets {
		wg.Add(1)
		go func(socket string) {
			defer wg.Done()
			uidStr := filepath.Base(filepath.Dir(socket))
			uid, err := strconv.Atoi(uidStr)
			if err != nil {
				logger.Noticef("Socket %q does not appear to be in a valid XDG_RUNTIME_DIR", socket)
				return
			}
			u := url.URL{
				Scheme:   "http",
				Host:     uidStr,
				Path:     urlpath,
				RawQuery: query.Encode(),
			}
			req, err := http.NewRequest(method, u.String(), bytes.NewBuffer(body))
			if err != nil {
				logger.Noticef("Failed to create HTTP request: %v", err)
				return
			}
			req = req.WithContext(ctx)
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			httpResp, err := client.doer.Do(req)
			response := response{
				uid: uid,
				err: err,
			}
			if err == nil {
				defer httpResp.Body.Close()
				response.statusCode = httpResp.StatusCode
				response.err = decodeInto(httpResp.Body, &response)
				response.checkError()
			}
			mu.Lock()
			defer mu.Unlock()
			responses = append(responses, &response)
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

func decodeServiceErrors(uid int, errorValue map[string]interface{}, kind string) []ServiceFailure {
	errors, ok := errorValue[kind].(map[string]interface{})
	if !ok {
		return nil
	}
	var failures []ServiceFailure
	for service, reason := range errors {
		if reasonString, ok := reason.(string); ok {
			failures = append(failures, ServiceFailure{
				Uid:     uid,
				Service: service,
				Error:   reasonString,
			})
		} else {
			logger.Noticef("Could not decode %s failure for %q: expected string, but got %T", kind, service, reason)
		}
	}
	return failures
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
		if resp.err == nil {
			continue
		}
		if agentErr, ok := resp.err.(*Error); ok && agentErr.Kind == "service-control" {
			if errorValue, ok := agentErr.Value.(map[string]interface{}); ok {
				startFailures = append(startFailures, decodeServiceErrors(resp.uid, errorValue, "start-errors")...)
				stopFailures = append(stopFailures, decodeServiceErrors(resp.uid, errorValue, "stop-errors")...)
				continue
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
