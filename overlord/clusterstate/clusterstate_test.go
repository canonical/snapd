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

package clusterstate_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { check.TestingT(t) }

type clusterStateSuite struct{}

var _ = check.Suite(&clusterStateSuite{})

func (s *clusterStateSuite) generateTestCert(c *check.C) ([]byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, check.IsNil)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	c.Assert(err, check.IsNil)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

func (s *clusterStateSuite) TestCreateClusterHappy(c *check.C) {
	st := state.New(nil)

	config := clusterstate.AssembleConfig{
		Secret:       "test-secret-12345",
		Address:      "127.0.0.1:8080",
		ExpectedSize: 3,
	}

	st.Lock()
	ts, err := clusterstate.Assemble(st, config)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Assert(ts, check.NotNil)

	tasks := ts.Tasks()
	c.Assert(tasks, check.HasLen, 1)

	task := tasks[0]
	c.Assert(task.Kind(), check.Equals, "assemble-cluster")

	// verify the setup was stored correctly
	st.Lock()
	var setup clusterstate.AssembleClusterSetup
	err = task.Get("assemble-cluster-setup", &setup)
	st.Unlock()
	c.Assert(err, check.IsNil)

	c.Check(setup.Secret, check.Equals, "test-secret-12345")
	c.Check(setup.RDT, check.Not(check.Equals), "") // RDT is randomly generated
	c.Check(setup.IP, check.Equals, "127.0.0.1")
	c.Check(setup.Port, check.Equals, 8080)
	c.Check(setup.ExpectedSize, check.Equals, 3)
	c.Check(len(setup.TLSCert) > 0, check.Equals, true) // cert is randomly generated
	c.Check(len(setup.TLSKey) > 0, check.Equals, true)  // key is randomly generated
}

func (s *clusterStateSuite) TestCreateClusterValidation(c *check.C) {
	st := state.New(nil)

	tests := []struct {
		name   string
		config clusterstate.AssembleConfig
		err    string
	}{
		{
			name: "missing secret",
			config: clusterstate.AssembleConfig{
				Address:      "127.0.0.1:8080",
				ExpectedSize: 3,
			},
			err: "secret is required",
		},
		{
			name: "missing address",
			config: clusterstate.AssembleConfig{
				Secret:       "test-secret",
				ExpectedSize: 3,
			},
			err: "address is required",
		},
		{
			name: "invalid address format - missing port",
			config: clusterstate.AssembleConfig{
				Secret:  "test-secret",
				Address: "127.0.0.1",
			},
			err: ".*missing port in address.*",
		},
		{
			name: "invalid IP in address",
			config: clusterstate.AssembleConfig{
				Secret:  "test-secret",
				Address: "not-an-ip:8080",
			},
			err: "invalid IP address in address",
		},
		{
			name: "invalid port",
			config: clusterstate.AssembleConfig{
				Secret:  "test-secret",
				Address: "127.0.0.1:invalid",
			},
			err: "invalid port in address.*",
		},
	}

	for _, tc := range tests {
		st.Lock()
		_, err := clusterstate.Assemble(st, tc.config)
		st.Unlock()
		c.Check(err, check.ErrorMatches, tc.err, check.Commentf("test case: %s", tc.name))
	}
}

func (s *clusterStateSuite) TestTaskAssembleClusterSetup(c *check.C) {
	st := state.New(nil)

	config := clusterstate.AssembleConfig{
		Secret:  "test-secret",
		Address: "192.168.1.100:9090",
	}

	st.Lock()
	ts, err := clusterstate.Assemble(st, config)
	st.Unlock()
	c.Assert(err, check.IsNil)

	task := ts.Tasks()[0]

	st.Lock()
	setup, err := clusterstate.TaskAssembleClusterSetup(task)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(setup.Secret, check.Equals, "test-secret")
	c.Check(setup.RDT, check.Not(check.Equals), "") // RDT is randomly generated
	c.Check(setup.IP, check.Equals, "192.168.1.100")
	c.Check(setup.Port, check.Equals, 9090)
}

func (s *clusterStateSuite) TestUncommittedClusterState(c *check.C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	uncommitted := clusterstate.UncommittedClusterState{
		ClusterID: "bf3675f5-cffa-40f4-a119-7492ccc08e04",
		Devices: []asserts.ClusterDevice{
			{
				ID:        1,
				BrandID:   "canonical",
				Model:     "ubuntu-core-24-amd64",
				Serial:    "9cc45ad6-d01b-4efd-9f76-db55b76c076b",
				Addresses: []string{"192.168.1.10", "10.0.0.10"},
			},
			{
				ID:        2,
				BrandID:   "canonical",
				Model:     "ubuntu-core-24-amd64",
				Serial:    "bc3c0a19-cdad-4cfc-a6f0-85e917bc6280",
				Addresses: []string{"192.168.1.20"},
			},
		},
		Subclusters: []asserts.ClusterSubcluster{
			{
				Name:    "default",
				Devices: []int{1, 2},
				Snaps:   []asserts.ClusterSnap{},
			},
		},
		CompletedAt: time.Now(),
	}

	st.Set("uncommitted-cluster-state", uncommitted)

	var retrieved clusterstate.UncommittedClusterState
	err := st.Get("uncommitted-cluster-state", &retrieved)
	c.Assert(err, check.IsNil)
	c.Check(retrieved.ClusterID, check.Equals, uncommitted.ClusterID)
	c.Check(len(retrieved.Devices), check.Equals, 2)
	c.Check(retrieved.Devices[0].BrandID, check.Equals, "canonical")
	c.Check(retrieved.Devices[0].Serial, check.Equals, "9cc45ad6-d01b-4efd-9f76-db55b76c076b")
	c.Check(retrieved.Devices[1].Serial, check.Equals, "bc3c0a19-cdad-4cfc-a6f0-85e917bc6280")
	c.Check(len(retrieved.Subclusters), check.Equals, 1)
	c.Check(retrieved.Subclusters[0].Name, check.Equals, "default")
	c.Check(retrieved.Subclusters[0].Devices, check.DeepEquals, []int{1, 2})
}

func (s *clusterStateSuite) TestGetUncommittedClusterHeaders(c *check.C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	completed := time.Now().Truncate(time.Second).UTC()
	uncommitted := clusterstate.UncommittedClusterState{
		ClusterID: "bf3675f5-cffa-40f4-a119-7492ccc08e04",
		Devices: []asserts.ClusterDevice{
			{
				ID:        1,
				BrandID:   "canonical",
				Model:     "ubuntu-core-24-amd64",
				Serial:    "9cc45ad6-d01b-4efd-9f76-db55b76c076b",
				Addresses: []string{"192.168.1.10"},
			},
			{
				ID:        2,
				BrandID:   "canonical",
				Model:     "ubuntu-core-24-amd64",
				Serial:    "bc3c0a19-cdad-4cfc-a6f0-85e917bc6280",
				Addresses: []string{"192.168.1.20"},
			},
		},
		Subclusters: []asserts.ClusterSubcluster{
			{
				Name:    "default",
				Devices: []int{1, 2},
				Snaps: []asserts.ClusterSnap{
					{
						State:    "clustered",
						Instance: "test-snap",
						Channel:  "stable",
					},
				},
			},
		},
		CompletedAt: completed,
	}

	st.Set("uncommitted-cluster-state", uncommitted)

	headers, err := clusterstate.GetUncommittedClusterHeaders(st)
	c.Assert(err, check.IsNil)

	c.Check(headers["type"], check.Equals, "cluster")
	c.Check(headers["cluster-id"], check.Equals, "bf3675f5-cffa-40f4-a119-7492ccc08e04")
	c.Check(headers["sequence"], check.Equals, "1")
	c.Check(headers["timestamp"], check.Equals, completed.Format(time.RFC3339))

	devices, ok := headers["devices"].([]any)
	c.Assert(ok, check.Equals, true)
	c.Check(len(devices), check.Equals, 2)
	dev0 := devices[0].(map[string]any)
	c.Check(dev0["id"], check.Equals, "1")
	c.Check(dev0["serial"], check.Equals, "9cc45ad6-d01b-4efd-9f76-db55b76c076b")

	subclusters, ok := headers["subclusters"].([]any)
	c.Assert(ok, check.Equals, true)
	c.Check(len(subclusters), check.Equals, 1)
	sc0 := subclusters[0].(map[string]any)
	c.Check(sc0["name"], check.Equals, "default")
}

func (s *clusterStateSuite) TestGetUncommittedClusterHeadersNoState(c *check.C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := clusterstate.GetUncommittedClusterHeaders(st)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *clusterStateSuite) TestCommitClusterAssertion(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	store := assertstest.NewStoreStack("canonical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	})
	c.Assert(err, check.IsNil)
	assertstate.ReplaceDB(st, db)

	err = db.Add(store.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	account := assertstest.NewAccount(store, "test-account", map[string]any{
		"validation": "verified",
	}, "")
	err = db.Add(account)
	c.Assert(err, check.IsNil)

	key, _ := assertstest.GenerateKey(752)
	err = db.Add(assertstest.NewAccountKey(store, account, nil, key.PublicKey(), ""))
	c.Assert(err, check.IsNil)

	uncommitted := clusterstate.UncommittedClusterState{
		ClusterID: "test-cluster-id",
		Devices: []asserts.ClusterDevice{
			{
				ID:        1,
				BrandID:   "canonical",
				Model:     "ubuntu-core-24-amd64",
				Serial:    "test-serial-1",
				Addresses: []string{"192.168.1.10"},
			},
		},
		Subclusters: []asserts.ClusterSubcluster{
			{
				Name:    "default",
				Devices: []int{1},
				Snaps:   []asserts.ClusterSnap{},
			},
		},
		CompletedAt: time.Now(),
	}
	st.Set("uncommitted-cluster-state", uncommitted)

	headers := map[string]any{
		"type":         "cluster",
		"authority-id": account.AccountID(),
		"cluster-id":   "test-cluster-id",
		"sequence":     "1",
		"devices": []any{
			map[string]any{
				"id":        "1",
				"brand-id":  "canonical",
				"model":     "ubuntu-core-24-amd64",
				"serial":    "test-serial-1",
				"addresses": []any{"192.168.1.10"},
			},
		},
		"subclusters": []any{
			map[string]any{
				"name":    "default",
				"devices": []any{"1"},
			},
		},
		"timestamp": uncommitted.CompletedAt.Format(time.RFC3339),
	}

	signingDB := assertstest.NewSigningDB(account.AccountID(), key)
	cluster, err := signingDB.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	err = clusterstate.CommitClusterAssertion(st, cluster.(*asserts.Cluster))
	c.Assert(err, check.IsNil)

	retrieved, err := db.Find(asserts.ClusterType, map[string]string{
		"cluster-id": "test-cluster-id",
		"sequence":   "1",
	})
	c.Assert(err, check.IsNil)
	c.Check(retrieved.(*asserts.Cluster).ClusterID(), check.Equals, "test-cluster-id")

	// verify uncommitted state was cleared
	uncommitted = clusterstate.UncommittedClusterState{}
	err = st.Get("uncommitted-cluster-state", &uncommitted)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *clusterStateSuite) TestCommitClusterAssertionNoUncommittedState(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	err := clusterstate.CommitClusterAssertion(st, &asserts.Cluster{})
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *clusterStateSuite) TestCommitClusterAssertionClusterIDMismatch(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	store := assertstest.NewStoreStack("canonical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	})
	c.Assert(err, check.IsNil)
	assertstate.ReplaceDB(st, db)

	err = db.Add(store.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	account := assertstest.NewAccount(store, "test-account", map[string]any{
		"validation": "verified",
	}, "")
	err = db.Add(account)
	c.Assert(err, check.IsNil)

	key, _ := assertstest.GenerateKey(752)
	err = db.Add(assertstest.NewAccountKey(store, account, nil, key.PublicKey(), ""))
	c.Assert(err, check.IsNil)

	// create uncommitted state with one cluster ID
	uncommitted := clusterstate.UncommittedClusterState{
		ClusterID:   "expected-cluster-id",
		Devices:     []asserts.ClusterDevice{},
		Subclusters: []asserts.ClusterSubcluster{},
		CompletedAt: time.Now(),
	}
	st.Set("uncommitted-cluster-state", uncommitted)

	// create cluster assertion with different cluster ID
	headers := map[string]any{
		"type":         "cluster",
		"authority-id": account.AccountID(),
		"cluster-id":   "different-cluster-id",
		"sequence":     "1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	signingDB := assertstest.NewSigningDB(account.AccountID(), key)
	cluster, err := signingDB.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	// commit should fail with cluster ID mismatch
	err = clusterstate.CommitClusterAssertion(st, cluster.(*asserts.Cluster))
	c.Check(err, check.ErrorMatches, "cluster ID mismatch: expected expected-cluster-id, got different-cluster-id")
}
