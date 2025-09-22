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
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
)

// taskAssembleClusterSetup extracts the assemble-cluster-setup from the task.
func taskAssembleClusterSetup(t *state.Task) (assembleClusterSetup, error) {
	var setup assembleClusterSetup
	if err := t.Get("assemble-cluster-setup", &setup); err != nil {
		return assembleClusterSetup{}, fmt.Errorf("internal error: cannot get assemble-cluster-setup from task: %v", err)
	}
	return setup, nil
}

// interfaceWithIP finds the network interface that has the given IP address
func interfaceWithIP(ip net.IP) (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ifaceIP net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ifaceIP = v.IP
			case *net.IPAddr:
				ifaceIP = v.IP
			}

			if ifaceIP != nil && ifaceIP.Equal(ip) {
				return iface.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no interface found with IP %s", ip)
}

// doAssembleCluster handles the "assemble-cluster" task by using assemblestate
// to perform the actual cluster assembly process.
func (m *ClusterManager) doAssembleCluster(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	setup, err := taskAssembleClusterSetup(t)
	if err != nil {
		return err
	}

	ip := net.ParseIP(setup.IP)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", setup.IP)
	}

	// TODO: get device serial/private key in some more reasonable way? could
	// maybe be passed into the task?

	ctx := tomb.Context(context.Background())

	if err := m.receiver.start(st, setup.IP); err != nil {
		return err
	}

	devices, routes, err := assemble(ctx, &m.receiver, t, setup)
	if err != nil {
		return err
	}

	clusterID, err := randutil.RandomKernelUUID()
	if err != nil {
		return fmt.Errorf("cannot generate cluster ID: %v", err)
	}

	devs := make([]ClusterDevice, 0, len(devices))
	ids := make([]int, 0, len(devices))
	for i, dev := range devices {
		devs = append(devs, ClusterDevice{
			ID:        i + 1,
			BrandID:   dev.BrandID,
			Model:     dev.Model,
			Serial:    dev.Serial,
			Addresses: dev.Addresses,
		})
		ids = append(ids, i+1)
	}

	// TODO: handle non-default clusters
	uncommitted := UncommittedClusterState{
		ClusterID: clusterID,
		Devices:   devs,
		Subclusters: []ClusterSubcluster{
			{
				Name:    "default",
				Devices: ids,
				Snaps:   []ClusterSnap{},
			},
		},
		CompletedAt: time.Now(),
		Sequence:    0,
	}

	st.Set("uncommitted-cluster-state", uncommitted)

	t.Logf(
		"Cluster assembled with %d devices and %d routes",
		len(devices),
		len(routes.Routes)/3,
	)

	t.SetStatus(state.DoneStatus)

	return nil
}

func assemble(
	ctx context.Context,
	ar *AssertionReceiver,
	t *state.Task,
	setup assembleClusterSetup,
) ([]assemblestate.ClusterDevice, assemblestate.Routes, error) {
	st := t.State()

	deviceMgr := devicestate.DeviceMgr(st)

	serial, err := deviceMgr.Serial()
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cannot get device serial: %v", err)
	}

	signer := func(data []byte) ([]byte, error) {
		st.Lock()
		defer st.Unlock()
		return deviceMgr.SignWithDeviceKey(data)
	}

	assertDB := assertstate.DB(st)

	// TODO: create initial session, eventually attempt to detect a resumed
	// session
	session := assemblestate.AssembleSession{}

	// unlock state before long-running operations
	st.Unlock()
	defer st.Lock()

	config := assemblestate.AssembleConfig{
		Secret:       setup.Secret,
		RDT:          assemblestate.DeviceToken(setup.RDT),
		TLSCert:      setup.TLSCert,
		TLSKey:       setup.TLSKey,
		ExpectedSize: setup.ExpectedSize,
		Serial:       serial,
		Signer:       signer,
	}

	meter := snapstate.NewTaskProgressAdapterUnlocked(t)
	selector := func(
		self assemblestate.DeviceToken,
		identified func(assemblestate.DeviceToken) bool,
	) (assemblestate.RouteSelector, error) {
		return assemblestate.NewPrioritySelector(self, nil, identified), nil
	}

	// commit function to persist session state and report progress
	prev := assemblestate.AssembleSession{}
	message := func(devs, routes int) string {
		var b strings.Builder
		b.WriteString("Assembling cluster: discovered %d ")
		if devs != 1 {
			b.WriteString("devices and %d ")
		} else {
			b.WriteString("device and %d ")
		}
		if routes != 1 {
			b.WriteString("routes")
		} else {
			b.WriteString("route")
		}
		return fmt.Sprintf(b.String(), devs, routes)
	}

	commit := func(session assemblestate.AssembleSession) {
		// persist session to task state for resumption
		st.Lock()
		t.Set("assemble-session", session)
		st.Unlock()

		if len(prev.Devices.IDs) != len(session.Devices.IDs) || len(prev.Routes.Routes) != len(session.Routes.Routes) {
			meter.Notify(message(len(session.Devices.IDs), len(session.Routes.Routes)/3))
		}
		prev = session
	}

	as, err := assemblestate.NewAssembleState(
		config,
		session,
		selector,
		commit,
		assertDB,
	)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cannot create assembly state: %v", err)
	}

	transport := assemblestate.NewHTTPSTransport()

	ip := net.ParseIP(setup.IP)
	if ip == nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("invalid ip: %s", setup.IP)
	}

	iface, err := interfaceWithIP(ip)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cannot find network interface for IP %s: %v", setup.IP, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// when cancel is called by our server, then all the other handlers will
	// exit
	ar.set(cancel)

	discoveries, stop, err := assemblestate.MulticastDiscovery(
		ctx,
		iface,
		ip.String(),
		setup.Port,
		assemblestate.DeviceToken(config.RDT),
	)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cannot start multicast discovery: %v", err)
	}
	defer stop()

	period := setup.Period
	if period == 0 {
		period = 5 * time.Second
	}
	opts := assemblestate.RunOptions{
		Period: period,
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(
		ip.String(), strconv.Itoa(setup.Port),
	))
	if err != nil {
		return nil, assemblestate.Routes{}, err
	}
	defer ln.Close()

	start := time.Now()
	devices, routes, err := as.Run(ctx, ln, transport, discoveries, opts)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cluster assembly failed: %v", err)
	}

	stats := transport.Stats()
	meter.Notify(fmt.Sprintf(
		"Cluster assembled in %s: sent %d messages (%d bytes), received %d messages (%d bytes)",
		time.Since(start).Truncate(time.Second), stats.Sent, stats.Tx, stats.Received, stats.Rx,
	))

	return devices, routes, nil
}
