// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package store_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type storeActionSuite struct {
	baseStoreSuite

	mockXDelta *testutil.MockCmd
}

var _ = Suite(&storeActionSuite{})

func (s *storeActionSuite) SetUpTest(c *C) {
	s.baseStoreSuite.SetUpTest(c)

	s.mockXDelta = testutil.MockCommand(c, "xdelta3", "")
	s.AddCleanup(s.mockXDelta.Restore)
}

var (
	helloRefreshedDateStr = "2018-02-27T11:00:00Z"
	helloRefreshedDate    time.Time
)

func init() {
	t, err := time.Parse(time.RFC3339, helloRefreshedDateStr)
	if err != nil {
		panic(err)
	}
	helloRefreshedDate = t
}

const helloCohortKey = "this is a very short cohort key, as cohort keys go, because those are *long*"

func (s *storeActionSuite) TestSnapAction(c *C) {
	s.testSnapAction(c, nil)
}

func (s *storeActionSuite) TestSnapActionResources(c *C) {
	s.testSnapAction(c, []string{"component"})
}

func (s *storeActionSuite) testSnapAction(c *C, resources []string) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")
		c.Check(r.Header.Get("Snap-Refresh-Reason"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.DpkgArchitecture())
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, "")
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Fields  []string                 `json:"fields"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"cohort-key":   helloCohortKey,
		})

		expectedFields := make([]string, len(store.SnapActionFields))
		copy(expectedFields, store.SnapActionFields)
		if len(resources) > 0 {
			expectedFields = append(expectedFields, "resources")
		}

		c.Check(req.Fields, DeepEquals, expectedFields)

		res := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"result":       "refresh",
					"instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
					"snap-id":      "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
					"name":         "hello-world",
					"snap": map[string]interface{}{
						"snap-id":  "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
						"name":     "hello-world",
						"revision": 26,
						"version":  "6.1",
						"epoch":    map[string]interface{}{"read": []int{0}, "write": []int{0}},
						"publisher": map[string]interface{}{
							"id":           "canonical",
							"username":     "canonical",
							"display-name": "Canonical",
						},
					},
				},
			},
		}

		if len(resources) > 0 {
			res["results"].([]map[string]interface{})[0]["snap"].(map[string]interface{})["resources"] = []map[string]interface{}{
				{
					"download": map[string]interface{}{
						"sha3-384": "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
						"size":     1024,
						"url":      "https://example.com/comp.comp",
					},
					"type":        "component/test-component",
					"name":        "comp",
					"revision":    3,
					"version":     "1",
					"created-at":  "2023-06-02T19:34:30.179208",
					"description": "A test component",
				},
			}
		}

		json.NewEncoder(w).Encode(res)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, aresults, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			CohortKey:    helloCohortKey,
		},
	}, nil, nil, &store.RefreshOptions{IncludeResources: len(resources) > 0})
	c.Assert(err, IsNil)
	c.Assert(aresults, HasLen, 0)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	c.Assert(results[0].Epoch, DeepEquals, snap.E("0"))
	if len(resources) > 0 {
		c.Assert(results[0].Resources, HasLen, 1)
		c.Assert(results[0].Resources[0].Name, Equals, "comp")
		c.Assert(results[0].Resources[0].Type, Equals, "component/test-component")
		c.Assert(results[0].Resources[0].Revision, Equals, 3)
		c.Assert(results[0].Resources[0].Version, Equals, "1")
	}
}

func (s *storeActionSuite) TestSnapActionNonZeroEpochAndEpochBump(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	numReqs := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		numReqs++
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.DpkgArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Fields  []string                 `json:"fields"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Check(req.Fields, DeepEquals, store.SnapActionFields)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iFiveStarEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "epoch": {"read": [5, 6], "write": [6]},
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
			Epoch:           snap.E("5*"),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	c.Assert(results[0].Epoch, DeepEquals, snap.E("6*"))

	c.Assert(numReqs, Equals, 1) // should be >1 soon :-)
}

func (s *storeActionSuite) TestSnapActionNoResults(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 0)
		io.WriteString(w, `{
  "results": []
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, nil, nil, nil, nil)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})

	// local no-op
	results, _, err = sto.SnapAction(s.ctx, nil, nil, nil, nil, nil)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})

	c.Check(err.Error(), Equals, "no install/refresh information results from the store")
}

func (s *storeActionSuite) TestSnapActionRefreshedDateIsOptional(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":      helloWorldSnapID,
			"instance-key": helloWorldSnapID,

			"revision":         float64(1),
			"tracking-channel": "beta",
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 0)
		io.WriteString(w, `{
  "results": []
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
		},
	}, nil, nil, nil, nil)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})
}

func (s *storeActionSuite) TestSnapActionSkipBlocked(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
			Block:           []snap.Revision{snap.R(26)},
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": store.ErrNoUpdateAvailable,
		},
	})
}

func (s *storeActionSuite) TestSnapActionSkipCurrent(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": store.ErrNoUpdateAvailable,
		},
	})
}

func (s *storeActionSuite) TestSnapActionRetryOnEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		n++
		if n < 4 {
			io.WriteString(w, "{")
			mockServer.CloseClientConnections()
			return
		}

		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		c.Assert(err, IsNil)
		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Actions, HasLen, 1)
		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
}

func (s *storeActionSuite) TestSnapActionIgnoreValidation(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":           helloWorldSnapID,
			"instance-key":      helloWorldSnapID,
			"revision":          float64(1),
			"tracking-channel":  "stable",
			"refreshed-date":    helloRefreshedDateStr,
			"ignore-validation": true,
			"epoch":             iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":            "refresh",
			"instance-key":      helloWorldSnapID,
			"snap-id":           helloWorldSnapID,
			"channel":           "stable",
			"ignore-validation": false,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:     "hello-world",
			SnapID:           helloWorldSnapID,
			TrackingChannel:  "stable",
			Revision:         snap.R(1),
			RefreshedDate:    helloRefreshedDate,
			IgnoreValidation: true,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
			Flags:        store.SnapActionEnforceValidation,
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionInstallWithValidationSets(c *C) {
	s.testSnapActionGet("install", "", "", []snapasserts.ValidationSetKey{"foo/bar", "foo/baz"}, c)
}

func (s *storeActionSuite) TestSnapActionAutoRefresh(c *C) {
	// the bare TestSnapAction does more SnapAction checks; look there
	// this one mostly just checks the refresh-reason header

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		c.Check(r.Header.Get("Snap-Refresh-Reason"), Equals, "scheduled")

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "epoch": {"read": [0], "write": [0]},
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil, &store.RefreshOptions{Scheduled: true})
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
}

func (s *storeActionSuite) TestInstallFallbackChannelIsStable(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:  "hello-world",
			SnapID:        helloWorldSnapID,
			RefreshedDate: helloRefreshedDate,
			Revision:      snap.R(1),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
}

func (s *storeActionSuite) TestSnapActionNonDefaultsHeaders(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "foo")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, "21")
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, "archXYZ")
		c.Check(r.Header.Get("Snap-Device-Location"), Equals, `cloud-name="gcp" region="us-west1" availability-zone="us-west1-b"`)
		c.Check(r.Header.Get("Snap-Classic"), Equals, "true")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.DefaultConfig()
	cfg.StoreBaseURL = mockServerURL
	cfg.Series = "21"
	cfg.Architecture = "archXYZ"
	cfg.StoreID = "foo"
	dauthCtx := &testDauthContext{c: c, device: s.device, cloudInfo: &auth.CloudInfo{Name: "gcp", Region: "us-west1", AvailabilityZone: "us-west1-b"}}
	sto := store.New(cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			RefreshedDate:   helloRefreshedDate,
			Revision:        snap.R(1),
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
}

func (s *storeActionSuite) TestSnapActionWithDeltas(c *C) {
	origUseDeltas := os.Getenv("SNAPD_USE_DELTAS_EXPERIMENTAL")
	defer os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", origUseDeltas)
	c.Assert(os.Setenv("SNAPD_USE_DELTAS_EXPERIMENTAL", "1"), IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Accept-Delta-Format"), Equals, "xdelta3")
		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "beta",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionOptions(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "true")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		},
	}, nil, nil, &store.RefreshOptions{RefreshManaged: true})
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionInstall(c *C) {
	s.testSnapActionGet("install", "", "", nil, c)
}
func (s *storeActionSuite) TestSnapActionInstallWithCohort(c *C) {
	s.testSnapActionGet("install", "what", "", nil, c)
}
func (s *storeActionSuite) TestSnapActionDownload(c *C) {
	s.testSnapActionGet("download", "", "", nil, c)
}
func (s *storeActionSuite) TestSnapActionDownloadWithCohort(c *C) {
	s.testSnapActionGet("download", "here", "", nil, c)
}
func (s *storeActionSuite) TestSnapActionInstallRedirect(c *C) {
	s.testSnapActionGet("install", "", "2.0/candidate", nil, c)
}
func (s *storeActionSuite) TestSnapActionDownloadRedirect(c *C) {
	s.testSnapActionGet("download", "", "2.0/candidate", nil, c)
}
func (s *storeActionSuite) testSnapActionGet(action, cohort, redirectChannel string, validationSets []snapasserts.ValidationSetKey, c *C) {
	// action here is one of install or download
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.DpkgArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		expectedAction := map[string]interface{}{
			"action":       action,
			"instance-key": action + "-1",
			"name":         "hello-world",
			"channel":      "beta",
			"epoch":        nil,
		}
		if cohort != "" {
			expectedAction["cohort-key"] = cohort
		}
		if validationSets != nil {
			// XXX: rewrite as otherwise DeepEquals complains about
			// []interface {}{[]interface {}{..} vs expected [][]string{[]string{..}.
			var sets []interface{}
			for _, vs := range validationSets {
				var vss []interface{}
				for _, vv := range vs.Components() {
					vss = append(vss, vv)
				}
				sets = append(sets, vss)
			}
			expectedAction["validation-sets"] = sets
		}
		c.Assert(req.Actions[0], DeepEquals, expectedAction)

		fmt.Fprintf(w, `{
  "results": [{
     "result": "%s",
     "instance-key": "%[1]s-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "effective-channel": "candidate",
     "redirect-channel": "%s",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`, action, redirectChannel)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil,
		[]*store.SnapAction{
			{
				Action:         action,
				InstanceName:   "hello-world",
				Channel:        "beta",
				CohortKey:      cohort,
				ValidationSets: validationSets,
			},
		}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel
	c.Assert(results[0].Channel, Equals, "candidate")
	c.Assert(results[0].RedirectChannel, Equals, redirectChannel)
}

func (s *storeActionSuite) TestSnapActionInstallAmend(c *C) {
	// this is what amend would look like
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.DpkgArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "hello-world",
			"channel":      "beta",
			"epoch":        map[string]interface{}{"read": []interface{}{0., 1.}, "write": []interface{}{1.}},
		})

		fmt.Fprint(w, `{
  "results": [{
     "result": "install",
     "instance-key": "install-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "effective-channel": "candidate",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil,
		[]*store.SnapAction{
			{
				Action:       "install",
				InstanceName: "hello-world",
				Channel:      "beta",
				Epoch:        snap.E("1*"),
			},
		}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel
	c.Assert(results[0].Channel, Equals, "candidate")
}

func (s *storeActionSuite) TestSnapActionWithClientUserAgent(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	serverCalls := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		assertRequest(c, r, "POST", snapActionPath)

		c.Check(r.Header.Get("Snap-Client-User-Agent"), Equals, "some-snap-agent/1.0")

		io.WriteString(w, `{
  "results": []
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	// to construct the client-user-agent context we need to
	// create a req that simulates what the req that the daemon got
	r, err := http.NewRequest("POST", "/snapd/api", nil)
	r.Header.Set("User-Agent", "some-snap-agent/1.0")
	c.Assert(err, IsNil)
	ctx := store.WithClientUserAgent(s.ctx, r)

	results, _, err := sto.SnapAction(ctx, nil, []*store.SnapAction{{Action: "install", InstanceName: "some-snap"}}, nil, nil, nil)
	c.Check(serverCalls, Equals, 1)
	c.Check(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{NoResults: true})
}

func (s *storeActionSuite) TestSnapActionDownloadParallelInstanceKey(c *C) {
	// action here is one of install or download
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("should not be reached")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	_, _, err := sto.SnapAction(s.ctx, nil,
		[]*store.SnapAction{
			{
				Action:       "download",
				InstanceName: "hello-world_foo",
				Channel:      "beta",
			},
		}, nil, nil, nil)
	c.Assert(err, ErrorMatches, `internal error: unsupported download with instance name "hello-world_foo"`)
}

func (s *storeActionSuite) TestSnapActionInstallWithRevision(c *C) {
	s.testSnapActionGetWithRevision("install", c)
}

func (s *storeActionSuite) TestSnapActionDownloadWithRevision(c *C) {
	s.testSnapActionGetWithRevision("download", c)
}

func (s *storeActionSuite) testSnapActionGetWithRevision(action string, c *C) {
	// action here is one of install or download
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Snap-Refresh-Managed"), Equals, "")

		// no store ID by default
		storeID := r.Header.Get("Snap-Device-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.Header.Get("Snap-Device-Series"), Equals, release.Series)
		c.Check(r.Header.Get("Snap-Device-Architecture"), Equals, arch.DpkgArchitecture())
		c.Check(r.Header.Get("Snap-Classic"), Equals, "false")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       action,
			"instance-key": action + "-1",
			"name":         "hello-world",
			"revision":     float64(28),
			"epoch":        nil,
		})

		fmt.Fprintf(w, `{
  "results": [{
     "result": "%s",
     "instance-key": "%[1]s-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`, action)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil,
		[]*store.SnapAction{
			{
				Action:       action,
				InstanceName: "hello-world",
				Revision:     snap.R(28),
			},
		}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(28))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Publisher.ID, Equals, helloWorldDeveloperID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel is not set
	c.Assert(results[0].Channel, Equals, "")
}

func (s *storeActionSuite) TestSnapActionRevisionNotAvailable(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          "snap2-id",
			"instance-key":     "snap2-id",
			"revision":         float64(2),
			"tracking-channel": "edge",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 4)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})
		c.Assert(req.Actions[1], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": "snap2-id",
			"snap-id":      "snap2-id",
			"channel":      "candidate",
		})
		c.Assert(req.Actions[2], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo",
			"channel":      "stable",
			"epoch":        nil,
		})
		c.Assert(req.Actions[3], DeepEquals, map[string]interface{}{
			"action":       "download",
			"instance-key": "download-1",
			"name":         "bar",
			"revision":     42.,
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "error": {
       "code": "revision-not-found",
       "message": "msg1"
     }
  }, {
     "result": "error",
     "instance-key": "snap2-id",
     "snap-id": "snap2-id",
     "name": "snap2",
     "error": {
       "code": "revision-not-found",
       "message": "msg1",
       "extra": {
         "releases": [{"architecture": "amd64", "channel": "beta"},
                      {"architecture": "arm64", "channel": "beta"}]
       }
     }
  }, {
     "result": "error",
     "instance-key": "install-1",
     "snap-id": "foo-id",
     "name": "foo",
     "error": {
       "code": "revision-not-found",
       "message": "msg2"
     }
  }, {
     "result": "error",
     "instance-key": "download-1",
     "snap-id": "bar-id",
     "name": "bar",
     "error": {
       "code": "revision-not-found",
       "message": "msg3"
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
		{
			InstanceName:    "snap2",
			SnapID:          "snap2-id",
			TrackingChannel: "edge",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			InstanceName: "hello-world",
			SnapID:       helloWorldSnapID,
		}, {
			Action:       "refresh",
			InstanceName: "snap2",
			SnapID:       "snap2-id",
			Channel:      "candidate",
		}, {
			Action:       "install",
			InstanceName: "foo",
			Channel:      "stable",
		}, {
			Action:       "download",
			InstanceName: "bar",
			Revision:     snap.R(42),
		},
	}, nil, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "stable",
			},
			"snap2": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "candidate",
				Releases: []channel.Channel{
					snaptest.MustParseChannel("beta", "amd64"),
					snaptest.MustParseChannel("beta", "arm64"),
				},
			},
		},
		Install: map[string]error{
			"foo": &store.RevisionNotAvailableError{
				Action:  "install",
				Channel: "stable",
			},
		},
		Download: map[string]error{
			"bar": &store.RevisionNotAvailableError{
				Action:  "download",
				Channel: "",
			},
		},
	})
}

func (s *storeActionSuite) TestSnapActionSnapNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 3)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})
		c.Assert(req.Actions[1], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo",
			"channel":      "stable",
			"epoch":        nil,
		})
		c.Assert(req.Actions[2], DeepEquals, map[string]interface{}{
			"action":       "download",
			"instance-key": "download-1",
			"name":         "bar",
			"revision":     42.,
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "error": {
       "code": "id-not-found",
       "message": "msg1"
     }
  }, {
     "result": "error",
     "instance-key": "install-1",
     "name": "foo",
     "error": {
       "code": "name-not-found",
       "message": "msg2"
     }
  }, {
     "result": "error",
     "instance-key": "download-1",
     "name": "bar",
     "error": {
       "code": "name-not-found",
       "message": "msg3"
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
			Channel:      "stable",
		}, {
			Action:       "install",
			InstanceName: "foo",
			Channel:      "stable",
		}, {
			Action:       "download",
			InstanceName: "bar",
			Revision:     snap.R(42),
		},
	}, nil, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": store.ErrSnapNotFound,
		},
		Install: map[string]error{
			"foo": store.ErrSnapNotFound,
		},
		Download: map[string]error{
			"bar": store.ErrSnapNotFound,
		},
	})
}

func (s *storeActionSuite) TestSnapActionOtherErrors(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo",
			"channel":      "stable",
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "error": {
       "code": "other1",
       "message": "other error one"
     }
  }],
  "error-list": [
     {"code": "global-error", "message": "global error"}
  ]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "foo",
			Channel:      "stable",
		},
	}, nil, nil, nil)
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Other: []error{
			fmt.Errorf("other error one"),
			fmt.Errorf("global error"),
		},
	})
}

func (s *storeActionSuite) TestSnapActionUnknownAction(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Fatal("should not have made it to the server")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil,
		[]*store.SnapAction{
			{
				Action:       "something unexpected",
				InstanceName: "hello-world",
			},
		}, nil, nil, nil)
	c.Assert(err, ErrorMatches, `.* unsupported action .*`)
	c.Assert(results, IsNil)
}

func (s *storeActionSuite) TestSnapActionErrorError(c *C) {
	e := &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh"),
	}}
	c.Check(e.Error(), Equals, `cannot refresh snap "foo": sad refresh`)

	op, name, err := e.SingleOpError()
	c.Check(op, Equals, "refresh")
	c.Check(name, Equals, "foo")
	c.Check(err, ErrorMatches, "sad refresh")

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
		"bar": fmt.Errorf("sad refresh 2"),
	}}
	errMsg := e.Error()
	c.Check(strings.HasPrefix(errMsg, "cannot refresh:"), Equals, true)
	c.Check(errMsg, testutil.Contains, "\nsad refresh 1: \"foo\"")
	c.Check(errMsg, testutil.Contains, "\nsad refresh 2: \"bar\"")

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{Install: map[string]error{
		"foo": fmt.Errorf("sad install"),
	}}
	c.Check(e.Error(), Equals, `cannot install snap "foo": sad install`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "install")
	c.Check(name, Equals, "foo")
	c.Check(err, ErrorMatches, "sad install")

	e = &store.SnapActionError{Install: map[string]error{
		"foo": fmt.Errorf("sad install 1"),
		"bar": fmt.Errorf("sad install 2"),
	}}
	errMsg = e.Error()
	c.Check(strings.HasPrefix(errMsg, "cannot install:\n"), Equals, true)
	c.Check(errMsg, testutil.Contains, "\nsad install 1: \"foo\"")
	c.Check(errMsg, testutil.Contains, "\nsad install 2: \"bar\"")

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{Download: map[string]error{
		"foo": fmt.Errorf("sad download"),
	}}
	c.Check(e.Error(), Equals, `cannot download snap "foo": sad download`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "download")
	c.Check(name, Equals, "foo")
	c.Check(err, ErrorMatches, "sad download")

	e = &store.SnapActionError{Download: map[string]error{
		"foo": fmt.Errorf("sad download 1"),
		"bar": fmt.Errorf("sad download 2"),
	}}
	errMsg = e.Error()
	c.Check(strings.HasPrefix(errMsg, "cannot download:\n"), Equals, true)
	c.Check(errMsg, testutil.Contains, "\nsad download 1: \"foo\"")
	c.Check(errMsg, testutil.Contains, "\nsad download 2: \"bar\"")

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
	},
		Install: map[string]error{
			"bar": fmt.Errorf("sad install 2"),
		}}
	c.Check(e.Error(), Equals, `cannot refresh or install:
sad refresh 1: "foo"
sad install 2: "bar"`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
	},
		Download: map[string]error{
			"bar": fmt.Errorf("sad download 2"),
		}}
	c.Check(e.Error(), Equals, `cannot refresh or download:
sad refresh 1: "foo"
sad download 2: "bar"`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{Install: map[string]error{
		"foo": fmt.Errorf("sad install 1"),
	},
		Download: map[string]error{
			"bar": fmt.Errorf("sad download 2"),
		}}
	c.Check(e.Error(), Equals, `cannot install or download:
sad install 1: "foo"
sad download 2: "bar"`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{Refresh: map[string]error{
		"foo": fmt.Errorf("sad refresh 1"),
	},
		Install: map[string]error{
			"bar": fmt.Errorf("sad install 2"),
		},
		Download: map[string]error{
			"baz": fmt.Errorf("sad download 3"),
		}}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download:
sad refresh 1: "foo"
sad install 2: "bar"
sad download 3: "baz"`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{
		NoResults: true,
		Other:     []error{fmt.Errorf("other error")},
	}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download: other error`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{
		Other: []error{fmt.Errorf("other error 1"), fmt.Errorf("other error 2")},
	}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download:
other error 1
other error 2`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{
		Install: map[string]error{
			"bar": fmt.Errorf("sad install"),
		},
		Other: []error{fmt.Errorf("other error 1"), fmt.Errorf("other error 2")},
	}
	c.Check(e.Error(), Equals, `cannot refresh, install, or download:
sad install: "bar"
other error 1
other error 2`)

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)

	e = &store.SnapActionError{
		NoResults: true,
	}
	c.Check(e.Error(), Equals, "no install/refresh information results from the store")

	op, name, err = e.SingleOpError()
	c.Check(op, Equals, "")
	c.Check(name, Equals, "")
	c.Check(err, IsNil)
}

func (s *storeActionSuite) TestSnapActionRefreshesBothAuths(c *C) {
	// snap action (install/refresh) has is its own custom way to
	// signal macaroon refreshes that allows to do a best effort
	// with the available results

	refresh, err := makeTestRefreshDischargeResponse()
	c.Assert(err, IsNil)
	c.Check(s.user.StoreDischarges[0], Not(Equals), refresh)

	// mock refresh response
	refreshDischargeEndpointHit := false
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fmt.Sprintf(`{"discharge_macaroon": "%s"}`, refresh))
		refreshDischargeEndpointHit = true
	}))
	defer mockSSOServer.Close()
	store.UbuntuoneRefreshDischargeAPI = mockSSOServer.URL + "/tokens/refresh"

	refreshSessionRequested := false
	expiredAuth := `Macaroon root="expired-session-macaroon"`
	n := 0
	// mock store response
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		switch r.URL.Path {
		case snapActionPath:
			n++
			type errObj struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			var errors []errObj

			authorization := r.Header.Get("Authorization")
			c.Check(authorization, Equals, expectedAuthorization(c, s.user))
			if s.user.StoreDischarges[0] != refresh {
				errors = append(errors, errObj{Code: "user-authorization-needs-refresh"})
			}

			devAuthorization := r.Header.Get("Snap-Device-Authorization")
			if devAuthorization == "" {
				c.Fatalf("device authentication missing")
			} else if devAuthorization == expiredAuth {
				errors = append(errors, errObj{Code: "device-authorization-needs-refresh"})
			} else {
				c.Check(devAuthorization, Equals, `Macaroon root="refreshed-session-macaroon"`)
			}

			errorsJSON, err := json.Marshal(errors)
			c.Assert(err, IsNil)

			io.WriteString(w, fmt.Sprintf(`{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "name": "canonical",
          "title": "Canonical"
       }
     }
  }],
  "error-list": %s
}`, errorsJSON))
		case authNoncesPath:
			io.WriteString(w, `{"nonce": "1234567890:9876543210"}`)
		case authSessionPath:
			// validity of request
			jsonReq, err := io.ReadAll(r.Body)
			c.Assert(err, IsNil)
			var req map[string]string
			err = json.Unmarshal(jsonReq, &req)
			c.Assert(err, IsNil)
			c.Check(strings.HasPrefix(req["device-session-request"], "type: device-session-request\n"), Equals, true)
			c.Check(strings.HasPrefix(req["serial-assertion"], "type: serial\n"), Equals, true)
			c.Check(strings.HasPrefix(req["model-assertion"], "type: model\n"), Equals, true)

			authorization := r.Header.Get("X-Device-Authorization")
			if authorization == "" {
				c.Fatalf("expecting only refresh")
			} else {
				c.Check(authorization, Equals, expiredAuth)
				io.WriteString(w, `{"macaroon": "refreshed-session-macaroon"}`)
				refreshSessionRequested = true
			}
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)

	// make sure device session is expired
	s.device.SessionMacaroon = "expired-session-macaroon"
	dauthCtx := &testDauthContext{c: c, device: s.device, user: s.user}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "beta",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			InstanceName: "hello-world",
		},
	}, nil, s.user, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Check(refreshDischargeEndpointHit, Equals, true)
	c.Check(refreshSessionRequested, Equals, true)
	c.Check(n, Equals, 2)
}

func (s *storeActionSuite) TestSnapActionRefreshParallelInstall(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldFooInstanceKeyWithSalt,
			"revision":         float64(2),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldFooInstanceKeyWithSalt,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ:IDKVhLy-HUyfYGFKcsH4V-7FVG7hLGs4M5zsraZU5tk",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		}, {
			InstanceName:    "hello-world_foo",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world_foo",
		},
	}, nil, nil, &store.RefreshOptions{PrivacyKey: "123"})
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].InstanceName(), Equals, "hello-world_foo")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionRefreshStableInstanceKey(c *C) {
	// salt "foo"
	helloWorldFooInstanceKeyWithSaltFoo := helloWorldSnapID + ":CY2pHZ7nlQDuiO5DxIsdRttcqqBoD2ZCQiEtCJSdVcI"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
			"cohort-key":       "what",
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldFooInstanceKeyWithSaltFoo,
			"revision":         float64(2),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldFooInstanceKeyWithSaltFoo,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ:CY2pHZ7nlQDuiO5DxIsdRttcqqBoD2ZCQiEtCJSdVcI",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	opts := &store.RefreshOptions{PrivacyKey: "foo"}
	currentSnaps := []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
			CohortKey:       "what",
		}, {
			InstanceName:    "hello-world_foo",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}
	action := []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world_foo",
		},
	}
	results, _, err := sto.SnapAction(s.ctx, currentSnaps, action, nil, nil, opts)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].InstanceName(), Equals, "hello-world_foo")
	c.Assert(results[0].Revision, Equals, snap.R(26))

	// another request with the same seed, gives same result
	resultsAgain, _, err := sto.SnapAction(s.ctx, currentSnaps, action, nil, nil, opts)
	c.Assert(err, IsNil)
	c.Assert(resultsAgain, DeepEquals, results)
}

func (s *storeActionSuite) TestSnapActionRefreshWithHeld(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
			"held":             map[string]interface{}{"by": []interface{}{"foo", "bar"}},
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
			HeldBy:          []string{"foo", "bar"},
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world",
		},
	}, nil, nil, &store.RefreshOptions{PrivacyKey: "123"})

	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionRefreshWithHeldUnsupportedProxy(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		// `held` field was introduced in version 55 https://api.snapcraft.io/docs/
		// mock version that doesn't support `held` field (e.g. 52)
		w.Header().Set("Snap-Store-Version", "52")

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)
		c.Assert(req.Context, HasLen, 1)
		if _, exists := req.Context[0]["held"]; exists {
			w.WriteHeader(400)
			io.WriteString(w, `{
  "error-list":[{
    "code":"api-error",
    "message":"Additional properties are not allowed ('held' was unexpected) at /context/0"
  }]
}`)
			return
		}
		// snap action should retry without the `held` field
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
			HeldBy:          []string{"foo", "bar"},
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world",
		},
	}, nil, nil, &store.RefreshOptions{PrivacyKey: "123"})

	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionRefreshWithValidationSets(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(1),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
			"validation-sets":  []interface{}{[]interface{}{"foo", "other"}},
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":          "refresh",
			"instance-key":    helloWorldSnapID,
			"snap-id":         helloWorldSnapID,
			"channel":         "stable",
			"validation-sets": []interface{}{[]interface{}{"foo", "bar"}, []interface{}{"foo", "baz"}},
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(1),
			RefreshedDate:   helloRefreshedDate,
			// not actually set during refresh, but supported by snapAction
			ValidationSets: []snapasserts.ValidationSetKey{"foo/other"},
		},
	}, []*store.SnapAction{
		{
			Action:         "refresh",
			SnapID:         helloWorldSnapID,
			Channel:        "stable",
			InstanceName:   "hello-world",
			ValidationSets: []snapasserts.ValidationSetKey{"foo/bar", "foo/baz"},
		},
	}, nil, nil, &store.RefreshOptions{PrivacyKey: "123"})
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].InstanceName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
}

func (s *storeActionSuite) TestSnapActionRevisionNotAvailableParallelInstall(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldFooInstanceKeyWithSalt,
			"revision":         float64(2),
			"tracking-channel": "edge",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 3)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
		})
		c.Assert(req.Actions[1], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldFooInstanceKeyWithSalt,
			"snap-id":      helloWorldSnapID,
		})
		c.Assert(req.Actions[2], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "other",
			"channel":      "stable",
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "error": {
       "code": "revision-not-found",
       "message": "msg1"
     }
  }, {
     "result": "error",
     "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ:IDKVhLy-HUyfYGFKcsH4V-7FVG7hLGs4M5zsraZU5tk",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "error": {
       "code": "revision-not-found",
       "message": "msg2"
     }
  },  {
     "result": "error",
     "instance-key": "install-1",
     "snap-id": "foo-id",
     "name": "other",
     "error": {
       "code": "revision-not-found",
       "message": "msg3"
     }
  }
  ]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
		{
			InstanceName:    "hello-world_foo",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "edge",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			InstanceName: "hello-world",
			SnapID:       helloWorldSnapID,
		}, {
			Action:       "refresh",
			InstanceName: "hello-world_foo",
			SnapID:       helloWorldSnapID,
		}, {
			Action:       "install",
			InstanceName: "other_foo",
			Channel:      "stable",
		},
	}, nil, nil, &store.RefreshOptions{PrivacyKey: "123"})
	c.Assert(results, HasLen, 0)
	c.Check(err, DeepEquals, &store.SnapActionError{
		Refresh: map[string]error{
			"hello-world": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "stable",
			},
			"hello-world_foo": &store.RevisionNotAvailableError{
				Action:  "refresh",
				Channel: "edge",
			},
		},
		Install: map[string]error{
			"other_foo": &store.RevisionNotAvailableError{
				Action:  "install",
				Channel: "stable",
			},
		},
	})
}

func (s *storeActionSuite) TestSnapActionInstallParallelInstall(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "hello-world",
			"channel":      "stable",
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "install",
     "instance-key": "install-1",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "hello-world_foo",
			Channel:      "stable",
		},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "hello-world_foo")
	c.Assert(results[0].SnapName(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(28))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].Deltas, HasLen, 0)
	// effective-channel is not set
	c.Assert(results[0].Channel, Equals, "")
}

func (s *storeActionSuite) TestSnapActionErrorsWhenNoInstanceName(c *C) {
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{}, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:  "install",
			Channel: "stable",
		},
	}, nil, nil, nil)
	c.Assert(err, ErrorMatches, "internal error: action without instance name")
	c.Assert(results, IsNil)
}

func (s *storeActionSuite) TestSnapActionInstallUnexpectedInstanceKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "hello-world",
			"channel":      "stable",
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "install",
     "instance-key": "foo-2",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "hello-world_foo",
			Channel:      "stable",
		},
	}, nil, nil, nil)
	c.Assert(err, ErrorMatches, `unexpected invalid install/refresh API result: unexpected instance-key "foo-2"`)
	c.Assert(results, IsNil)
}

func (s *storeActionSuite) TestSnapActionRefreshUnexpectedInstanceKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 1)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "refresh",
			"instance-key": helloWorldSnapID,
			"snap-id":      helloWorldSnapID,
			"channel":      "stable",
		})

		io.WriteString(w, `{
  "results": [{
     "result": "refresh",
     "instance-key": "foo-5",
     "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
     "name": "hello-world",
     "snap": {
       "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
       "name": "hello-world",
       "revision": 26,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "refresh",
			SnapID:       helloWorldSnapID,
			Channel:      "stable",
			InstanceName: "hello-world",
		},
	}, nil, nil, nil)
	c.Assert(err, ErrorMatches, `unexpected invalid install/refresh API result: unexpected refresh`)
	c.Assert(results, IsNil)
}

func (s *storeActionSuite) TestSnapActionUnexpectedErrorKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 2)
		c.Assert(req.Context[0], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldSnapID,
			"revision":         float64(26),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Context[1], DeepEquals, map[string]interface{}{
			"snap-id":          helloWorldSnapID,
			"instance-key":     helloWorldFooInstanceKeyWithSalt,
			"revision":         float64(2),
			"tracking-channel": "stable",
			"refreshed-date":   helloRefreshedDateStr,
			"epoch":            iZeroEpoch,
		})
		c.Assert(req.Actions, HasLen, 1)
		c.Assert(req.Actions[0], DeepEquals, map[string]interface{}{
			"action":       "install",
			"instance-key": "install-1",
			"name":         "foo-2",
			"epoch":        nil,
		})

		io.WriteString(w, `{
  "results": [{
     "result": "install",
     "instance-key": "install-1",
     "snap-id": "foo-2-id",
     "name": "foo-2",
     "snap": {
       "snap-id": "foo-2-id",
       "name": "foo-2",
       "revision": 28,
       "version": "6.1",
       "publisher": {
          "id": "canonical",
          "username": "canonical",
          "display-name": "Canonical"
       }
     }
  },{
      "error": {
        "code": "duplicated-snap",
         "message": "The Snap is present more than once in the request."
      },
      "instance-key": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ:IDKVhLy-HUyfYGFKcsH4V-7FVG7hLGs4M5zsraZU5tk",
      "name": null,
      "result": "error",
      "snap": null,
      "snap-id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
  }]
}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, []*store.CurrentSnap{
		{
			InstanceName:    "hello-world",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(26),
			RefreshedDate:   helloRefreshedDate,
		}, {
			InstanceName:    "hello-world_foo",
			SnapID:          helloWorldSnapID,
			TrackingChannel: "stable",
			Revision:        snap.R(2),
			RefreshedDate:   helloRefreshedDate,
		},
	}, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "foo-2",
		},
	}, nil, nil, &store.RefreshOptions{PrivacyKey: "123"})
	c.Assert(err, DeepEquals, &store.SnapActionError{
		Other: []error{fmt.Errorf(`snap "hello-world_foo": The Snap is present more than once in the request.`)},
	})
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].InstanceName(), Equals, "foo-2")
	c.Assert(results[0].SnapID, Equals, "foo-2-id")
}

func (s *storeActionSuite) TestSnapAction500(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "foo",
		},
	}, nil, nil, nil)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 500 via POST to "http://127\.0\.0\.1:.*/v2/snaps/refresh"`)
	c.Check(err, FitsTypeOf, &store.UnexpectedHTTPStatusError{})
	c.Check(results, HasLen, 0)
}

func (s *storeActionSuite) TestSnapAction400(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		w.WriteHeader(400)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	results, _, err := sto.SnapAction(s.ctx, nil, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "foo",
		},
	}, nil, nil, nil)
	c.Assert(err, ErrorMatches, `cannot query the store for updates: got unexpected HTTP status code 400 via POST to "http://127\.0\.0\.1:.*/v2/snaps/refresh"`)
	c.Check(err, FitsTypeOf, &store.UnexpectedHTTPStatusError{})
	c.Check(results, HasLen, 0)
}

func (s *storeActionSuite) TestSnapActionTimeout(c *C) {
	restore := store.MockRequestTimeout(250 * time.Millisecond)
	defer restore()

	quit := make(chan bool)
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// block the handler, do not send response headers.
		select {
		case <-quit:
		case <-time.After(30 * time.Second):
			// we expect to hit RequestTimeout first
			c.Fatalf("unexpected")
		}
		mockServer.CloseClientConnections()
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	_, _, err := sto.SnapAction(s.ctx, nil, []*store.SnapAction{
		{
			Action:       "install",
			InstanceName: "foo",
		},
	}, nil, nil, nil)
	close(quit)
	// go 1.17 started quoting the failing URL, also context deadline
	// exceeded may appear in place of request being canceled
	c.Assert(err, ErrorMatches, `.*/v2/snaps/refresh"?: (net/http: request canceled|context deadline exceeded)( \(Client.Timeout exceeded while awaiting headers\))?.*`)
}

func (s *storeActionSuite) TestResourceToComponentType(c *C) {
	for _, tc := range []struct {
		resource   string
		error      string
		expCompTyp snap.ComponentType
	}{
		{"foo/bar", "foo/bar is not a component resource", ""},
		{"foobar", "foobar is not a component resource", ""},
		{"component/newtype", "invalid component type \"newtype\"", ""},
		{"component/test", "", snap.TestComponent},
		{"component/kernel-modules", "", snap.KernelModulesComponent},
	} {
		ctyp, err := store.ResourceToComponentType(tc.resource)
		if tc.error != "" {
			c.Check(ctyp, Equals, snap.ComponentType(""))
			c.Check(err.Error(), Equals, tc.error)
		} else {
			c.Check(ctyp, Equals, tc.expCompTyp)
			c.Check(err, IsNil)
		}
	}
}
