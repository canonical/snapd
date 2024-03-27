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
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/strutil"
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

	core20ModelExample = `type: model
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
display-name: Baz 3000
architecture: amd64
system-user-authority:
  - partner
  - brand-id1
base: core20
store: brand-store
grade: dangerous
storage-safety: prefer-unencrypted
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20
  -
    name: brand-gadget
    id: brandgadgetdidididididididididid
    type: gadget
  -
    name: other-base
    id: otherbasedididididididididididid
    type: base
    modes:
      - run
    presence: required
  -
    name: nm
    id: nmididididididididididididididid
    modes:
      - ephemeral
      - run
    default-channel: 1.0
  -
    name: myapp
    id: myappdididididididididididididid
    type: app
    default-channel: 2.0
  -
    name: myappopt
    id: myappoptidididididididididididid
    type: app
    presence: optional
    grade: secured
    storage-safety: encrypted
` + "TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	serialExample = "type: serial\n" +
		"authority-id: brand-id1\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"serial: 2700\n" +
		"device-key:\n    DEVICEKEY\n" +
		"device-key-sha3-384: KEYID\n" +
		"TSLINE" +
		"body-length: 2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"HW" +
		"\n\n" +
		"AXNpZw=="
)

type testFormatter struct {
}

func (tf testFormatter) GetEscapedDash() string {
	return "--"
}

func (tf testFormatter) LongPublisher(storeAccountID string) string {
	return storeAccountID
}

type modelInfoSuite struct {
	testutil.BaseTest
	ts            time.Time
	tsLine        string
	deviceKey     asserts.PrivateKey
	encodedDevKey string
	formatter     testFormatter
}

var testPrivKey2, _ = assertstest.GenerateKey(752)

var _ = Suite(&modelInfoSuite{})

func (s *modelInfoSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"

	s.deviceKey = testPrivKey2
	encodedPubKey, err := asserts.EncodePublicKey(s.deviceKey.PublicKey())
	c.Assert(err, IsNil)
	s.encodedDevKey = string(encodedPubKey)
}

func (s *modelInfoSuite) getModel(c *C, modelText string) asserts.Model {
	encoded := strings.Replace(modelText, "TSLINE", s.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	return *model
}

func (s *modelInfoSuite) getDeviceKey(padding string, termWidth int) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	key := strings.Join(strings.Split(s.encodedDevKey, "\n"), "")
	strutil.WordWrapPadded(w, []rune(key), padding, termWidth)
	return buffer.String()
}

func (s *modelInfoSuite) getSerial(c *C, serialText string) asserts.Serial {
	encoded := strings.Replace(serialText, "TSLINE", s.tsLine, 1)
	encoded = strings.Replace(encoded, "DEVICEKEY", strings.Replace(s.encodedDevKey, "\n", "\n    ", -1), 1)
	encoded = strings.Replace(encoded, "KEYID", s.deviceKey.PublicKey().ID(), 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SerialType)
	serial := a.(*asserts.Serial)
	return *serial
}

func (s *modelInfoSuite) TestPrintModelYAML(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, modelExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   false,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintModelAssertion(w, model, nil, s.formatter, options)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, `brand     brand-id1
model     Baz 3000 (baz-3000)
serial    - (device not registered yet)
`)
}

func (s *modelInfoSuite) TestPrintModelWithSerialYAML(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, modelExample)
	serial := s.getSerial(c, serialExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   false,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintModelAssertion(w, model, &serial, s.formatter, options)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, `brand     brand-id1
model     Baz 3000 (baz-3000)
serial    2700
`)
}

func (s *modelInfoSuite) TestPrintModelYAMLVerbose(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, core20ModelExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   true,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintModelAssertion(w, model, nil, s.formatter, options)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`brand-id:                 brand-id1
model:                    baz-3000
grade:                    dangerous
storage-safety:           prefer-unencrypted
serial:                   -- (device not registered yet)
architecture:             amd64
base:                     core20
display-name:             Baz 3000
store:                    brand-store
system-user-authority:
  - partner
  - brand-id1
timestamp:    %s
snaps:
  - name:               baz-linux
    id:                 bazlinuxidididididididididididid
    type:               kernel
    default-channel:    20
  - name:               brand-gadget
    id:                 brandgadgetdidididididididididid
    type:               gadget
  - name:               other-base
    id:                 otherbasedididididididididididid
    type:               base
    presence:           required
    modes:              [run]
  - name:               nm
    id:                 nmididididididididididididididid
    default-channel:    1.0
    modes:              [ephemeral, run]
  - name:               myapp
    id:                 myappdididididididididididididid
    type:               app
    default-channel:    2.0
  - name:               myappopt
    id:                 myappoptidididididididididididid
    type:               app
    presence:           optional
`, timeutil.Human(s.ts)))
}

func (s *modelInfoSuite) TestPrintModelAssertion(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, modelExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   false,
		AbsTime:   false,
		Assertion: true,
	}
	err := clientutil.PrintModelAssertion(w, model, nil, s.formatter, options)
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

func (s *modelInfoSuite) TestPrintSerialYAML(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	serial := s.getSerial(c, serialExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   false,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintSerialAssertionYAML(w, serial, s.formatter, options)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, `brand-id:    brand-id1
model:       baz-3000
serial:      2700
`)
}

func (s *modelInfoSuite) TestPrintSerialAssertion(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	serial := s.getSerial(c, serialExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   false,
		AbsTime:   false,
		Assertion: true,
	}
	err := clientutil.PrintSerialAssertionYAML(w, serial, s.formatter, options)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`type: serial
authority-id: brand-id1
brand-id: brand-id1
model: baz-3000
serial: 2700
device-key:
%sdevice-key-sha3-384: %s
timestamp: %s
body-length: 2
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

HW

`, s.getDeviceKey("    ", options.TermWidth), s.deviceKey.PublicKey().ID(), s.ts.Format(time.RFC3339)))
}

func (s *modelInfoSuite) TestPrintSerialVerboseYAML(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	serial := s.getSerial(c, serialExample)

	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   true,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintSerialAssertionYAML(w, serial, s.formatter, options)
	c.Assert(err, IsNil)
	c.Check(buffer.String(), Equals, fmt.Sprintf(`brand-id:     brand-id1
model:        baz-3000
serial:       2700
timestamp:    %s
device-key-sha3-384: |
  %s
device-key: |
%s`, timeutil.Human(s.ts), s.deviceKey.PublicKey().ID(), s.getDeviceKey("  ", options.TermWidth)))
}

func (s *modelInfoSuite) TestPrintModelJSON(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, modelExample)

	// For JSON verbose is always implictly true
	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   true,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintModelAssertionJSON(w, model, nil, options)
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
  "serial": null,
  "store": "brand-store",
  "system-user-authority": "*",
  "timestamp": "%s"
}`, s.ts.Format(time.RFC3339)))
}

func (s *modelInfoSuite) TestPrintModelWithSerialJSON(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, modelExample)
	serial := s.getSerial(c, serialExample)

	// For JSON verbose is always implictly true
	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   true,
		AbsTime:   false,
		Assertion: false,
	}
	err := clientutil.PrintModelAssertionJSON(w, model, &serial, options)
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
  "serial": "2700",
  "store": "brand-store",
  "system-user-authority": "*",
  "timestamp": "%s"
}`, s.ts.Format(time.RFC3339)))
}

func (s *modelInfoSuite) TestPrintModelJSONAssertion(c *C) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 5, 4, ' ', 0)
	model := s.getModel(c, modelExample)

	// For JSON verbose is always implictly true
	options := clientutil.PrintModelAssertionOptions{
		TermWidth: 80,
		Verbose:   true,
		AbsTime:   false,
		Assertion: true,
	}
	err := clientutil.PrintModelAssertionJSON(w, model, nil, options)
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
