// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package signtool_test

import (
	"encoding/json"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/signtool"
)

func TestSigntool(t *testing.T) { TestingT(t) }

type signSuite struct {
	keypairMgr asserts.KeypairManager
	testKeyID  string
	testAccKey *asserts.AccountKey
}

var _ = Suite(&signSuite{})

func (s *signSuite) SetUpSuite(c *C) {
	testKey, _ := assertstest.GenerateKey(752)

	s.keypairMgr = asserts.NewMemoryKeypairManager()
	s.keypairMgr.Put(testKey)
	s.testKeyID = testKey.PublicKey().ID()

	encPubKey := mylog.Check2(asserts.EncodePublicKey(testKey.PublicKey()))

	s.testAccKey = assertstest.FakeAssertionWithBody(encPubKey,
		map[string]interface{}{
			"type":                "account-key",
			"authority-id":        "canonical",
			"public-key-sha3-384": s.testKeyID,
			"account-id":          "user-id1",
			"since":               "2015-11-01T20:00:00Z",
			"body-length":         strconv.Itoa(len(encPubKey)),
			"constraints": []interface{}{
				map[string]interface{}{
					"headers": map[string]interface{}{
						"type":  "model",
						"model": `baz-.*`,
					},
				},
			},
		},
	).(*asserts.AccountKey)
}

func expectedModelHeaders(a asserts.Assertion) map[string]interface{} {
	m := map[string]interface{}{
		"type":           "model",
		"authority-id":   "user-id1",
		"series":         "16",
		"brand-id":       "user-id1",
		"model":          "baz-3000",
		"architecture":   "amd64",
		"gadget":         "brand-gadget",
		"kernel":         "baz-linux",
		"store":          "brand-store",
		"required-snaps": []interface{}{"foo", "bar"},
		"timestamp":      "2015-11-25T20:00:00Z",
	}
	if a != nil {
		m["sign-key-sha3-384"] = a.SignKeyID()
	}
	return m
}

func exampleJSON(overrides map[string]interface{}) []byte {
	m := expectedModelHeaders(nil)
	for k, v := range overrides {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b := mylog.Check2(json.Marshal(m))

	return b
}

func (s *signSuite) TestSignJSON(c *C) {
	opts := signtool.Options{
		KeyID: s.testKeyID,

		Statement: exampleJSON(nil),
	}

	assertText := mylog.Check2(signtool.Sign(&opts, s.keypairMgr))


	a := mylog.Check2(asserts.Decode(assertText))


	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 0)
	expectedHeaders := expectedModelHeaders(a)
	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	for n, v := range a.Headers() {
		c.Check(v, DeepEquals, expectedHeaders[n], Commentf(n))
	}

	c.Check(a.Body(), IsNil)
}

func (s *signSuite) TestSignJSONWithAccountKeyCrossCheck(c *C) {
	opts := signtool.Options{
		KeyID:      s.testKeyID,
		AccountKey: s.testAccKey,

		Statement: exampleJSON(nil),
	}

	assertText := mylog.Check2(signtool.Sign(&opts, s.keypairMgr))


	a := mylog.Check2(asserts.Decode(assertText))


	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 0)
	expectedHeaders := expectedModelHeaders(a)
	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	for n, v := range a.Headers() {
		c.Check(v, DeepEquals, expectedHeaders[n], Commentf(n))
	}

	c.Check(a.Body(), IsNil)
}

func (s *signSuite) TestSignJSONWithBodyAndRevision(c *C) {
	statement := exampleJSON(map[string]interface{}{
		"body":     "BODY",
		"revision": "11",
	})
	opts := signtool.Options{
		KeyID: s.testKeyID,

		Statement: statement,
	}

	assertText := mylog.Check2(signtool.Sign(&opts, s.keypairMgr))


	a := mylog.Check2(asserts.Decode(assertText))


	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 11)

	expectedHeaders := expectedModelHeaders(a)
	expectedHeaders["revision"] = "11"
	expectedHeaders["body-length"] = "4"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), DeepEquals, []byte("BODY"))
}

func (s *signSuite) TestSignJSONWithBodyAndComplementRevision(c *C) {
	statement := exampleJSON(map[string]interface{}{
		"body": "BODY",
	})
	opts := signtool.Options{
		KeyID: s.testKeyID,

		Statement: statement,
		Complement: map[string]interface{}{
			"revision": "11",
		},
	}

	assertText := mylog.Check2(signtool.Sign(&opts, s.keypairMgr))


	a := mylog.Check2(asserts.Decode(assertText))


	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 11)

	expectedHeaders := expectedModelHeaders(a)
	expectedHeaders["revision"] = "11"
	expectedHeaders["body-length"] = "4"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), DeepEquals, []byte("BODY"))
}

func (s *signSuite) TestSignJSONWithRevisionAndComplementBodyAndRepeatedType(c *C) {
	statement := exampleJSON(map[string]interface{}{
		"revision": "11",
	})
	opts := signtool.Options{
		KeyID: s.testKeyID,

		Statement: statement,
		Complement: map[string]interface{}{
			"type": "model",
			"body": "BODY",
		},
	}

	assertText := mylog.Check2(signtool.Sign(&opts, s.keypairMgr))


	a := mylog.Check2(asserts.Decode(assertText))


	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 11)

	expectedHeaders := expectedModelHeaders(a)
	expectedHeaders["revision"] = "11"
	expectedHeaders["body-length"] = "4"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), DeepEquals, []byte("BODY"))
}

func (s *signSuite) TestSignErrors(c *C) {
	opts := signtool.Options{
		KeyID: s.testKeyID,
	}

	emptyList := []interface{}{}

	tests := []struct {
		expError        string
		brokenStatement []byte
		complement      map[string]interface{}
		accKey          *asserts.AccountKey
	}{
		{
			`cannot parse the assertion input as JSON:.*`,
			[]byte("\x00"),
			nil, nil,
		},
		{
			`invalid assertion type: what`,
			exampleJSON(map[string]interface{}{"type": "what"}),
			nil, nil,
		},
		{
			`assertion type must be a string, not: \[\]`,
			exampleJSON(map[string]interface{}{"type": emptyList}),
			nil, nil,
		},
		{
			`missing assertion type header`,
			exampleJSON(map[string]interface{}{"type": nil}),
			nil, nil,
		},
		{
			"revision should be positive: -10",
			exampleJSON(map[string]interface{}{"revision": "-10"}),
			nil, nil,
		},
		{
			`"authority-id" header is mandatory`,
			exampleJSON(map[string]interface{}{"authority-id": nil}),
			nil, nil,
		},
		{
			`body if specified must be a string`,
			exampleJSON(map[string]interface{}{"body": emptyList}),
			nil, nil,
		},
		{
			`repeated assertion type does not match`,
			exampleJSON(nil),
			map[string]interface{}{"type": "foo"},
			nil,
		},
		{
			`complementary header "kernel" clashes with assertion input`,
			exampleJSON(nil),
			map[string]interface{}{"kernel": "foo"},
			nil,
		},
		{
			`authority-id does not match the account-id of the signing account-key`,
			exampleJSON(map[string]interface{}{"authority-id": "user-id2", "brand-id": "user-id2"}),
			nil, s.testAccKey,
		},
		{
			`the assertion headers do not match the constraints of the signing account-key`,
			exampleJSON(map[string]interface{}{"model": "bar"}),
			nil, s.testAccKey,
		},
	}

	for _, t := range tests {
		fresh := opts

		fresh.Statement = t.brokenStatement
		fresh.Complement = t.complement
		fresh.AccountKey = t.accKey

		_ := mylog.Check2(signtool.Sign(&fresh, s.keypairMgr))
		c.Check(err, ErrorMatches, t.expError)
	}
}
