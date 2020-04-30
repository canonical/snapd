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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
)

type storeActionFetchAssertionsSuite struct {
	baseStoreSuite
}

var _ = Suite(&storeActionFetchAssertionsSuite{})

type testAssertQuery struct {
	toResolve map[asserts.Grouping][]*asserts.AtRevision
}

func (q *testAssertQuery) ToResolve() (map[asserts.Grouping][]*asserts.AtRevision, error) {
	return q.toResolve, nil
}

func (s *storeActionFetchAssertionsSuite) TestFetch(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`

			AssertionMaxFormats map[string]int `json:"assertion-max-formats"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		expectedAction := map[string]interface{}{
			"action": "fetch-assertions",
			"key":    "g1",
			"assertions": []interface{}{
				map[string]interface{}{
					"type": "snap-declaration",
					"primary-key": []interface{}{
						"16",
						"iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
					},
				},
			},
		}
		c.Assert(req.Actions[0], DeepEquals, expectedAction)

		c.Assert(req.AssertionMaxFormats, DeepEquals, asserts.MaxSupportedFormats(1))

		fmt.Fprintf(w, `{
  "results": [{
     "result": "fetch-assertions",
     "key": "g1",
     "assertion-stream-urls": [
        "https://api.snapcraft.io/api/v1/snaps/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"
      ]
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

	assertq := &testAssertQuery{
		toResolve: map[asserts.Grouping][]*asserts.AtRevision{
			asserts.Grouping("g1"): {{
				Ref: asserts.Ref{
					Type: asserts.SnapDeclarationType,
					PrimaryKey: []string{
						"16",
						"iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
					},
				},
				Revision: asserts.RevisionNotKnown,
			}},
		},
	}

	results, aresults, err := sto.SnapAction(s.ctx, nil,
		nil, assertq, nil, nil)
	c.Assert(err, IsNil)
	c.Check(results, HasLen, 0)
	c.Check(aresults, HasLen, 1)
	c.Check(aresults[0].Grouping, Equals, asserts.Grouping("g1"))
	c.Check(aresults[0].StreamURLs, DeepEquals, []string{
		"https://api.snapcraft.io/api/v1/snaps/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
	})
}

func (s *storeActionFetchAssertionsSuite) TestUpdateIfNewerThan(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "POST", snapActionPath)
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var req struct {
			Context []map[string]interface{} `json:"context"`
			Actions []map[string]interface{} `json:"actions"`

			AssertionMaxFormats map[string]int `json:"assertion-max-formats"`
		}

		err = json.Unmarshal(jsonReq, &req)
		c.Assert(err, IsNil)

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 2)
		expectedAction1 := map[string]interface{}{
			"action": "fetch-assertions",
			"key":    "g1",
			"assertions": []interface{}{
				map[string]interface{}{
					"type": "snap-declaration",
					"primary-key": []interface{}{
						"16",
						"iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
					},
					"if-newer-than": float64(0),
				},
			},
		}
		expectedAction2 := map[string]interface{}{
			"action": "fetch-assertions",
			"key":    "g2",
			"assertions": []interface{}{
				map[string]interface{}{
					"type": "snap-declaration",
					"primary-key": []interface{}{
						"16",
						"CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6",
					},
					"if-newer-than": float64(1),
				},
			},
		}
		expectedActions := []map[string]interface{}{expectedAction1, expectedAction2}
		if req.Actions[0]["key"] != "g1" {
			expectedActions = []map[string]interface{}{expectedAction2, expectedAction1}
		}
		c.Assert(req.Actions, DeepEquals, expectedActions)

		c.Assert(req.AssertionMaxFormats, DeepEquals, asserts.MaxSupportedFormats(1))

		fmt.Fprintf(w, `{
  "results": [{
     "result": "fetch-assertions",
     "key": "g1",
     "assertion-stream-urls": [
        "https://api.snapcraft.io/api/v1/snaps/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"
      ]
     }, {
     "result": "fetch-assertions",
     "key": "g2",
     "assertion-stream-urls": []
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

	assertq := &testAssertQuery{
		toResolve: map[asserts.Grouping][]*asserts.AtRevision{
			asserts.Grouping("g1"): {{
				Ref: asserts.Ref{
					Type: asserts.SnapDeclarationType,
					PrimaryKey: []string{
						"16",
						"iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
					},
				},
				Revision: 0,
			}},
			asserts.Grouping("g2"): {{
				Ref: asserts.Ref{
					Type: asserts.SnapDeclarationType,
					PrimaryKey: []string{
						"16",
						"CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6",
					},
				},
				Revision: 1,
			}},
		},
	}

	results, aresults, err := sto.SnapAction(s.ctx, nil,
		nil, assertq, nil, nil)
	c.Assert(err, IsNil)
	c.Check(results, HasLen, 0)
	c.Check(aresults, HasLen, 2)
	seen := 0
	for _, aresult := range aresults {
		if aresult.Grouping == asserts.Grouping("g1") {
			seen++
			c.Check(aresult.StreamURLs, DeepEquals, []string{
				"https://api.snapcraft.io/api/v1/snaps/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
			})
		} else {
			seen++
			c.Check(aresult.Grouping, Equals, asserts.Grouping("g2"))
			c.Check(aresult.StreamURLs, HasLen, 0)
		}
	}
	c.Check(seen, Equals, 2)
}
