// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"errors"
	"time"

	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

// assembleClusterSetup contains the configuration for creating a cluster.
type assembleClusterSetup struct {
	// Secret is the shared secret used for assembly authentication.
	Secret string `json:"secret"`
	// RDT is the random device token for this device.
	RDT string `json:"rdt"`
	// IP is the IP address this device should bind to during assembly.
	IP string `json:"ip"`
	// Port is the port this device should bind to during assembly.
	Port int `json:"port"`
	// ExpectedSize is the expected number of devices in the cluster. If set to
	// 0, assembly will run indefinitely until cancelled.
	ExpectedSize int `json:"expected-size,omitempty"`
	// Period is the duration of time between route publications.
	Period time.Duration `json:"period,omitempty"`
	// TLSCert is this device's TLS certificate in PEM format.
	TLSCert []byte `json:"tls-cert"`
	// TLSKey is this device's TLS private key in PEM format.
	TLSKey []byte `json:"tls-key"`
}

func (m *ClusterManager) doAssembleCluster(*state.Task, *tomb.Tomb) error {
	return errors.New("unimplemented")
}
