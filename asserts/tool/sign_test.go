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

package tool_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/tool"
)

func TestTool(t *testing.T) { TestingT(t) }

type signSuite struct {
	keypairMgr asserts.KeypairManager
	testKeyID  string

	accKey []byte
}

var _ = Suite(&signSuite{})

func (s *signSuite) SetUpSuite(c *C) {
	testKey, err := asserts.GenerateKey()
	c.Assert(err, IsNil)

	s.keypairMgr = asserts.NewMemoryKeypairManager()
	s.keypairMgr.Put("user-id1", testKey)
	s.testKeyID = testKey.PublicKey().ID()

	pubKeyEncoded, err := asserts.EncodePublicKey(testKey.PublicKey())
	c.Assert(err, IsNil)

	now := time.Now()
	// good enough as a handle as is used by Sign
	mockAccKey := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: user-id1\n" +
		"public-key-id: " + s.testKeyID + "\n" +
		"public-key-fingerprint: " + testKey.PublicKey().Fingerprint() + "\n" +
		"since: " + now.Format(time.RFC3339) + "\n" +
		"until: " + now.AddDate(1, 0, 0).Format(time.RFC3339) + "\n" +
		fmt.Sprintf("body-length: %v", len(pubKeyEncoded)) + "\n\n" +
		string(pubKeyEncoded) + "\n\n" +
		"openpgp c2ln"

	s.accKey = []byte(mockAccKey)
}

const (
	flatModelYaml = `series: "16"
brand-id: user-id1
model: baz-3000
os: core
architecture: amd64
gadget: brand-gadget
kernel: baz-linux
store: brand-store
allowed-modes:
required-snaps: [foo,bar]
class: fixed
extra-flag: true
timestamp: 2015-11-25T20:00:00Z
`
	nestedModelYaml = `headers:
  series: "16"
  brand-id: user-id1
  model: baz-3000
  os: core
  architecture: amd64
  gadget: brand-gadget
  kernel: baz-linux
  store: brand-store
  allowed-modes:
  required-snaps: [foo,bar]
  class: fixed
  extra-flag: true
  timestamp: 2015-11-25T20:00:00Z
`
)

func expectedModelHeaders() map[string]string {
	return map[string]string{
		"type":           "model",
		"authority-id":   "user-id1",
		"series":         "16",
		"brand-id":       "user-id1",
		"model":          "baz-3000",
		"allowed-modes":  "",
		"architecture":   "amd64",
		"class":          "fixed",
		"extra-flag":     "yes",
		"gadget":         "brand-gadget",
		"kernel":         "baz-linux",
		"os":             "core",
		"store":          "brand-store",
		"required-snaps": "foo,bar",
		"timestamp":      "2015-11-25T20:00:00Z",
	}
}

func (s *signSuite) TestSignKeyIDFlatYAML(c *C) {
	req := tool.SignRequest{
		KeyID:       s.testKeyID,
		AuthorityID: "user-id1",

		AssertionType:      "model",
		StatementMediaType: "application/x-yaml",
		Statement:          []byte(flatModelYaml),
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Headers(), DeepEquals, expectedModelHeaders())
	c.Check(a.Body(), IsNil)
}

func (s *signSuite) TestSignKeyIDNestedYAML(c *C) {
	req := tool.SignRequest{
		KeyID:       s.testKeyID,
		AuthorityID: "user-id1",

		AssertionType:      "model",
		StatementMediaType: "application/x-yaml",
		Statement:          []byte(nestedModelYaml),
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Headers(), DeepEquals, expectedModelHeaders())
	c.Check(a.Body(), IsNil)
}

func (s *signSuite) TestSignKeyIDNestedYAMLWithBodyAndRevision(c *C) {
	req := tool.SignRequest{
		KeyID:       s.testKeyID,
		AuthorityID: "user-id1",

		AssertionType:      "model",
		StatementMediaType: "application/x-yaml",
		Statement:          []byte(nestedModelYaml + `content-body: "CONTENT"`),

		Revision: 11,
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 11)

	expectedHeaders := expectedModelHeaders()
	expectedHeaders["revision"] = "11"
	expectedHeaders["body-length"] = "7"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), DeepEquals, []byte("CONTENT"))
}

func (s *signSuite) TestSignKeyIDFlatYAMLRevisionWithinHeaders(c *C) {
	req := tool.SignRequest{
		KeyID:       s.testKeyID,
		AuthorityID: "user-id1",

		AssertionType:      "model",
		StatementMediaType: "application/x-yaml",
		Statement:          []byte(flatModelYaml + "revision: 12"),
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 12)

	expectedHeaders := expectedModelHeaders()
	expectedHeaders["revision"] = "12"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), IsNil)
}

func headersForJSON() map[string]interface{} {
	return map[string]interface{}{
		"series":         "16",
		"brand-id":       "user-id1",
		"model":          "baz-3000",
		"os":             "core",
		"architecture":   "amd64",
		"gadget":         "brand-gadget",
		"kernel":         "baz-linux",
		"store":          "brand-store",
		"allowed-modes":  nil,
		"required-snaps": []string{"foo", "bar"},
		"class":          "fixed",
		"extra-flag":     true,
		"timestamp":      "2015-11-25T20:00:00Z",
	}
}

func (s *signSuite) TestSignKeyIDFlatJSONRevisionWithinHeaders(c *C) {
	hdrs := headersForJSON()
	hdrs["revision"] = 12
	statement, err := json.Marshal(hdrs)
	c.Assert(err, IsNil)

	req := tool.SignRequest{
		KeyID:       s.testKeyID,
		AuthorityID: "user-id1",

		AssertionType:      "model",
		StatementMediaType: "application/json",
		Statement:          statement,
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 12)

	expectedHeaders := expectedModelHeaders()
	expectedHeaders["revision"] = "12"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), IsNil)
}

func (s *signSuite) TestSignKeyIDNestedJSONWithBodyAndRevision(c *C) {
	hdrs := headersForJSON()
	statement, err := json.Marshal(map[string]interface{}{
		"headers":      hdrs,
		"content-body": "CONTENT",
	})
	c.Assert(err, IsNil)

	req := tool.SignRequest{
		KeyID:       s.testKeyID,
		AuthorityID: "user-id1",

		AssertionType:      "model",
		StatementMediaType: "application/json",
		Statement:          statement,

		Revision: 11,
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 11)

	expectedHeaders := expectedModelHeaders()
	expectedHeaders["revision"] = "11"
	expectedHeaders["body-length"] = "7"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), DeepEquals, []byte("CONTENT"))
}

func (s *signSuite) TestSignAccountKeyHandleNestedYAML(c *C) {
	req := tool.SignRequest{
		AccountKey: s.accKey,

		AssertionType:      "model",
		StatementMediaType: "application/x-yaml",
		Statement:          []byte(nestedModelYaml),
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Headers(), DeepEquals, expectedModelHeaders())
	c.Check(a.Body(), IsNil)
}
