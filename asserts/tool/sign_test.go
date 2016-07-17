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
	"bytes"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/tool"
)

func TestTool(t *testing.T) { TestingT(t) }

type signSuite struct {
	keypairMgr asserts.KeypairManager
	testKeyID  string

	accKey      []byte
	otherAssert []byte
}

var _ = Suite(&signSuite{})

func (s *signSuite) SetUpSuite(c *C) {
	testKey, _ := assertstest.GenerateKey(752)

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

	s.otherAssert = []byte("type: account\n" +
		"authority-id: canonical\n" +
		"account-id: user-id1\n" +
		"display-name: User One\n" +
		"username: userone\n" +
		"validation: unproven\n" +
		"timestamp: " + now.Format(time.RFC3339) + "\n\n" +
		"openpgp c2ln")
}

const (
	modelYaml = `type: model
authority-id: user-id1
series: "16"
brand-id: user-id1
model: baz-3000
core: core
architecture: amd64
gadget: brand-gadget
kernel: baz-linux
store: brand-store
allowed-modes:
required-snaps: "foo,bar"
class: fixed
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
		"gadget":         "brand-gadget",
		"kernel":         "baz-linux",
		"core":           "core",
		"store":          "brand-store",
		"required-snaps": "foo,bar",
		"timestamp":      "2015-11-25T20:00:00Z",
	}
}

func (s *signSuite) TestSignKeyIDYAML(c *C) {
	req := tool.SignRequest{
		KeyID: s.testKeyID,

		Statement: []byte(modelYaml),
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

func (s *signSuite) TestSignKeyIDYAMLWithBodyAndRevision(c *C) {
	req := tool.SignRequest{
		KeyID: s.testKeyID,

		Statement: []byte(modelYaml + `body: "BODY"
revision: "11"`),
	}

	assertText, err := tool.Sign(&req, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 11)

	expectedHeaders := expectedModelHeaders()
	expectedHeaders["revision"] = "11"
	expectedHeaders["body-length"] = "4"

	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	c.Check(a.Body(), DeepEquals, []byte("BODY"))
}

func (s *signSuite) TestSignAccountKeyHandle(c *C) {
	req := tool.SignRequest{
		AccountKey: s.accKey,

		Statement: []byte(modelYaml),
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

func (s *signSuite) TestSignErrors(c *C) {
	req := tool.SignRequest{
		KeyID: s.testKeyID,

		Statement: []byte(modelYaml),
	}

	tests := []struct {
		expError string
		breakReq func(*tool.SignRequest)
	}{
		{`cannot parse the assertion input as YAML:.*`,
			func(req *tool.SignRequest) {
				req.Statement = []byte("\x00")
			},
		},
		{`invalid assertion type: "what"`,
			func(req *tool.SignRequest) {
				req.Statement = bytes.Replace(req.Statement, []byte(": model"), []byte(": what"), 1)
			},
		},
		{"revision should be positive: -10",
			func(req *tool.SignRequest) {
				req.Statement = append(req.Statement, "revision: -10"...)
			},
		},
		{"both account-key and key id were not specified",
			func(req *tool.SignRequest) {
				req.KeyID = ""
				req.AccountKey = nil
			},
		},
		{"cannot specify both an account-key together with a key id",
			func(req *tool.SignRequest) {
				req.AccountKey = []byte("ak")
			},
		},
		{"cannot parse handle account-key:.*",
			func(req *tool.SignRequest) {
				req.KeyID = ""
				req.AccountKey = []byte("ak")
			},
		},
		{`account-key owner "user-id1" does not match assertion input authority-id: "user-idX"`,
			func(req *tool.SignRequest) {
				req.KeyID = ""
				req.AccountKey = s.accKey
				req.Statement = bytes.Replace(req.Statement, []byte("authority-id: user-id1\n"), []byte("authority-id: user-idX\n"), 1)
			},
		},
		{"cannot use handle account-key, not actually an account-key, got: account",
			func(req *tool.SignRequest) {
				req.KeyID = ""
				req.AccountKey = s.otherAssert
			},
		},
		{`"authority-id" header is mandatory`,
			func(req *tool.SignRequest) {
				req.Statement = bytes.Replace(req.Statement, []byte("authority-id: user-id1\n"), []byte(""), 1)

			},
		},
	}

	for _, t := range tests {
		fresh := req

		t.breakReq(&fresh)

		_, err := tool.Sign(&fresh, s.keypairMgr)
		c.Check(err, ErrorMatches, t.expError)
	}
}
