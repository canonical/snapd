// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
)

// Service represents a service belonging to a Snap
type Service struct {
	Op     string        `json:"op"`
	Spec   ServiceSpec   `json:"spec"`
	Status ServiceStatus `json:"status"`
}

// ServiceSpec represents the Service specification
type ServiceSpec struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Start       string       `json:"start,omitempty"`
	StopTimeout string       `json:"stop-timeout,omitempty"`
	Ports       ServicePorts `json:"ports,omitempty"`
}

// ServicePorts are the collection of internal and external ports the Service
// may listen on
type ServicePorts struct {
	Internal map[string]ServicePort `json:"internal,omitempty"`
	External map[string]ServicePort `json:"external,omitempty"`
}

// ServicePort is a port a Service may listen on
type ServicePort struct {
	Port       string `json:",omitempty"`
	Negotiable bool   `json:",omitempty"`
}

// ServiceStatus represents the status of a Service
type ServiceStatus struct {
	ServiceFileName string `json:"service_file_name"`
	LoadState       string `json:"load_state"`
	ActiveState     string `json:"active_state"`
	SubState        string `json:"sub_state"`
	UnitFileState   string `json:"unit_file_state"`
	SnapName        string `json:"snap_name"`
	AppName         string `json:"service_name"`
}

// Services returns the list of services belonging to an *active* Snap
func (client *Client) Services(pkg string) (map[string]*Service, error) {
	var services map[string]*Service

	path := fmt.Sprintf("/v2/snaps/%s/services", pkg)
	if err := client.doSync("GET", path, nil, nil, &services); err != nil {
		return nil, fmt.Errorf("cannot list services: %s", err)
	}

	return services, nil
}
