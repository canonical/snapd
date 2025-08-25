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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
)

// assembleClusterSetup contains the configuration for creating a cluster.
// This struct is stored in the task state for the "assemble-cluster" task.
type assembleClusterSetup struct {
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
	// Domain is the mDNS domain for device discovery. Defaults to "local" if empty.
	Domain string `json:"domain,omitempty"`
	// Period is the route publication period duration.
	Period time.Duration `json:"period,omitempty"`
	// TLSCert is the TLS certificate in PEM format for secure communication
	TLSCert []byte `json:"tls-cert"`
	// TLSKey is the TLS private key in PEM format for secure communication
	TLSKey []byte `json:"tls-key"`
}

// AssembleConfig contains the configuration for creating a new cluster.
type AssembleConfig struct {
	// Secret is the shared secret used for cluster assembly authentication
	Secret string
	// Address is the address that we should listen for incoming cluster
	// assembly communication.
	Address string
	// ExpectedSize is the expected number of devices in the cluster. If set to
	// 0, cluster assembly will run indefinitely until cancelled.
	ExpectedSize int
	// Domain is the mDNS domain for device discovery. Defaults to "local" if empty.
	Domain string
	// Period is the route publication period duration.
	Period time.Duration
}

// UncommittedClusterState holds the cluster configuration after assembly
// but before it has been signed and committed as an assertion.
type UncommittedClusterState struct {
	// ClusterID is the unique identifier for this cluster
	ClusterID string `json:"cluster-id"`
	// Devices is the list of devices that are part of the cluster
	Devices []asserts.ClusterDevice `json:"devices"`
	// Subclusters defines the logical groupings of devices
	Subclusters []asserts.ClusterSubcluster `json:"subclusters"`
	// CompletedAt records when the assembly process completed
	CompletedAt time.Time `json:"completed-at"`
}

// Assemble creates a new task to assemble a cluster using the given configuration.
func Assemble(st *state.State, config AssembleConfig) (*state.TaskSet, error) {
	if config.Secret == "" {
		return nil, errors.New("secret is required")
	}

	if config.Address == "" {
		return nil, errors.New("address is required")
	}

	host, port, err := net.SplitHostPort(config.Address)
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, errors.New("invalid IP address in address")
	}

	certPEM, keyPEM, err := createCertAndKey(ip)
	if err != nil {
		return nil, err
	}

	rdt, err := randutil.RandomKernelUUID()
	if err != nil {
		return nil, err
	}

	p, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("invalid port in address: %w", err)
	}

	setup := &assembleClusterSetup{
		Secret:       config.Secret,
		RDT:          rdt,
		IP:           ip.String(),
		Port:         p,
		ExpectedSize: config.ExpectedSize,
		Domain:       config.Domain,
		Period:       config.Period,
		TLSCert:      certPEM,
		TLSKey:       keyPEM,
	}

	// create the task
	task := st.NewTask("assemble-cluster", "Assemble cluster")
	task.Set("assemble-cluster-setup", setup)

	// create and return task set
	ts := state.NewTaskSet(task)
	return ts, nil
}

// GetUncommittedClusterHeaders retrieves the uncommitted cluster state and
// returns headers formatted for signing.
func GetUncommittedClusterHeaders(st *state.State) (map[string]any, error) {
	var uncommitted UncommittedClusterState
	if err := st.Get("uncommitted-cluster-state", &uncommitted); err != nil {
		return nil, err
	}

	// TODO: is there a better way of doing this conversion?

	var devices []any
	if len(uncommitted.Devices) > 0 {
		devices = make([]any, 0, len(uncommitted.Devices))
		for _, d := range uncommitted.Devices {
			addresses := make([]any, 0, len(d.Addresses))
			for _, addr := range d.Addresses {
				addresses = append(addresses, addr)
			}

			devices = append(devices, map[string]any{
				"id":        strconv.Itoa(d.ID),
				"brand-id":  d.BrandID,
				"model":     d.Model,
				"serial":    d.Serial,
				"addresses": addresses,
			})
		}
	}

	var subclusters []any
	if len(uncommitted.Subclusters) > 0 {
		subclusters = make([]any, 0, len(uncommitted.Subclusters))
		for _, sc := range uncommitted.Subclusters {
			ids := make([]any, 0, len(sc.Devices))
			for _, id := range sc.Devices {
				ids = append(ids, strconv.Itoa(id))
			}

			subcluster := map[string]any{
				"name":    sc.Name,
				"devices": ids,
			}

			if len(sc.Snaps) > 0 {
				snaps := make([]any, 0, len(sc.Snaps))
				for _, snap := range sc.Snaps {
					snaps = append(snaps, map[string]any{
						"state":    snap.State,
						"instance": snap.Instance,
						"channel":  snap.Channel,
					})
				}
				subcluster["snaps"] = snaps
			}

			subclusters = append(subclusters, subcluster)
		}
	}

	return map[string]any{
		"type":        "cluster",
		"cluster-id":  uncommitted.ClusterID,
		"sequence":    "1", // TODO: handle sequences properly
		"devices":     devices,
		"subclusters": subclusters,
		"timestamp":   uncommitted.CompletedAt.Format(time.RFC3339),
	}, nil
}

// CommitClusterAssertion validates that a cluster assertion exists in the database
// and clears the uncommitted state if the cluster IDs match.
func CommitClusterAssertion(st *state.State, clusterID string) error {
	var uncommitted UncommittedClusterState
	if err := st.Get("uncommitted-cluster-state", &uncommitted); err != nil {
		return err
	}

	// verify the cluster ID matches the uncommitted state
	if clusterID != uncommitted.ClusterID {
		return fmt.Errorf("cluster ID mismatch: expected %s, got %s", uncommitted.ClusterID, clusterID)
	}

	// verify the cluster assertion exists in the database
	db := assertstate.DB(st)

	as, err := db.Find(asserts.ClusterType, map[string]string{
		"cluster-id": clusterID,
		"sequence":   "1", // TODO: handle sequences properly
	})
	if err != nil {
		return fmt.Errorf("cannot find cluster assertion: %v", err)
	}

	buf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(buf)

	// encode the cluster assertion itself
	if err := enc.Encode(as); err != nil {
		return err
	}

	// fetch and encode the account-key assertion
	ak, err := db.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": as.SignKeyID(),
	})
	if err != nil {
		return fmt.Errorf("cannot find account-key for signing key: %v", err)
	}

	if err := enc.Encode(ak); err != nil {
		return err
	}

	if err := os.MkdirAll("/tmp/snapd-clusterdb", 0755); err != nil {
		return err
	}

	if err := os.WriteFile("/tmp/snapd-clusterdb/cluster.assert", buf.Bytes(), 0644); err != nil {
		return err
	}

	cluster, ok := as.(*asserts.Cluster)
	if !ok {
		return fmt.Errorf("internal error: invalid assertion type: %T", as)
	}

	// TODO: this is a super temporary standin for our actual distributed
	// database solution
	for _, dev := range cluster.Devices() {
		for _, addr := range dev.Addresses {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return err
			}

			res, err := http.Post(fmt.Sprintf("http://%s:7070", host), "text/plain", bytes.NewReader(buf.Bytes()))
			if err != nil {
				return err
			}

			if err := res.Body.Close(); err != nil {
				return fmt.Errorf("cannot close body: %w", err)
			}

			if res.StatusCode != 200 {
				return fmt.Errorf("non-200 status code from peer: %d", res.StatusCode)
			}
		}
	}

	// clear the uncommitted state
	st.Set("uncommitted-cluster-state", nil)

	return nil
}

// createCertAndKey generates a self-signed certificate and private key for the given IP.
func createCertAndKey(ip net.IP) (certPEM []byte, keyPEM []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost-ed25519"},
		NotBefore:    now,
		NotAfter:     now.AddDate(100, 0, 0), // TODO: valid for 100 years, drop to an hour?
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{ip},
	}

	cert, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	if err != nil {
		return nil, nil, err
	}

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	return certPEM, keyPEM, nil
}
