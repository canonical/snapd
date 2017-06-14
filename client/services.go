// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"net/url"
	"strings"

	"github.com/snapcore/snapd/systemd"
)

// ServicesOp encapsulate requests for performing an operation on a series of services
type ServiceOp struct {
	Services []string `json:"services,omitempty"`
	Action   string   `json:"action"`
}

// A Service is a description of a service's status in the system
type Service struct {
	Snap    string `json:"snap"`
	AppInfo        // note this is much less than snap.AppInfo, right now
	*systemd.ServiceStatus
	Logs []systemd.Log `json:"logs,omitempty"`
}

// helper for ServiceStatus and ServiceLogs
func (client *Client) serviceStatusAndLogs(serviceNames []string, logs bool) ([]Service, error) {
	query := url.Values{}
	query.Set("services", strings.Join(serviceNames, ","))
	if logs {
		query.Set("logs", "true")
	}
	var statuses []Service
	_, err := client.doSync("GET", "/v2/services", query, nil, nil, &statuses)
	if err != nil {
		return nil, err
	}
	return statuses, nil
}

// ServiceStatus asks for the status of a series of services, by name.
func (client *Client) ServiceStatus(serviceNames []string) ([]Service, error) {
	return client.serviceStatusAndLogs(serviceNames, false)
}

// ServiceLogs asks for the status and logs of a series of services, by name.
func (client *Client) ServiceLogs(serviceNames []string) ([]Service, error) {
	return client.serviceStatusAndLogs(serviceNames, true)
}

// ServiceOp asks to perform an operation on a series of services, by name.
func (client *Client) ServiceOp(action string, services []string) (changeID string, err error) {
	buf, err := json.Marshal(&ServiceOp{Action: action, Services: services})
	if err != nil {
		return "", err
	}
	return client.doAsync("POST", "/v2/services", nil, nil, bytes.NewReader(buf))
}
