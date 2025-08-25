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
	"errors"
	"time"
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
	// Period is the route publication period duration.
	// Defaults to 5 seconds if zero value.
	Period time.Duration
}

// ClusterAssemble initiates cluster assembly with the given options
func (client *Client) ClusterAssemble(opts ClusterAssembleOptions) (changeID string, err error) {
	req := struct {
		Action       string        `json:"action"`
		Secret       string        `json:"secret"`
		Address      string        `json:"address"`
		ExpectedSize int           `json:"expected-size,omitempty"`
		Domain       string        `json:"domain,omitempty"`
		Period       time.Duration `json:"period,omitempty"`
	}{
		Action:       "assemble",
		Secret:       opts.Secret,
		Address:      opts.Address,
		ExpectedSize: opts.ExpectedSize,
		Domain:       opts.Domain,
		Period:       opts.Period,
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

// ClusterDevice represents a device in the cluster
type ClusterDevice struct {
	ID        int      `json:"id"`
	BrandID   string   `json:"brand-id"`
	Model     string   `json:"model"`
	Serial    string   `json:"serial"`
	Addresses []string `json:"addresses"`
}

// ClusterSnap represents a snap in a subcluster
type ClusterSnap struct {
	State    string `json:"state"`
	Instance string `json:"instance"`
	Channel  string `json:"channel"`
}

// ClusterSubcluster represents a logical grouping of devices
type ClusterSubcluster struct {
	Name    string        `json:"name"`
	Devices []int         `json:"devices"`
	Snaps   []ClusterSnap `json:"snaps"`
}

// UncommittedClusterState holds the cluster configuration after assembly
// but before it has been signed and committed as an assertion.
type UncommittedClusterState struct {
	// ClusterID is the unique identifier for this cluster
	ClusterID string `json:"cluster-id"`
	// Devices is the list of devices that are part of the cluster
	Devices []ClusterDevice `json:"devices"`
	// Subclusters defines the logical groupings of devices
	Subclusters []ClusterSubcluster `json:"subclusters"`
	// CompletedAt records when the assembly process completed
	CompletedAt time.Time `json:"completed-at"`
}

// GetClusterUncommittedState retrieves the uncommitted cluster state.
func (client *Client) GetClusterUncommittedState() (UncommittedClusterState, error) {
	var state UncommittedClusterState
	_, err := client.doSync("GET", "/v2/cluster/uncommitted", nil, nil, nil, &state)
	if err != nil {
		return UncommittedClusterState{}, err
	}
	return state, nil
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

	_, err := client.doSync("POST", "/v2/cluster/commit", nil, headers, &body, nil)
	return err
}

// ClusterInstall adds a snap to the uncommitted cluster state.
// TODO: support non-default subclusters
func (client *Client) ClusterInstall(snapName string) error {
	// first, get the current uncommitted state
	state, err := client.GetClusterUncommittedState()
	if err != nil {
		return err
	}

	var sc *ClusterSubcluster
	for i := range state.Subclusters {
		if state.Subclusters[i].Name == "default" {
			sc = &state.Subclusters[i]
			break
		}
	}

	if sc == nil {
		return errors.New("missing default subcluster")
	}

	// check if snap already exists in the default subcluster
	for _, snap := range sc.Snaps {
		if snap.Instance == snapName {
			return nil // snap already in cluster state
		}
	}

	sc.Snaps = append(sc.Snaps, ClusterSnap{
		State:    "clustered",
		Instance: snapName,
		Channel:  "stable",
	})

	// send the updated state back
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&state); err != nil {
		return err
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	_, err = client.doSync("POST", "/v2/cluster/uncommitted", nil, headers, &body, nil)
	return err
}
