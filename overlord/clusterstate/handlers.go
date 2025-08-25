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
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
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

	deviceMgr := devicestate.DeviceMgr(st)

	serial, err := deviceMgr.Serial()
	if err != nil {
		return fmt.Errorf("cannot get device serial: %v", err)
	}

	key, err := deviceMgr.DeviceKey()
	if err != nil {
		return fmt.Errorf("cannot get device private key: %v", err)
	}

	config := assemblestate.AssembleConfig{
		Secret:       setup.Secret,
		RDT:          assemblestate.DeviceToken(setup.RDT),
		IP:           ip,
		Port:         setup.Port,
		TLSCert:      setup.TLSCert,
		TLSKey:       setup.TLSKey,
		ExpectedSize: setup.ExpectedSize,
		Serial:       serial,
		PrivateKey:   key,
	}

	ctx := tomb.Context(context.Background())
	devices, routes, err := assemble(ctx, t, setup.Domain, config, setup.Period)
	if err != nil {
		return err
	}

	clusterID, err := randutil.RandomKernelUUID()
	if err != nil {
		return fmt.Errorf("cannot generate cluster ID: %v", err)
	}

	devs := make([]asserts.ClusterDevice, 0, len(devices))
	ids := make([]int, 0, len(devices))
	for i, dev := range devices {
		devs = append(devs, asserts.ClusterDevice{
			ID:        i + 1,
			BrandID:   dev.BrandID,
			Model:     dev.Model,
			Serial:    dev.Serial,
			Addresses: dev.Addresses,
		})
		ids = append(ids, i+1)
	}

	uncommitted := UncommittedClusterState{
		ClusterID: clusterID,
		Devices:   devs,
		Subclusters: []asserts.ClusterSubcluster{
			// TODO: handle non-default clusters
			{
				Name:    "default",
				Devices: ids,
				Snaps:   []asserts.ClusterSnap{},
			},
		},
		CompletedAt: time.Now(),
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
	t *state.Task,
	domain string,
	config assemblestate.AssembleConfig,
	period time.Duration,
) ([]assemblestate.ClusterDevice, assemblestate.Routes, error) {
	st := t.State()
	assertDB := assertstate.DB(st)

	// TODO: create initial session, eventually attempt to detect a resumed
	// session
	session := assemblestate.AssembleSession{}

	// unlock state before long-running operations
	st.Unlock()
	defer st.Lock()

	meter := progress.Meter(progress.Null)
	if config.ExpectedSize > 0 {
		meter = snapstate.NewTaskProgressAdapterUnlocked(t)
	}

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

	iface, err := interfaceWithIP(config.IP)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cannot find network interface for IP %s: %v", config.IP, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// start up our "distributed database". this server is scoped to this task
	stopped := make(chan struct{})
	defer func() { <-stopped }()
	go func() {
		srv := &http.Server{
			Addr: fmt.Sprintf("%s:%d", config.IP, 7070),
		}
		srv.Handler = receiver(st, srv)

		// if the whole context gets cancelled, then shut this thing down. that
		// will allow the task handler to return, regardless of if we actually
		// received the assertion or not.
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()

		_ = srv.ListenAndServe()

		// if the server shutdown, that means that either we got in an assertion
		// or something is cancelling our task (meaning we are going to send our
		// assertion).
		cancel()

		defer close(stopped)
	}()

	discoveries, stop, err := assemblestate.MulticastDiscovery(
		ctx,
		iface,
		config.IP,
		config.Port,
		assemblestate.DeviceToken(config.RDT),
		domain,
		false,
	)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cannot start multicast discovery: %v", err)
	}
	defer stop()

	if period == 0 {
		period = 5 * time.Second
	}
	opts := assemblestate.PublicationOptions{
		Period: period,
	}

	start := time.Now()
	devices, routes, err := as.Run(ctx, transport, discoveries, opts)
	if err != nil {
		return nil, assemblestate.Routes{}, fmt.Errorf("cluster assembly failed: %v", err)
	}

	stats := transport.Stats()
	meter.Notify(fmt.Sprintf(
		"Cluster assembled in %s: sent %d messages (%d bytes), received %d messages (%d bytes)",
		time.Since(start).Truncate(time.Second), stats.Sent, stats.Tx, stats.Received, stats.Rx,
	))

	// once the call to Run has returned, we should shutdown the http server.
	cancel()

	return devices, routes, nil
}

func receiver(st *state.State, srv *http.Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		batch := asserts.NewBatch(nil)
		refs, err := batch.AddStream(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// validate that we only have cluster and account-key assertions
		var cluster *asserts.Ref
		for _, r := range refs {
			switch r.Type.Name {
			case asserts.ClusterType.Name:
				cluster = r
			case asserts.AccountKeyType.Name:
			default:
				// reject any other assertion types
				http.Error(w, fmt.Sprintf("unexpected assertion type %q in bundle, only cluster and account-key assertions are allowed", r.Type.Name), 400)
				return
			}
		}

		if cluster == nil {
			http.Error(w, "missing cluster assertion in bundle!", 400)
			return
		}

		st.Lock()
		defer st.Unlock()

		db := assertstate.DB(st)
		if err := assertstate.AddBatch(st, batch, &asserts.CommitOptions{
			Precheck: true,
		}); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// save our assertion to our "distributed database"
		if err := os.MkdirAll("/tmp/snapd-clusterdb", 0755); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		f, err := os.Create("/tmp/snapd-clusterdb/cluster.assert")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer f.Close()

		buf := bytes.NewBuffer(nil)
		enc := asserts.NewEncoder(buf)
		for _, r := range refs {
			a, err := r.Resolve(db.Find)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}

			if err := enc.Encode(a); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}

		if err := os.WriteFile("/tmp/snapd-clusterdb/cluster.assert", buf.Bytes(), 0644); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// since we got the assertion, we know that we don't need this state any
		// more
		st.Set("uncommitted-cluster-state", nil)

		st.EnsureBefore(0)

		// wait for the response to be done
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// shutdown the server
		go srv.Shutdown(context.Background())
	})
}
