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
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
)

// taskCreateClusterSetup extracts the create-cluster-setup from the task.
func taskCreateClusterSetup(t *state.Task) (*createClusterSetup, error) {
	var setup createClusterSetup
	if err := t.Get("create-cluster-setup", &setup); err != nil {
		return nil, fmt.Errorf("internal error: cannot get create-cluster-setup from task: %v", err)
	}
	return &setup, nil
}

// doCreateCluster handles the "create-cluster" task by using assemblestate
// to perform the actual cluster assembly process.
func (m *ClusterManager) doCreateCluster(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// get configuration from task
	setup, err := taskCreateClusterSetup(t)
	if err != nil {
		return err
	}

	// parse ip address
	ip := net.ParseIP(setup.IP)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", setup.IP)
	}

	// create tls certificate
	_, err = tls.X509KeyPair(setup.TLSCert, setup.TLSKey)
	if err != nil {
		return fmt.Errorf("cannot load TLS certificate: %v", err)
	}

	// get device manager for system credentials
	deviceMgr := devicestate.DeviceMgr(st)

	// get device serial assertion
	serial, err := deviceMgr.Serial()
	if err != nil {
		return fmt.Errorf("cannot get device serial: %v", err)
	}

	// get device private key
	privateKey, err := deviceMgr.DeviceKey()
	if err != nil {
		return fmt.Errorf("cannot get device private key: %v", err)
	}

	// create assemblestate configuration
	assembleConfig := assemblestate.AssembleConfig{
		Secret:       setup.Secret,
		RDT:          assemblestate.DeviceToken(setup.RDT),
		IP:           ip,
		Port:         setup.Port,
		TLSCert:      setup.TLSCert,
		TLSKey:       setup.TLSKey,
		ExpectedSize: setup.ExpectedSize,
		Serial:       serial,
		PrivateKey:   privateKey,
	}

	// get assertion database
	assertDB := assertstate.DB(st)

	// create initial session (empty for new cluster)
	session := assemblestate.AssembleSession{}

	// unlock state before long-running operations
	st.Unlock()

	// create assemblestate instance
	selector := func(self assemblestate.DeviceToken, identified func(assemblestate.DeviceToken) bool) (assemblestate.RouteSelector, error) {
		// use priority selector as default - pass nil for default random source
		return assemblestate.NewPrioritySelector(self, nil, identified), nil
	}

	// commit function to persist session state
	commit := func(session assemblestate.AssembleSession) {
		// TODO: persist session to task state for resumption
		logger.Debugf("cluster assembly session updated: %d trusted peers", len(session.Trusted))
	}

	as, err := assemblestate.NewAssembleState(
		assembleConfig,
		session,
		selector,
		logger.New(nil, 0, &logger.LoggerOptions{}),
		commit,
		assertDB,
	)
	if err != nil {
		st.Lock()
		return fmt.Errorf("cannot create assembly state: %v", err)
	}

	// create transport for communication
	transport := assemblestate.NewHTTPTransport(logger.New(nil, 0, &logger.LoggerOptions{}))

	// create discovery channel
	discoveries := make(chan []string, 1)

	// send initial discovery addresses if provided
	if len(setup.Addresses) > 0 {
		go func() {
			discoveries <- setup.Addresses
		}()
	}

	// create context that respects tomb cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// monitor tomb for cancellation
	go func() {
		select {
		case <-tomb.Dying():
			cancel()
		case <-ctx.Done():
		}
	}()

	// run cluster assembly
	opts := assemblestate.PublicationOptions{
		Period: 30 * time.Second,
		Jitter: 5 * time.Second,
	}

	routes, err := as.Run(ctx, transport, discoveries, opts)
	st.Lock()

	if err != nil {
		if ctx.Err() != nil {
			// cancellation is not an error for us
			return nil
		}
		return fmt.Errorf("cluster assembly failed: %v", err)
	}

	// store results in task state
	t.Set("cluster-routes", routes)
	t.Logf("Cluster assembly completed successfully with %d devices and %d routes",
		len(routes.Devices), len(routes.Routes)/3)

	return nil
}
