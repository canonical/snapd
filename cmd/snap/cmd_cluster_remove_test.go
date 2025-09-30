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

package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type clusterRemoveSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&clusterRemoveSuite{})

func (s *clusterRemoveSuite) TestClusterRemove(c *check.C) {
	// mock the API response for GET /v2/cluster/uncommitted
	initialState := client.UncommittedClusterState{
		ClusterID: "test-cluster",
		Devices: []client.ClusterDevice{
			{ID: 1, BrandID: "canonical", Model: "test", Serial: "123", Addresses: []string{"192.168.1.1"}},
		},
		Subclusters: []client.ClusterSubcluster{
			{
				Name:    "default",
				Devices: []int{1},
				Snaps: []client.ClusterSnap{
					{State: "clustered", Instance: "existing-snap", Channel: "stable"},
				},
			},
		},
		CompletedAt: time.Now(),
	}

	var updatedState client.UncommittedClusterState
	getCalled := false
	postCalled := false

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster/uncommitted":
			if r.Method == "GET" {
				getCalled = true
				w.Header().Set("Content-Type", "application/json")
				c.Assert(json.NewEncoder(w).Encode(map[string]interface{}{
					"type":   "sync",
					"result": initialState,
				}), check.IsNil)
			} else if r.Method == "POST" {
				postCalled = true
				var body map[string]interface{}
				decoder := json.NewDecoder(r.Body)
				c.Assert(decoder.Decode(&body), check.IsNil)

				// decode the state that was sent
				stateBytes, err := json.Marshal(body)
				c.Assert(err, check.IsNil)
				c.Assert(json.Unmarshal(stateBytes, &updatedState), check.IsNil)

				w.Header().Set("Content-Type", "application/json")
				c.Assert(json.NewEncoder(w).Encode(map[string]interface{}{
					"type":   "sync",
					"result": nil,
				}), check.IsNil)
			}
		default:
			c.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"cluster", "remove", "existing-snap"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `Marked "existing-snap" as removed in uncommitted cluster state.
`)
	c.Check(s.Stderr(), check.Equals, "")

	// verify API calls were made
	c.Check(getCalled, check.Equals, true)
	c.Check(postCalled, check.Equals, true)

	// verify the state was updated correctly
	c.Assert(updatedState.Subclusters, check.HasLen, 1)
	c.Assert(updatedState.Subclusters[0].Snaps, check.HasLen, 1)
	c.Check(updatedState.Subclusters[0].Snaps[0].Instance, check.Equals, "existing-snap")
	c.Check(updatedState.Subclusters[0].Snaps[0].State, check.Equals, "removed")
}

func (s *clusterRemoveSuite) TestClusterRemoveNewSnap(c *check.C) {
	// mock the API response for GET /v2/cluster/uncommitted
	initialState := client.UncommittedClusterState{
		ClusterID: "test-cluster",
		Devices: []client.ClusterDevice{
			{ID: 1, BrandID: "canonical", Model: "test", Serial: "123", Addresses: []string{"192.168.1.1"}},
		},
		Subclusters: []client.ClusterSubcluster{
			{
				Name:    "default",
				Devices: []int{1},
				Snaps:   []client.ClusterSnap{},
			},
		},
		CompletedAt: time.Now(),
	}

	var updatedState client.UncommittedClusterState

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster/uncommitted":
			if r.Method == "GET" {
				w.Header().Set("Content-Type", "application/json")
				c.Assert(json.NewEncoder(w).Encode(map[string]interface{}{
					"type":   "sync",
					"result": initialState,
				}), check.IsNil)
			} else if r.Method == "POST" {
				var body map[string]interface{}
				decoder := json.NewDecoder(r.Body)
				c.Assert(decoder.Decode(&body), check.IsNil)

				// decode the state that was sent
				stateBytes, err := json.Marshal(body)
				c.Assert(err, check.IsNil)
				c.Assert(json.Unmarshal(stateBytes, &updatedState), check.IsNil)

				w.Header().Set("Content-Type", "application/json")
				c.Assert(json.NewEncoder(w).Encode(map[string]interface{}{
					"type":   "sync",
					"result": nil,
				}), check.IsNil)
			}
		default:
			c.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"cluster", "remove", "new-snap"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `Marked "new-snap" as removed in uncommitted cluster state.
`)

	// verify the snap was added as removed
	c.Assert(updatedState.Subclusters, check.HasLen, 1)
	c.Assert(updatedState.Subclusters[0].Snaps, check.HasLen, 1)
	c.Check(updatedState.Subclusters[0].Snaps[0].Instance, check.Equals, "new-snap")
	c.Check(updatedState.Subclusters[0].Snaps[0].State, check.Equals, "removed")
	c.Check(updatedState.Subclusters[0].Snaps[0].Channel, check.Equals, "stable")
}

func (s *clusterRemoveSuite) TestClusterRemoveMissingDefaultSubcluster(c *check.C) {
	// mock the API response for GET /v2/cluster/uncommitted with no default subcluster
	initialState := client.UncommittedClusterState{
		ClusterID: "test-cluster",
		Devices: []client.ClusterDevice{
			{ID: 1, BrandID: "canonical", Model: "test", Serial: "123", Addresses: []string{"192.168.1.1"}},
		},
		Subclusters: []client.ClusterSubcluster{
			{
				Name:    "other",
				Devices: []int{1},
				Snaps:   []client.ClusterSnap{},
			},
		},
		CompletedAt: time.Now(),
	}

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster/uncommitted":
			if r.Method == "GET" {
				w.Header().Set("Content-Type", "application/json")
				c.Assert(json.NewEncoder(w).Encode(map[string]interface{}{
					"type":   "sync",
					"result": initialState,
				}), check.IsNil)
			}
		default:
			c.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"cluster", "remove", "test-snap"})
	c.Assert(err, check.ErrorMatches, "missing default subcluster")
}

func (s *clusterRemoveSuite) TestClusterRemoveNoSnapName(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"cluster", "remove"})
	c.Assert(err, check.ErrorMatches, "the required argument `<snap>` was not provided")
}

func (s *clusterRemoveSuite) TestClusterRemoveExtraArgs(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"cluster", "remove", "test-snap", "extra"})
	c.Assert(err, check.ErrorMatches, "too many arguments for command")
}

func (s *clusterRemoveSuite) TestClusterRemoveServerError(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/cluster/uncommitted":
			if r.Method == "GET" {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(w, `{"type":"error","status-code":500,"result":{"message":"server error"}}`)
			}
		default:
			c.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"cluster", "remove", "test-snap"})
	c.Assert(err, check.ErrorMatches, "server error")
}
