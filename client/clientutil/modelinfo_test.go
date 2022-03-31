// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package clientutil_test

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
)

const (
	modelExample = "type: model\n" +
		"authority-id: brand-id1\n" +
		"series: 16\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"display-name: Baz 3000\n" +
		"architecture: amd64\n" +
		"gadget: brand-gadget\n" +
		"base: core18\n" +
		"kernel: baz-linux\n" +
		"store: brand-store\n" +
		"serial-authority:\n  - generic\n" +
		"system-user-authority: *\n" +
		"required-snaps:\n  - foo\n  - bar\n" +
		"TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
)

type testFormatter struct {
	publisher string
}

func (tf testFormatter) GetEscapedDash() string {
	return "--"
}

func (tf testFormatter) GetPublisher() string {
	return tf.publisher
}

type modelInfoSuite struct {
	testutil.BaseTest
	ts        time.Time
	tsLine    string
	formatter testFormatter
}

var _ = Suite(&modelInfoSuite{})

func (s *modelInfoSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
	s.formatter.publisher = "brand-id1"
}

func (s *modelInfoSuite) getModel(c *C) *asserts.Model {
	encoded := strings.Replace(modelExample, "TSLINE", s.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	return model
}

func (s *modelInfoSuite) TestPrintModelYAML(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c)

	err := clientutil.PrintModelAssertionYAML(w, s.formatter, 80, false, false, false, false, model, nil)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, `brand     brand-id1
model     Baz 3000 (baz-3000)
serial    - (device not registered yet)
`)
}

func (s *modelInfoSuite) TestPrintModelYAMLVerbose(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c)

	err := clientutil.PrintModelAssertionYAML(w, s.formatter, 80, false, false, true, false, model, nil)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`brand-id:                 brand-id1
model:                    baz-3000
serial:                   -- (device not registered yet)
architecture:             amd64
base:                     core18
display-name:             Baz 3000
gadget:                   brand-gadget
kernel:                   baz-linux
store:                    brand-store
system-user-authority:    *
timestamp:                %s
required-snaps:           
  - foo
  - bar
`, timeutil.Human(s.ts)))
}

func (s *modelInfoSuite) TestPrintModelYAMLAssertion(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c)

	err := clientutil.PrintModelAssertionYAML(w, s.formatter, 80, false, false, false, true, model, nil)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`type: model
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
display-name: Baz 3000
architecture: amd64
gadget: brand-gadget
base: core18
kernel: baz-linux
store: brand-store
serial-authority:
  - generic
system-user-authority: *
required-snaps:
  - foo
  - bar
timestamp: %s
body-length: 0
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

`, s.ts.Format(time.RFC3339)))
}

func (s *modelInfoSuite) TestPrintModelJSON(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c)

	err := clientutil.PrintModelAssertionJSON(w, s.formatter, false, false, false, model, nil)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`{
  "architecture": "amd64",
  "base": "core18",
  "brand-id": "brand-id1",
  "display-name": "Baz 3000",
  "gadget": "brand-gadget",
  "kernel": "baz-linux",
  "model": "baz-3000",
  "required-snaps": [
    "foo",
    "bar"
  ],
  "serial": "-- (device not registered yet)",
  "store": "brand-store",
  "system-user-authority": "*",
  "timestamp": "%s"
}`, timeutil.Human(s.ts)))
}

func (s *modelInfoSuite) TestPrintModelJSONAssertion(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c)

	err := clientutil.PrintModelAssertionJSON(w, s.formatter, false, false, true, model, nil)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`{
  "headers": {
    "architecture": "amd64",
    "authority-id": "brand-id1",
    "base": "core18",
    "body-length": "0",
    "brand-id": "brand-id1",
    "display-name": "Baz 3000",
    "gadget": "brand-gadget",
    "kernel": "baz-linux",
    "model": "baz-3000",
    "required-snaps": [
      "foo",
      "bar"
    ],
    "serial-authority": [
      "generic"
    ],
    "series": "16",
    "sign-key-sha3-384": "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
    "store": "brand-store",
    "system-user-authority": "*",
    "timestamp": "%s",
    "type": "model"
  }
}`, s.ts.Format(time.RFC3339)))
}
