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
	toResolve    map[asserts.Grouping][]*asserts.AtRevision
	toResolveSeq map[asserts.Grouping][]*asserts.AtSequence
	errors       map[string]error
}

func (q *testAssertQuery) ToResolve() (map[asserts.Grouping][]*asserts.AtRevision, map[asserts.Grouping][]*asserts.AtSequence, error) {
	return q.toResolve, q.toResolveSeq, nil
}

func (q *testAssertQuery) addError(e error, u string) {
	if q.errors == nil {
		q.errors = make(map[string]error)
	}
	q.errors[u] = e
}

func (q *testAssertQuery) AddError(e error, ref *asserts.Ref) error {
	q.addError(e, ref.Unique())
	return nil
}

func (q *testAssertQuery) AddSequenceError(e error, atSeq *asserts.AtSequence) error {
	q.addError(e, atSeq.Unique())
	return nil
}

func (q *testAssertQuery) AddGroupingError(e error, grouping asserts.Grouping) error {
	q.addError(e, fmt.Sprintf("{%s}", grouping))
	return nil
}

func (s *storeActionFetchAssertionsSuite) testFetch(c *C, assertionMaxFormats map[string]int) {
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

		if assertionMaxFormats != nil {
			c.Assert(req.AssertionMaxFormats, DeepEquals, assertionMaxFormats)
		} else {
			c.Assert(req.AssertionMaxFormats, DeepEquals, asserts.MaxSupportedFormats(1))
		}

		fmt.Fprintf(w, `{
  "results": [{
     "result": "fetch-assertions",
     "key": "g1",
     "assertion-stream-urls": [
        "https://api.snapcraft.io/v2/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"
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

	if assertionMaxFormats != nil {
		sto.SetAssertionMaxFormats(assertionMaxFormats)
	}

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
		"https://api.snapcraft.io/v2/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
	})
}

func (s *storeActionFetchAssertionsSuite) TestFetch(c *C) {
	s.testFetch(c, nil)
}

func (s *storeActionFetchAssertionsSuite) TestFetchSetAssertionMaxFormats(c *C) {
	s.testFetch(c, map[string]int{
		"snap-declaration": 7,
	})
}

func (s *storeActionFetchAssertionsSuite) TestUpdateIfNewerThan(c *C) {
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
        "https://api.snapcraft.io/v2/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"
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
				"https://api.snapcraft.io/v2/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
			})
		} else {
			seen++
			c.Check(aresult.Grouping, Equals, asserts.Grouping("g2"))
			c.Check(aresult.StreamURLs, HasLen, 0)
		}
	}
	c.Check(seen, Equals, 2)
}

func (s *storeActionFetchAssertionsSuite) TestFetchNotFound(c *C) {
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
						"xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
					},
				},
			},
		}
		c.Assert(req.Actions[0], DeepEquals, expectedAction)

		fmt.Fprintf(w, `{
  "results": [{
     "result": "fetch-assertions",
     "key": "g1",
     "assertion-stream-urls": [],
     "error-list": [
        {
          "code": "not-found",
          "message": "not found: no assertion with type \"snap-declaration\" and key {\"series\":\"16\",\"snap-id\":\"xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5\"}",
          "primary-key": [
            "16",
            "xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"
          ],
          "type": "snap-declaration"
        }
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
						"xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
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
	c.Check(aresults, HasLen, 0)

	c.Check(assertq.errors, DeepEquals, map[string]error{
		"snap-declaration/16/xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5": &asserts.NotFoundError{
			Type:    asserts.SnapDeclarationType,
			Headers: map[string]string{"series": "16", "snap-id": "xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"}},
	})
}

func (s *storeActionFetchAssertionsSuite) TestFetchValidationSetNotFound(c *C) {
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

		c.Assert(req.Context, HasLen, 0)
		c.Assert(req.Actions, HasLen, 1)
		expectedAction := map[string]interface{}{
			"action": "fetch-assertions",
			"key":    "g1",
			"assertions": []interface{}{
				map[string]interface{}{
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"foo",
						"bar",
					},
				},
			},
		}
		c.Assert(req.Actions[0], DeepEquals, expectedAction)

		fmt.Fprintf(w, `{
  "results": [{
     "result": "fetch-assertions",
     "key": "g1",
     "assertion-stream-urls": [],
     "error-list": [
		{
			"code": "not-found",
			"message": "not found: no assertion with type \"validation-set\" and sequence key {\"account-id\":\"foo\",\"name\":\"bar\",\"series\":\"16\"}",
			"sequence-key": [
			  "16",
			  "foo",
			  "bar"
			],
			"type": "validation-set"
		}
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
		toResolveSeq: map[asserts.Grouping][]*asserts.AtSequence{
			asserts.Grouping("g1"): {
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"foo",
						"bar",
					},
					Revision: asserts.RevisionNotKnown,
				},
			},
		},
	}

	results, aresults, err := sto.SnapAction(s.ctx, nil,
		nil, assertq, nil, nil)
	c.Assert(err, IsNil)
	c.Check(results, HasLen, 0)
	c.Check(aresults, HasLen, 0)

	c.Check(assertq.errors, DeepEquals, map[string]error{
		"validation-set/16/foo/bar": &asserts.NotFoundError{
			Type:    asserts.ValidationSetType,
			Headers: map[string]string{"series": "16", "account-id": "foo", "name": "bar"}},
	})
}

func (s *storeActionFetchAssertionsSuite) TestReportFetchAssertionsError(c *C) {
	notFound := store.ErrorListEntryJSON{
		Code: "not-found",
		Type: "snap-declaration",
		PrimaryKey: []string{
			"16",
			"xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
		},
		Message: `not found: no assertion with type "snap-declaration" and key {"series":"16","snap-id":"xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"}`,
	}
	invalidRequest := store.ErrorListEntryJSON{
		Code:       "invalid-request",
		Type:       "snap-declaration",
		PrimaryKey: []string{},
		Message:    `invalid request: invalid key, should be "{series}/{snap-id}"`,
	}
	// not a realistic error, but for completeness
	otherRefError := store.ErrorListEntryJSON{
		Code: "other-ref-error",
		Type: "snap-declaration",
		PrimaryKey: []string{
			"16",
			"xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
		},
		Message: "other ref error",
	}
	otherSeqKeyError := store.ErrorListEntryJSON{
		Code: "other-seq-error",
		Type: "validation-set",
		SequenceKey: []string{
			"16",
			"foo",
			"bar",
		},
		Message: "other sequence key error",
	}

	tests := []struct {
		errorList []store.ErrorListEntryJSON
		errkey    string
		err       string
	}{
		{[]store.ErrorListEntryJSON{notFound}, "snap-declaration/16/xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5", "snap-declaration.*not found"},
		{[]store.ErrorListEntryJSON{otherRefError}, "snap-declaration/16/xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5", "other ref error"},
		{[]store.ErrorListEntryJSON{otherRefError, notFound}, "snap-declaration/16/xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5", "other ref error"},
		{[]store.ErrorListEntryJSON{notFound, otherRefError}, "snap-declaration/16/xEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5", "other ref error"},
		{[]store.ErrorListEntryJSON{otherSeqKeyError}, "validation-set/16/foo/bar", "other sequence key error"},
		{[]store.ErrorListEntryJSON{notFound, otherSeqKeyError}, "validation-set/16/foo/bar", "other sequence key error"},
		{[]store.ErrorListEntryJSON{invalidRequest}, "{g1}", "invalid request: invalid key.*"},
		{[]store.ErrorListEntryJSON{invalidRequest, otherRefError}, "{g1}", "invalid request: invalid key.*"},
		{[]store.ErrorListEntryJSON{invalidRequest, notFound}, "{g1}", "invalid request: invalid key.*"},
		{[]store.ErrorListEntryJSON{notFound, invalidRequest, otherRefError}, "{g1}", "invalid request: invalid key.*"},
	}

	for _, t := range tests {
		assertq := &testAssertQuery{}

		res := &store.SnapActionResultJSON{
			Key:       "g1",
			ErrorList: t.errorList,
		}

		err := store.ReportFetchAssertionsError(res, assertq)
		c.Assert(err, IsNil)

		c.Check(assertq.errors, HasLen, 1)
		for k, e := range assertq.errors {
			c.Check(k, Equals, t.errkey)
			c.Check(e, ErrorMatches, t.err)
		}
	}
}

func (s *storeActionFetchAssertionsSuite) TestUpdateSequenceForming(c *C) {
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
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"account-1/name-1",
					},
					"sequence": float64(3),
				},
				map[string]interface{}{
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"account-1/name-2",
					},
					"if-sequence-equal-or-newer-than": float64(5),
					"if-newer-than":                   float64(10),
				},
			},
		}
		expectedAction2 := map[string]interface{}{
			"action": "fetch-assertions",
			"key":    "g2",
			"assertions": []interface{}{
				map[string]interface{}{
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"account-2/name",
					},
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
        "https://api.snapcraft.io/v2/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5"
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
		toResolveSeq: map[asserts.Grouping][]*asserts.AtSequence{
			asserts.Grouping("g1"): {
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"account-1/name-1",
					},
					Sequence: 3,
					Pinned:   true,
					Revision: asserts.RevisionNotKnown,
				},
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"account-1/name-2",
					},
					Sequence: 5,
					Revision: 10,
				},
			},
			asserts.Grouping("g2"): {
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"account-2/name",
					},
					Revision: asserts.RevisionNotKnown,
				},
			},
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
				"https://api.snapcraft.io/v2/assertions/snap-declaration/16/iEr2EpvaIaqrXxoM2JyHOmuXQYvSzUt5",
			})
		} else {
			seen++
			c.Check(aresult.Grouping, Equals, asserts.Grouping("g2"))
			c.Check(aresult.StreamURLs, HasLen, 0)
		}
	}
	c.Check(seen, Equals, 2)
}

func (s *storeActionFetchAssertionsSuite) TestUpdateSequenceFormingCommonGroupings(c *C) {
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
				map[string]interface{}{
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"account-1/name-1",
					},
					"sequence": float64(3),
				},
				map[string]interface{}{
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"account-1/name-2",
					},
					"if-sequence-equal-or-newer-than": float64(5),
					"if-newer-than":                   float64(10),
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
				map[string]interface{}{
					"type": "validation-set",
					"sequence-key": []interface{}{
						"16",
						"account-2/name",
					},
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
     "result": "fetch-assertions", "key": "g1", "assertion-stream-urls": []
     }, {
     "result": "fetch-assertions", "key": "g2", "assertion-stream-urls": []
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
		toResolveSeq: map[asserts.Grouping][]*asserts.AtSequence{
			asserts.Grouping("g1"): {
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"account-1/name-1",
					},
					Sequence: 3,
					Pinned:   true,
					Revision: asserts.RevisionNotKnown,
				},
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"account-1/name-2",
					},
					Sequence: 5,
					Revision: 10,
				},
			},
			asserts.Grouping("g2"): {
				&asserts.AtSequence{
					Type: asserts.ValidationSetType,
					SequenceKey: []string{
						"16",
						"account-2/name",
					},
					Revision: asserts.RevisionNotKnown,
				},
			},
		},
	}

	_, _, err := sto.SnapAction(s.ctx, nil, nil, assertq, nil, nil)
	c.Assert(err, IsNil)
}

func (s *storeActionFetchAssertionsSuite) TestFetchOptionalPrimaryKeys(c *C) {
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
					"type": "snap-revision",
					"primary-key": []interface{}{
						"QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL",
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
        "https://api.snapcraft.io/v2/assertions/snap-revision/QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL/global-upload"
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
					Type: asserts.SnapRevisionType,
					PrimaryKey: []string{
						"QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL",
						"global-upload",
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
		"https://api.snapcraft.io/v2/assertions/snap-revision/QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL/global-upload",
	})
}
