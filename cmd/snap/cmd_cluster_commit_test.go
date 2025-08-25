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

package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type clusterCommitSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&clusterCommitSuite{})

func (s *clusterCommitSuite) TestClusterCommitClientInteraction(c *check.C) {
	// create expected cluster headers
	headers := map[string]any{
		"type":       "cluster",
		"cluster-id": "test-cluster-123",
		"sequence":   "1",
		"devices": []any{
			map[string]any{
				"id":        "1",
				"brand-id":  "canonical",
				"model":     "ubuntu-core-24-amd64",
				"serial":    "device-1",
				"addresses": []any{"192.168.1.10"},
			},
		},
		"subclusters": []any{
			map[string]any{
				"name":    "default",
				"devices": []any{"1"},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// create a test assertion for the POST
	privKey, _ := assertstest.GenerateKey(752)
	storeStack := assertstest.NewStoreStack("canonical", nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeStack.Trusted,
	})
	c.Assert(err, check.IsNil)

	err = db.Add(storeStack.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	account := assertstest.NewAccount(storeStack, "test-account", map[string]any{
		"validation": "verified",
	}, "")
	err = db.Add(account)
	c.Assert(err, check.IsNil)

	accountKey := assertstest.NewAccountKey(storeStack, account, nil, privKey.PublicKey(), "")
	err = db.Add(accountKey)
	c.Assert(err, check.IsNil)

	// add authority-id to headers for signing
	headers["authority-id"] = account.AccountID()

	signingDB := assertstest.NewSigningDB(account.AccountID(), privKey)
	clusterAssert, err := signingDB.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	// track requests
	getCalled := false
	ackCalled := false
	commitCalled := false

	// mock client
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster/uncommitted":
			if r.Method == "GET" {
				getCalled = true
				// return the uncommitted state
				stateResponse := map[string]any{
					"cluster-id": "test-cluster-123",
					"devices": []any{
						map[string]any{
							"id":        1,
							"brand-id":  "canonical",
							"model":     "ubuntu-core-24-amd64",
							"serial":    "device-1",
							"addresses": []any{"192.168.1.10"},
						},
					},
					"subclusters": []any{
						map[string]any{
							"name":    "default",
							"devices": []any{1},
							"snaps":   []any{},
						},
					},
					"completed-at": headers["timestamp"],
				}
				fmt.Fprintln(w, fmt.Sprintf(`{"type":"sync","result":%s}`, jsonMarshal(c, stateResponse)))
			} else {
				c.Fatalf("unexpected method %q", r.Method)
			}
		case "/v2/cluster/commit":
			if r.Method == "POST" {
				commitCalled = true
				// verify we got a cluster ID
				var req map[string]string
				c.Assert(json.NewDecoder(r.Body).Decode(&req), check.IsNil)

				// expect cluster-id in the request
				clusterID, ok := req["cluster-id"]
				c.Assert(ok, check.Equals, true)
				c.Check(clusterID, check.Equals, "test-cluster-123")

				fmt.Fprintln(w, `{"type":"sync","result":null}`)
			} else {
				c.Fatalf("unexpected method %q", r.Method)
			}
		case "/v2/assertions":
			if r.Method == "POST" {
				ackCalled = true
				// verify we got assertions
				decoder := asserts.NewDecoder(r.Body)

				// should have at least the cluster assertion
				assertion, err := decoder.Decode()
				c.Assert(err, check.IsNil)
				c.Check(assertion.Type().Name, check.Equals, "cluster")

				fmt.Fprintln(w, `{"type":"sync","result":null}`)
			} else {
				c.Fatalf("unexpected method %q", r.Method)
			}
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	// note: we can't easily test the full command because it requires GPG setup
	// but we can verify the client methods work correctly

	// test GetClusterUncommittedState
	client := snap.Client()
	gotState, err := client.GetClusterUncommittedState()
	c.Assert(err, check.IsNil)
	c.Check(gotState.ClusterID, check.Equals, "test-cluster-123")
	c.Check(getCalled, check.Equals, true)

	// test Ack with the cluster assertion
	err = client.Ack(asserts.Encode(clusterAssert))
	c.Assert(err, check.IsNil)
	c.Check(ackCalled, check.Equals, true)

	// test CommitClusterAssertion with cluster ID
	err = client.CommitClusterAssertion("test-cluster-123")
	c.Assert(err, check.IsNil)
	c.Check(commitCalled, check.Equals, true)
}

func jsonMarshal(c *check.C, v any) string {
	b, err := json.Marshal(v)
	c.Assert(err, check.IsNil)
	return string(b)
}
