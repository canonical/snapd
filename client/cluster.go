// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
)

// ClusterAssembleOptions holds the options for cluster assembly
type ClusterAssembleOptions struct {
	// Secret is the shared secret used for cluster assembly authentication
	Secret string
	// Address is the IP:port address this device should bind to for cluster assembly
	Address string
	// ExpectedSize is the expected number of devices in the cluster.
	// If set to 0, cluster assembly will run indefinitely until cancelled.
	ExpectedSize int
	// Domain is the mDNS domain for device discovery. Defaults to "local" if empty.
	Domain string
}

// ClusterAssemble initiates cluster assembly with the given options
func (client *Client) ClusterAssemble(opts ClusterAssembleOptions) (changeID string, err error) {
	req := struct {
		Action       string `json:"action"`
		Secret       string `json:"secret"`
		Address      string `json:"address"`
		ExpectedSize int    `json:"expected-size,omitempty"`
		Domain       string `json:"domain,omitempty"`
	}{
		Action:       "assemble",
		Secret:       opts.Secret,
		Address:      opts.Address,
		ExpectedSize: opts.ExpectedSize,
		Domain:       opts.Domain,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&req); err != nil {
		return "", err
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/cluster", nil, headers, &body)
}

// GetClusterUncommittedHeaders retrieves the uncommitted cluster state headers
// that are ready to be signed into a cluster assertion.
func (client *Client) GetClusterUncommittedHeaders() (map[string]any, error) {
	var headers map[string]any
	_, err := client.doSync("GET", "/v2/cluster/uncommitted", nil, nil, nil, &headers)
	if err != nil {
		return nil, err
	}
	return headers, nil
}

func (client *Client) CommitClusterAssertion(clusterID string) error {
	req := struct {
		ClusterID string `json:"cluster-id"`
	}{
		ClusterID: clusterID,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&req); err != nil {
		return err
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	_, err := client.doSync("POST", "/v2/cluster/uncommitted", nil, headers, &body, nil)
	return err
}
