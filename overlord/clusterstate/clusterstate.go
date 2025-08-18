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

package clusterstate

import (
	"fmt"
	"net"

	"github.com/snapcore/snapd/overlord/state"
)

// createClusterSetup contains the configuration for creating a cluster.
// This struct is stored in the task state for the "create-cluster" task.
type createClusterSetup struct {
	// Secret is the shared secret used for cluster assembly authentication
	Secret string `json:"secret"`

	// RDT is the random device token for this device
	RDT string `json:"rdt"`

	// IP is the IP address this device should bind to for cluster assembly
	IP string `json:"ip"`

	// Port is the port this device should bind to for cluster assembly
	Port int `json:"port"`

	// ExpectedSize is the expected number of devices in the cluster.
	// If set to 0, cluster assembly will run indefinitely until cancelled.
	ExpectedSize int `json:"expected-size,omitempty"`

	// TLSCert is the TLS certificate in PEM format for secure communication
	TLSCert []byte `json:"tls-cert"`

	// TLSKey is the TLS private key in PEM format for secure communication
	TLSKey []byte `json:"tls-key"`

	// Addresses is a list of discovery addresses to connect to other nodes
	Addresses []string `json:"addresses,omitempty"`
}

// CreateClusterConfig contains the configuration for creating a new cluster.
type CreateClusterConfig struct {
	// Secret is the shared secret used for cluster assembly authentication
	Secret string

	// RDT is the random device token for this device
	RDT string

	// IP is the IP address this device should bind to for cluster assembly
	IP net.IP

	// Port is the port this device should bind to for cluster assembly
	Port int

	// ExpectedSize is the expected number of devices in the cluster.
	// If set to 0, cluster assembly will run indefinitely until cancelled.
	ExpectedSize int

	// TLSCert is the TLS certificate in PEM format for secure communication
	TLSCert []byte

	// TLSKey is the TLS private key in PEM format for secure communication
	TLSKey []byte

	// Addresses is a list of discovery addresses to connect to other nodes
	Addresses []string
}

// CreateCluster creates a new task to assemble a cluster using the given configuration.
func CreateCluster(st *state.State, config *CreateClusterConfig) (*state.TaskSet, error) {
	if config == nil {
		return nil, fmt.Errorf("cluster configuration cannot be nil")
	}

	// validate required fields
	if config.Secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	if config.RDT == "" {
		return nil, fmt.Errorf("rdt is required")
	}
	if config.IP == nil {
		return nil, fmt.Errorf("ip is required")
	}
	if config.Port <= 0 {
		return nil, fmt.Errorf("port must be positive")
	}
	if len(config.TLSCert) == 0 {
		return nil, fmt.Errorf("tls certificate is required")
	}
	if len(config.TLSKey) == 0 {
		return nil, fmt.Errorf("tls private key is required")
	}

	// convert to internal setup struct
	setup := &createClusterSetup{
		Secret:       config.Secret,
		RDT:          config.RDT,
		IP:           config.IP.String(),
		Port:         config.Port,
		ExpectedSize: config.ExpectedSize,
		TLSCert:      config.TLSCert,
		TLSKey:       config.TLSKey,
		Addresses:    config.Addresses,
	}

	// create the task
	task := st.NewTask("create-cluster", "Create cluster assembly")
	task.Set("create-cluster-setup", setup)

	// create and return task set
	ts := state.NewTaskSet(task)
	return ts, nil
}
