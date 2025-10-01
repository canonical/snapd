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
	"strings"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)


var _ = Suite(&hardwareIdentitySuite{})

type hardwareIdentitySuite struct {}

const errPrefix = "assertion hardware-identity: "

const (
	hardwareIdentityExample = `type: hardware-identity
authority-id: account-id-1
issuer-id: account-id-1
manufacturer: some-manufacturer
hardware-name: raspberry-pi-4gb
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp
hardware-id: random-id-1
hardware-id-key: TUlHZk1BMEdDU3FHU0liM0RRRUJBUVVBQTRHTkFEQ0JpUUtCZ1FDaVFjaFlNVFAra25jNnZtUFd3SC9tMThqbApIRVN5U0wyZFBIb25lQ1dnOUFuMlM0N3ZBQ1VJd3ZlU0FDRHFHam5Ld3JvcFM3Rmw1cTVFTTZNQXlIUElkSmJwCmdXUFV6bHJBRTRZc2M4VDh5QTUwUlRPaVFPc0x4MHZUMnFHU0kzYk16bFU3bkhyZW0zWXRNOUErbjlGUEVuOVAKT2hNaGVyZkExekVFVmkvSWZ3SURBUUFC
hardware-id-key-sha3-384: 20HX1SS8dJW8tNCmybAdP7frkn0dmEV9DdwIABskpSsxBUaylHA6oO8dSj+ORWXa

AXNpZw==`
)


func (s *hardwareIdentitySuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(hardwareIdentityExample))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.HardwareIdentityType)

	req := a.(*asserts.HardwareIdentity)
	c.Check(req.IssuerID(), Equals, "account-id-1")
	c.Check(req.Manufacturer(), Equals, "some-manufacturer")
	c.Check(req.HardwareName(), Equals, "raspberry-pi-4gb")
	c.Check(req.HardwareID(), Equals, "random-id-1")
	c.Check(req.HardwareIDKey(), Equals, "TUlHZk1BMEdDU3FHU0liM0RRRUJBUVVBQTRHTkFEQ0JpUUtCZ1FDaVFjaFlNVFAra25jNnZtUFd3SC9tMThqbApIRVN5U0wyZFBIb25lQ1dnOUFuMlM0N3ZBQ1VJd3ZlU0FDRHFHam5Ld3JvcFM3Rmw1cTVFTTZNQXlIUElkSmJwCmdXUFV6bHJBRTRZc2M4VDh5QTUwUlRPaVFPc0x4MHZUMnFHU0kzYk16bFU3bkhyZW0zWXRNOUErbjlGUEVuOVAKT2hNaGVyZkExekVFVmkvSWZ3SURBUUFC")
	c.Check(req.HardwareIDKeySha3384(), Equals, "20HX1SS8dJW8tNCmybAdP7frkn0dmEV9DdwIABskpSsxBUaylHA6oO8dSj+ORWXa")	
	c.Check(string(req.Body()), Equals, "")
}

func (s *hardwareIdentitySuite) TestDecodeInvalid(c *C) {
	const (
		hardwareKeyID        = "hardware-id-key: TUlHZk1BMEdDU3FHU0liM0RRRUJBUVVBQTRHTkFEQ0JpUUtCZ1FDaVFjaFlNVFAra25jNnZtUFd3SC9tMThqbApIRVN5U0wyZFBIb25lQ1dnOUFuMlM0N3ZBQ1VJd3ZlU0FDRHFHam5Ld3JvcFM3Rmw1cTVFTTZNQXlIUElkSmJwCmdXUFV6bHJBRTRZc2M4VDh5QTUwUlRPaVFPc0x4MHZUMnFHU0kzYk16bFU3bkhyZW0zWXRNOUErbjlGUEVuOVAKT2hNaGVyZkExekVFVmkvSWZ3SURBUUFC\n"
		elGamalhardwareKey   = "hardware-id-key: TUZNd09BWUdLdzRIQWdFQk1DNENGUUR0Z0dwZGNhdXkraExpSFF2TzFVV240ck90Q3dJVkFPdmg2OEZYNjBHVQo1TllFOW05MzJESDhYOFpvQXhjQUFoUU5PdEFNYktUazdqQi9FSlgvaWJ3bGVpWFpDZz09\n"
		hardwareKeyIDSha3384 = "hardware-id-key-sha3-384: 20HX1SS8dJW8tNCmybAdP7frkn0dmEV9DdwIABskpSsxBUaylHA6oO8dSj+ORWXa\n"
	)


	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"issuer-id: account-id-1\n", "", `"issuer-id" header is mandatory`},
		{"issuer-id: account-id-1\n", "issuer-id: \n", `"issuer-id" header should not be empty`},
		{"issuer-id: account-id-1\n", "issuer-id: @9\n", `"issuer-id" header contains invalid characters: "@9"`},
		{"issuer-id: account-id-1\n", "issuer-id: account-id-2\n", `issuer id must match authority id`},
		{"manufacturer: some-manufacturer\n", "", `"manufacturer" header is mandatory`},
		{"manufacturer: some-manufacturer\n", "manufacturer: \n", `"manufacturer" header should not be empty`},
		{"hardware-name: raspberry-pi-4gb\n", "", `"hardware-name" header is mandatory`},
		{"hardware-name: raspberry-pi-4gb\n", "hardware-name: \n", `"hardware-name" header should not be empty`},
		{"hardware-name: raspberry-pi-4gb\n", "hardware-name: raspberry&pi\n", `"hardware-name" header contains invalid characters: "raspberry&pi"`},
		{"hardware-id: random-id-1\n", "", `"hardware-id" header is mandatory`},
		{"hardware-id: random-id-1\n", "hardware-id: \n", `"hardware-id" header should not be empty`},
		{hardwareKeyID, "", `"hardware-id-key" header is mandatory`},
		{hardwareKeyID, "hardware-id-key: \n", `"hardware-id-key" header should not be empty`},
		{hardwareKeyID, "hardware-id-key: something\n", `illegal base64 data .*`},
		{hardwareKeyID, "hardware-id-key: TUlHZU1BMEdDU3FH\n", `asn1: syntax error: .*`},
		{hardwareKeyID, elGamalhardwareKey, `x509: unknown public key algorithm`},
		{hardwareKeyIDSha3384, "", `"hardware-id-key-sha3-384" header is mandatory`},
		{hardwareKeyIDSha3384, "hardware-id-key-sha3-384: \n", `"hardware-id-key-sha3-384" header should not be empty`},
		{hardwareKeyIDSha3384, "hardware-id-key-sha3-384: random\n", `hardware id key does not match provided hash`},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(hardwareIdentityExample, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, errPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}
