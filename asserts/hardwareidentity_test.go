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

type hardwareIdentitySuite struct {
}

var _ = Suite(&hardwareIdentitySuite{})

const (
	hardwareIdentityExample = `type: hardware-identity
issuer-id: account-id-1
manufacturer: some-manufacturer
hardware-name: raspberry-pi-4gb
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp
hardware-id: random-id-1
hardware-id-key: MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAlqcRew53puqlX/kB8GzyWWfYeAsDlx72Rss2Gu74G7rK6fgHGwcZaNVDg2Ka4qD04J7r6KS6GLqJOzr5AdEw94s1uoF/ugiFaJTKTIRHNANOTrhWr6RRSviowxlV67oOo0pA3EtQnNrgKtUbg0FDXp7/NhkGSIFCTLcTE/kIVr87BRyhTqjRO8dmzVVIZm3DQQEa39hdaHKncLmev1Uv+SdODtauEx10ITVX9ikyCEi/T1PzkNEoeuy5Bq7iqkz1ch0fnyty6Nic78hlcf+Wj16HxvnExWWHM/b0rAJqVp9zs/Ut6qKPsLsCVCvuZ0GF/0F+DAfegbNNVon9+bGA7QIDAQAB
hardware-id-key-sha3-384: a6c08d57270eab22300e8428d91d27974624361d4ba6f0264114ddbb6a865c7b71f83f626bdaac952c002e1d19779387

AXNpZw==`
)

func (s *hardwareIdentitySuite) TestDecodeOK(c *C) {

	a, err := asserts.Decode([]byte(hardwareIdentityExample))
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.HardwareIdentityType)

	req := a.(*asserts.HardwareIdentity)
	c.Check(req.IssuerId(), Equals, "account-id-1")
	c.Check(req.Manufacturer(), Equals, "some-manufacturer")
	c.Check(req.HardwareName(), Equals, "raspberry-pi-4gb")
	c.Check(req.HardwareId(), Equals, "random-id-1")
	c.Check(req.HardwareIdKey(), Equals, `MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAlqcRew53puqlX/kB8GzyWWfYeAsDlx72Rss2Gu74G7rK6fgHGwcZaNVDg2Ka4qD04J7r6KS6GLqJOzr5AdEw94s1uoF/ugiFaJTKTIRHNANOTrhWr6RRSviowxlV67oOo0pA3EtQnNrgKtUbg0FDXp7/NhkGSIFCTLcTE/kIVr87BRyhTqjRO8dmzVVIZm3DQQEa39hdaHKncLmev1Uv+SdODtauEx10ITVX9ikyCEi/T1PzkNEoeuy5Bq7iqkz1ch0fnyty6Nic78hlcf+Wj16HxvnExWWHM/b0rAJqVp9zs/Ut6qKPsLsCVCvuZ0GF/0F+DAfegbNNVon9+bGA7QIDAQAB`)
	c.Check(req.HardwareIdKeySha3384(), Equals, "a6c08d57270eab22300e8428d91d27974624361d4ba6f0264114ddbb6a865c7b71f83f626bdaac952c002e1d19779387")

	

	c.Check(string(req.Body()), Equals, "")
}

func (s *hardwareIdentitySuite) TestDecodeInvalid(c *C) {
	const errPrefix = "assertion hardware-identity: "
	hardwareKeyId := "hardware-id-key: MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAlqcRew53puqlX/kB8GzyWWfYeAsDlx72Rss2Gu74G7rK6fgHGwcZaNVDg2Ka4qD04J7r6KS6GLqJOzr5AdEw94s1uoF/ugiFaJTKTIRHNANOTrhWr6RRSviowxlV67oOo0pA3EtQnNrgKtUbg0FDXp7/NhkGSIFCTLcTE/kIVr87BRyhTqjRO8dmzVVIZm3DQQEa39hdaHKncLmev1Uv+SdODtauEx10ITVX9ikyCEi/T1PzkNEoeuy5Bq7iqkz1ch0fnyty6Nic78hlcf+Wj16HxvnExWWHM/b0rAJqVp9zs/Ut6qKPsLsCVCvuZ0GF/0F+DAfegbNNVon9+bGA7QIDAQAB\n"
	hardwareKeyIdSha3384 := "hardware-id-key-sha3-384: a6c08d57270eab22300e8428d91d27974624361d4ba6f0264114ddbb6a865c7b71f83f626bdaac952c002e1d19779387\n"

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"issuer-id: account-id-1\n", "", `"issuer-id" header is mandatory`},
		{"issuer-id: account-id-1\n", "issuer-id: \n", `"issuer-id" header should not be empty`},
		{"issuer-id: account-id-1\n", "issuer-id: @9\n", `invalid issuer id: @9`},
		{"manufacturer: some-manufacturer\n", "", `"manufacturer" header is mandatory`},
		{"manufacturer: some-manufacturer\n", "manufacturer: \n", `"manufacturer" header should not be empty`},
		{"hardware-name: raspberry-pi-4gb\n", "", `"hardware-name" header is mandatory`},
		{"hardware-name: raspberry-pi-4gb\n", "hardware-name: \n", `"hardware-name" header should not be empty`},
		{"hardware-id: random-id-1\n", "", `"hardware-id" header is mandatory`},
		{"hardware-id: random-id-1\n", "hardware-id: \n", `"hardware-id" header should not be empty`},
		{hardwareKeyId, "", `"hardware-id-key" header is mandatory`},
		{hardwareKeyId, "hardware-id-key: \n", `"hardware-id-key" header should not be empty`},
		{hardwareKeyId, "hardware-id-key: something\n", `"hardware-id-key" header should be the body of a PEM`},
		{hardwareKeyIdSha3384, "", `"hardware-id-key-sha3-384" header is mandatory`},
		{hardwareKeyIdSha3384, "hardware-id-key-sha3-384: \n", `"hardware-id-key-sha3-384" header should not be empty`},

	}

	for i, test := range invalidTests {
		
		invalid := strings.Replace(hardwareIdentityExample, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, errPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}