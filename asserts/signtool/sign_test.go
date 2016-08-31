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
	"bytes"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/signtool"
)

func TestSigntool(t *testing.T) { TestingT(t) }

type signSuite struct {
	keypairMgr asserts.KeypairManager
	testKeyID  string
}

var _ = Suite(&signSuite{})

func (s *signSuite) SetUpSuite(c *C) {
	testKey, _ := assertstest.GenerateKey(752)

	s.keypairMgr = asserts.NewMemoryKeypairManager()
	s.keypairMgr.Put(testKey)
	s.testKeyID = testKey.PublicKey().ID()
}

const (
	modelYaml = `type: model
authority-id: user-id1
series: "16"
brand-id: user-id1
model: baz-3000
architecture: amd64
gadget: brand-gadget
kernel: baz-linux
store: brand-store
required-snaps: [foo, bar]
timestamp: 2015-11-25T20:00:00Z
`
)

func expectedModelHeaders(a asserts.Assertion) map[string]interface{} {
	return map[string]interface{}{
		"type":              "model",
		"authority-id":      "user-id1",
		"series":            "16",
		"brand-id":          "user-id1",
		"model":             "baz-3000",
		"architecture":      "amd64",
		"gadget":            "brand-gadget",
		"kernel":            "baz-linux",
		"store":             "brand-store",
		"required-snaps":    []interface{}{"foo", "bar"},
		"timestamp":         "2015-11-25T20:00:00Z",
		"sign-key-sha3-384": a.SignKeyID(),
	}
}

func (s *signSuite) TestSignYAML(c *C) {
	opts := signtool.Options{
		KeyID: s.testKeyID,

		Statement: []byte(modelYaml),
	}

	assertText, err := signtool.Sign(&opts, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	c.Check(a.Revision(), Equals, 0)
	expectedHeaders := expectedModelHeaders(a)
	c.Check(a.Headers(), DeepEquals, expectedHeaders)

	for n, v := range a.Headers() {
		c.Check(v, DeepEquals, expectedHeaders[n], Commentf(n))
	}

	c.Check(a.Body(), IsNil)
}

func (s *signSuite) TestSignYAMLWithBodyAndRevision(c *C) {
	opts := signtool.Options{
		KeyID: s.testKeyID,

		Statement: []byte(modelYaml + `body: "BODY"
revision: "11"`),
	}

	assertText, err := signtool.Sign(&opts, s.keypairMgr)
	c.Assert(err, IsNil)

	a, err := asserts.Decode(assertText)
	c.Assert(err, IsNil)

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

		Statement: []byte(modelYaml),
	}

	tests := []struct {
		expError  string
		breakOpts func(*signtool.Options)
	}{
		{`cannot parse the assertion input as YAML:.*`,
			func(opts *signtool.Options) {
				opts.Statement = []byte("\x00")
			},
		},
		{`invalid assertion type: what`,
			func(opts *signtool.Options) {
				opts.Statement = bytes.Replace(opts.Statement, []byte(": model"), []byte(": what"), 1)
			},
		},
		{`assertion type must be a string, not: \[\]`,
			func(opts *signtool.Options) {
				opts.Statement = bytes.Replace(opts.Statement, []byte(": model"), []byte(": []"), 1)
			},
		},
		{`missing assertion type header`,
			func(opts *signtool.Options) {
				opts.Statement = bytes.Replace(opts.Statement, []byte("type: model\n"), []byte(""), 1)
			},
		},
		{"revision should be positive: -10",
			func(opts *signtool.Options) {
				opts.Statement = append(opts.Statement, `revision: "-10"`...)
			},
		},
		{`"authority-id" header is mandatory`,
			func(opts *signtool.Options) {
				opts.Statement = bytes.Replace(opts.Statement, []byte("authority-id: user-id1\n"), []byte(""), 1)

			},
		},
		{`body if specified must be a string`,
			func(opts *signtool.Options) {
				opts.Statement = append(opts.Statement, `body: []`...)
			},
		},
	}

	for _, t := range tests {
		fresh := opts

		t.breakOpts(&fresh)

		_, err := signtool.Sign(&fresh, s.keypairMgr)
		c.Check(err, ErrorMatches, t.expError)
	}
}
