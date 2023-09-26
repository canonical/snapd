// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type certsSuite struct {
	configcoreSuite
}

var _ = Suite(&certsSuite{})

func (s *certsSuite) TestConfigureCertsUnhappyName(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store-certs.cert-illegal-!": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `cannot set store ssl certificate under name "core.store-certs.cert-illegal-!": name must only contain word characters or a dash`)
}

var mockCert = `-----BEGIN CERTIFICATE-----
MIIEIDCCAwigAwIBAgIQNE7VVyDV7exJ9C/ON9srbTANBgkqhkiG9w0BAQUFADCB
qTELMAkGA1UEBhMCVVMxFTATBgNVBAoTDHRoYXd0ZSwgSW5jLjEoMCYGA1UECxMf
Q2VydGlmaWNhdGlvbiBTZXJ2aWNlcyBEaXZpc2lvbjE4MDYGA1UECxMvKGMpIDIw
MDYgdGhhd3RlLCBJbmMuIC0gRm9yIGF1dGhvcml6ZWQgdXNlIG9ubHkxHzAdBgNV
BAMTFnRoYXd0ZSBQcmltYXJ5IFJvb3QgQ0EwHhcNMDYxMTE3MDAwMDAwWhcNMzYw
NzE2MjM1OTU5WjCBqTELMAkGA1UEBhMCVVMxFTATBgNVBAoTDHRoYXd0ZSwgSW5j
LjEoMCYGA1UECxMfQ2VydGlmaWNhdGlvbiBTZXJ2aWNlcyBEaXZpc2lvbjE4MDYG
A1UECxMvKGMpIDIwMDYgdGhhd3RlLCBJbmMuIC0gRm9yIGF1dGhvcml6ZWQgdXNl
IG9ubHkxHzAdBgNVBAMTFnRoYXd0ZSBQcmltYXJ5IFJvb3QgQ0EwggEiMA0GCSqG
SIb3DQEBAQUAA4IBDwAwggEKAoIBAQCsoPD7gFnUnMekz52hWXMJEEUMDSxuaPFs
W0hoSVk3/AszGcJ3f8wQLZU0HObrTQmnHNK4yZc2AreJ1CRfBsDMRJSUjQJib+ta
3RGNKJpchJAQeg29dGYvajig4tVUROsdB58Hum/u6f1OCyn1PoSgAfGcq/gcfomk
6KHYcWUNo1F77rzSImANuVud37r8UVsLr5iy6S7pBOhih94ryNdOwUxkHt3Ph1i6
Sk/KaAcdHJ1KxtUvkcx8cXIcxcBn6zL9yZJclNqFwJu/U30rCfSMnZEfl2pSy94J
NqR32HuHUETVPm4pafs5SSYeCaWAe0At6+gnhcn+Yf1+5nyXHdWdAgMBAAGjQjBA
MA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMB0GA1UdDgQWBBR7W0XP
r87Lev0xkhpqtvNG61dIUDANBgkqhkiG9w0BAQUFAAOCAQEAeRHAS7ORtvzw6WfU
DW5FvlXok9LOAz/t2iWwHVfLHjp2oEzsUHboZHIMpKnxuIvW1oeEuzLlQRHAd9mz
YJ3rG9XRbkREqaYB7FViHXe4XI5ISXycO1cRrK1zN44veFyQaEfZYGDm/Ac9IiAX
xPcW6cTYcvnIc3zfFi8VqT79aie2oetaupgf1eNNZAqdE8hhuvU5HIe6uL17In/2
/qxAeeWsEG89jxt5dovEN7MhGITlNgDrYyCZuen+MwS7QcjBAvlEYyCegc5C09Y/
LHbTY5xZ3Y+m4Q6gLkH3LpVHz7z9M/P2C2F+fpErgUfCJzDupxBdN49cOSvkBPB7
jVaMaA==
-----END CERTIFICATE-----
`

func (s *certsSuite) TestConfigureCertsHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store-certs.cert1": mockCert,
		},
	})
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapdStoreSSLCertsDir, "cert1.pem"), testutil.FileEquals, mockCert)
}

func (s *certsSuite) TestConfigureCertsSimulteRevert(c *C) {
	// do a normal "snap set"
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store-certs.cert1": mockCert,
		},
	})
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapdStoreSSLCertsDir, "cert1.pem"), testutil.FilePresent)
	// and one more with a new cert that will be reverted
	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"store-certs.cert1": mockCert,
		},
		changes: map[string]interface{}{
			"store-certs.certthatwillbereverted": mockCert,
		},
	})
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapdStoreSSLCertsDir, "cert1.pem"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapdStoreSSLCertsDir, "certthatwillbereverted.pem"), testutil.FilePresent)

	// now simulate a "snap revert core" where "cert1" will stay in
	// the state but "cert-that-will-be-reverted" is part of the config
	// of the reverted core
	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"store-certs.cert1": mockCert,
		},
	})
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapdStoreSSLCertsDir, "cert1.pem"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapdStoreSSLCertsDir, "certthatwillbereverted.pem"), testutil.FileAbsent)
}

var certThatFailsToParse = `-----BEGIN CERTIFICATE-----
jVaMaA==
-----END CERTIFICATE-----
`

func (s *certsSuite) TestConfigureCertsFailsToParse(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store-certs.cert1": certThatFailsToParse,
		},
	})
	c.Assert(err, ErrorMatches, `cannot decode pem certificate "cert1"`)
}

func (s *certsSuite) TestConfigureCertsUnhappyContent(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store-certs.cert-bad": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `cannot decode pem certificate "cert-bad"`)
}
