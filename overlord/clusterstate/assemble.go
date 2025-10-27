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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"time"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
)

// AssembleConfig contains the configuration for creating a new cluster.
type AssembleConfig struct {
	// Secret is the shared secret used for cluster assembly authentication.
	Secret string
	// Address, in the format <ip>:<port>, is the address that this device binds
	// to during assembly.
	Address string
	// ExpectedSize is the expected number of devices in the cluster. If set to
	// 0, cluster assembly will run indefinitely until cancelled.
	ExpectedSize int
	// Period controls how often routes are published to cluster peers.
	Period time.Duration
}

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
		Period:       config.Period,
		TLSCert:      certPEM,
		TLSKey:       keyPEM,
	}

	task := st.NewTask("assemble-cluster", "Assemble cluster")
	task.Set("assemble-cluster-setup", setup)

	return state.NewTaskSet(task), nil
}

func assembleInProgress(st *state.State) error {
	for _, chg := range st.Changes() {
		if chg.Kind() == "assemble-cluster" && !chg.IsReady() {
			return errors.New("cluster assembly is already in progress")
		}
	}
	return nil
}

func createCertAndKey(ip net.IP) (certPEM []byte, keyPEM []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, nil, err
	}

	// tolerate a bit of inconsistency between clocks in the cluster
	now := time.Now().Add(-time.Minute)

	// TODO: an hour is fine for now. we will need a way to re-auth using device
	// identities post-assembly
	expires := now.Add(time.Hour)

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost-ed25519"},
		NotBefore:    now,
		NotAfter:     expires,
		KeyUsage:     x509.KeyUsageDigitalSignature,
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
