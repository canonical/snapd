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
	"github.com/snapcore/snapd/osutil"
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

type UncommittedClusterState struct {
	ClusterID   string              `json:"cluster-id"`
	Devices     []ClusterDevice     `json:"devices"`
	Subclusters []ClusterSubcluster `json:"subclusters"`
	CompletedAt time.Time           `json:"completed-at"`
	Sequence    int                 `json:"sequence"`
}

type ClusterDevice struct {
	ID        int      `json:"id"`
	BrandID   string   `json:"brand-id"`
	Model     string   `json:"model"`
	Serial    string   `json:"serial"`
	Addresses []string `json:"addresses"`
}

type ClusterSnap struct {
	State    string `json:"state"`
	Instance string `json:"instance"`
	Channel  string `json:"channel"`
}

type ClusterSubcluster struct {
	Name    string        `json:"name"`
	Devices []int         `json:"devices"`
	Snaps   []ClusterSnap `json:"snaps"`
}

// Assemble creates a new task to assemble a cluster using the given configuration.
func Assemble(st *state.State, config AssembleConfig) (*state.TaskSet, error) {
	if err := assembleInProgress(st); err != nil {
		return nil, err
	}

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

func assembleInProgress(st *state.State) error {
	for _, chg := range st.Changes() {
		if chg.Kind() == "assemble-cluster" && !chg.IsReady() {
			return errors.New("cluster assembly is already in progress")
		}
	}
	return nil
}

// GetUncommittedClusterState retrieves the uncommitted cluster state.
func GetUncommittedClusterState(st *state.State) (UncommittedClusterState, error) {
	var uncommitted UncommittedClusterState
	if err := st.Get("uncommitted-cluster-state", &uncommitted); err != nil {
		return UncommittedClusterState{}, err
	}
	return uncommitted, nil
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

	as, err := db.FindSequence(asserts.ClusterType, map[string]string{
		"cluster-id": clusterID,
	}, -1, asserts.ClusterType.MaxSupportedFormat())
	if err != nil {
		return fmt.Errorf("cannot find cluster assertion: %v", err)
	}

	buf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(buf)

	f := asserts.NewFetcher(db, func(r *asserts.Ref) (asserts.Assertion, error) {
		return r.Resolve(db.Find)
	}, enc.Encode)

	if err := f.Save(as); err != nil {
		return err
	}

	if err := os.MkdirAll("/tmp/snapd-clusterdb", 0755); err != nil {
		return err
	}

	if err := osutil.AtomicWriteFile("/tmp/snapd-clusterdb/cluster.assert", buf.Bytes(), 0644, 0); err != nil {
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

	uncommitted.Sequence = as.Sequence()
	UpdateUncommittedClusterState(st, uncommitted)

	st.EnsureBefore(0)

	return nil
}

// UpdateUncommittedClusterState updates the uncommitted cluster state in the state store.
func UpdateUncommittedClusterState(st *state.State, cs UncommittedClusterState) error {
	// validate the new state
	if cs.ClusterID == "" {
		return errors.New("cluster-id is required")
	}

	// store the updated state
	st.Set("uncommitted-cluster-state", cs)

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
