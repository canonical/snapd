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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/clusterstate"
)

var _ = check.Suite(&clusterSuite{})

type clusterSuite struct {
	apiBaseSuite
}

func (s *clusterSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)
	s.daemonWithOverlordMockAndStore()
	s.expectRootAccess()
}

func (s *clusterSuite) TestGetUncommittedClusterHeaders(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
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
				Snaps:   []asserts.ClusterSnap{},
			},
		},
		CompletedAt: completed,
	}
	st.Set("uncommitted-cluster-state", uncommitted)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/cluster/uncommitted", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Check(rsp.Status, check.Equals, 200)

	headers, ok := rsp.Result.(map[string]any)
	c.Assert(ok, check.Equals, true)

	c.Check(headers["type"], check.Equals, "cluster")
	c.Check(headers["cluster-id"], check.Equals, "bf3675f5-cffa-40f4-a119-7492ccc08e04")
	c.Check(headers["sequence"], check.Equals, "1")
	c.Check(headers["timestamp"], check.Equals, completed.Format(time.RFC3339))

	devices, ok := headers["devices"].([]any)
	c.Assert(ok, check.Equals, true)
	c.Check(len(devices), check.Equals, 2)

	subclusters, ok := headers["subclusters"].([]any)
	c.Assert(ok, check.Equals, true)
	c.Check(len(subclusters), check.Equals, 1)
}

func (s *clusterSuite) TestPostCommitClusterAssertion(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()

	key, _ := assertstest.GenerateKey(752)
	account := assertstest.NewAccount(s.StoreSigning, "user-1", map[string]any{
		"validation": "verified",
	}, "")

	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""), account)

	accountKey := assertstest.NewAccountKey(s.StoreSigning, account, nil, key.PublicKey(), "")
	assertstate.Add(st, accountKey)

	completed := time.Now().Truncate(time.Second).UTC()
	uncommitted := clusterstate.UncommittedClusterState{
		ClusterID: "bf3675f5-cffa-40f4-a119-7492ccc08e04",
		Devices: []asserts.ClusterDevice{
			{
				ID:        1,
				BrandID:   "canonical",
				Model:     "ubuntu-core-24-amd64",
				Serial:    "device-serial-1",
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
		CompletedAt: completed,
	}
	st.Set("uncommitted-cluster-state", uncommitted)
	st.Unlock()

	headers := map[string]any{
		"type":         "cluster",
		"authority-id": account.AccountID(),
		"cluster-id":   "bf3675f5-cffa-40f4-a119-7492ccc08e04",
		"sequence":     "1",
		"devices": []any{
			map[string]any{
				"id":        "1",
				"brand-id":  "canonical",
				"model":     "ubuntu-core-24-amd64",
				"serial":    "device-serial-1",
				"addresses": []any{"192.168.1.10"},
			},
		},
		"subclusters": []any{
			map[string]any{
				"name":    "default",
				"devices": []any{"1"},
			},
		},
		"timestamp": completed.Format(time.RFC3339),
	}

	signing := assertstest.NewSigningDB(account.AccountID(), key)
	cluster, err := signing.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	// add the cluster assertion to the database first (simulating /v2/assertions flow)
	st.Lock()
	err = assertstate.Add(st, cluster)
	st.Unlock()
	c.Assert(err, check.IsNil)

	// now commit using just the cluster ID
	body, err := json.Marshal(map[string]any{
		"cluster-id": "bf3675f5-cffa-40f4-a119-7492ccc08e04",
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/cluster/uncommitted", bytes.NewBuffer(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.IsNil)

	st.Lock()
	var checkState clusterstate.UncommittedClusterState
	err = st.Get("uncommitted-cluster-state", &checkState)
	st.Unlock()
	c.Check(err, check.NotNil)
}
