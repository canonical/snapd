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

package asserts_test

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

type requestMessageSuite struct {
}

var _ = Suite(&requestMessageSuite{})

const (
	requestMessageExample = `type: request-message
authority-id: store
account-id: account-id-1
message-id: someId
message-kind: confdb
devices:
  - generic.generic-classic.03961d5d-26e5
  - acme-corp.rpi5.66:c5:d7:14:84:f8
  - some.very.long.device.id
assumes:
  - snapd2.70
valid-since: 2025-01-08T13:31:20+00:00
valid-until: 2025-01-15T13:31:20+00:00
timestamp: 2025-01-08T13:31:20+00:00
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp
body-length: 112

%s

AXNpZw==`

	reqBodyExample = `{
  "action": "get",
  "account": "account-id-1",
  "view": "network/read-proxy",
  "keys": [ "https", "ftp" ]
}`
)

func (s *requestMessageSuite) TestDecodeOK(c *C) {
	encoded := fmt.Sprintf(requestMessageExample, reqBodyExample)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.RequestMessageType)

	req := a.(*asserts.RequestMessage)
	c.Check(req.AuthorityID(), Equals, "store")
	c.Check(req.AccountID(), Equals, "account-id-1")
	c.Check(req.ID(), Equals, "someId")
	c.Check(req.SeqNum(), Equals, 0)
	c.Check(req.Kind(), Equals, "confdb")

	expectedDevices := []asserts.DeviceID{
		{"generic", "generic-classic", "03961d5d-26e5"},
		{"acme-corp", "rpi5", "66:c5:d7:14:84:f8"},
		{"some", "very", "long.device.id"},
	}
	c.Check(req.Devices(), DeepEquals, expectedDevices)

	c.Check(req.Assumes(), DeepEquals, []string{"snapd2.70"})

	c.Check(string(req.Body()), Equals, reqBodyExample)
}

func (s *requestMessageSuite) TestDecodeSequencedOK(c *C) {
	encoded := fmt.Sprintf(requestMessageExample, reqBodyExample)
	encoded = strings.Replace(encoded, "message-id: someId", "message-id: someId-4", 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	req := a.(*asserts.RequestMessage)
	c.Check(req.ID(), Equals, "someId")
	c.Check(req.SeqNum(), Equals, 4)
}

func (s *requestMessageSuite) TestDecodeNoAssumesOK(c *C) {
	encoded := fmt.Sprintf(requestMessageExample, reqBodyExample)
	encoded = strings.Replace(encoded, "assumes:\n  - snapd2.70\n", "", 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	req := a.(*asserts.RequestMessage)
	c.Check(req.Assumes(), HasLen, 0)
}

func (s *requestMessageSuite) TestDecodeInvalid(c *C) {
	encoded := fmt.Sprintf(requestMessageExample, reqBodyExample)

	const errPrefix = "assertion request-message: "
	devices := `devices:
  - generic.generic-classic.03961d5d-26e5
  - acme-corp.rpi5.66:c5:d7:14:84:f8
  - some.very.long.device.id
`

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: account-id-1\n", "", `"account-id" header is mandatory`},
		{"account-id: account-id-1\n", "account-id: \n", `"account-id" header should not be empty`},
		{"account-id: account-id-1\n", "account-id: @9\n", `invalid account id: @9`},
		{"message-id: someId\n", "", `"message-id" header is mandatory`},
		{"message-id: someId\n", "message-id: \n", `"message-id" header should not be empty`},
		{"message-id: someId\n", "message-id: s#ome&Id\n", "invalid message-id: s#ome&Id"},
		{"message-id: someId\n", "message-id: someId-\n", "invalid message-id: someId-"},
		{"message-id: someId\n", "message-id: someId-abc\n", "invalid message-id: someId-abc"},
		{"message-id: someId\n", "message-id: someId-0\n", "invalid message-id: sequence number must be greater than 0"},
		{"message-kind: confdb\n", "", `"message-kind" header is mandatory`},
		{"message-kind: confdb\n", "message-kind: \n", `"message-kind" header should not be empty`},
		{"message-kind: confdb\n", "message-kind: 23#s\n", `"message-kind" header contains invalid characters: "23#s"`},
		{devices, "", `"devices" header must not be empty`},
		{devices, "devices: \n", `"devices" header must be a list of strings`},
		{devices, "devices:\n  - ab\n", "cannot parse device at position 1: invalid device id: ab"},
		{devices, "devices:\n  - a#3.b.c\n", `cannot parse device at position 1: invalid brand-id: a#3`},
		{devices, "devices:\n  - abc.x3#4.y\n", `cannot parse device at position 1: invalid model: x3#4`},
		{"assumes:\n  - snapd2.70\n", "assumes: \n", `"assumes" header must be a list of strings`},
		{"assumes:\n  - snapd2.70\n", "assumes:\n  - 345345\n", `invalid assumes: unsupported features: 345345`},
		{"valid-since: 2025-01-08T13:31:20+00:00\n", "", `"valid-since" header is mandatory`},
		{"valid-until: 2025-01-15T13:31:20+00:00\n", "", `"valid-until" header is mandatory`},
		{"valid-until: 2025-01-15T13:31:20+00:00\n", "valid-until: 2024-01-15T13:31:20+00:00\n", `'valid-until' time cannot be before 'valid-since' time`},
		{"timestamp: 2025-01-08T13:31:20+00:00\n", "", `"timestamp" header is mandatory`},
		{"body-length: 112\n\n" + reqBodyExample, "body-length: 0", `body must not be empty`},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, errPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}
