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

package sysdb

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
)

const (
	// TODO(matt): these are temporary asserts so tests start to pass. we need
	// to generate and embed real ones.
	encodedCanonicalAccount = `type: account
authority-id: canonical
account-id: canonical
display-name: Canonical
timestamp: 2016-04-01T00:00:00.0Z
username: canonical
validation: certified
sign-key-sha3-384: meokUyTBYzujY2PRL1LQnLZD4VFJuv023xqqZGvSHvZBuTOvJ82jiDZUI4WdsiKZ

AcLBXAQAAQoABgUCV6RehQAKCRCL+nyrE4tzhzC1EACdJJtzOELivTtuv7i1KGHKhZf58wRPtA+u
MQLm6QMKV5CA0jqlvgwLdbNAjqHdXizLDIOdbeI/wOwSu3BGAtk2+X4E025XJoW4NMzE/3h28uKl
pjiHukQYnDwdjoug5vT0pXVjHrI+lc0GIUVu2sK0/ZbZnC2rlo1Ksdnv+BmIEUrYrtVGUmYiVcY1
wGtVBO4Hn5nCYCfCVPGrd/3ow441DinHtxJmaPX1/SXZhwxrmer6OV+W6jEiBaF+3xbYj7chsFRl
oSG5zwbw5QwAhUVWDEFkPl4kJpVDmvzHN2xucizBmjoUeKyp/xdrSSKoj8jd/2tqOD7iV1sBgIw3
FpUuw1j94JVutHt1i3WFfiCWkubXSf2yb3XgbcPZm7kwROaY2mA4gZTfPVIbhFIp3J0o1Tc8zcPQ
rbAouRrSGBU3Scej8M2ihLASXTsOiSrQIgqmaiRPG5NGP7QH0YUMyI7bBsVDAxgZNbgl4wH8B+Of
+DaszPv8YkQpnRy9j2wdPPzk3826xMHRmnlwNdB3d3/l45/WfGLJoz1XpwksrBARUJPV3z5wf6c6
UQuxJVU0wFbDzmr+Jj2k3Iokn+SHR3dnxCUtUzWzrgxcv/0Xv60lRAzyHM1lWjf3vjWjGMaLhOYU
D+tOQlYHGsciGTM6MIt/Xi/QALN4W/ttrCxWlKAEfA==`

	encodedCanonicalRootAccountKey = `type: account-key
authority-id: canonical
public-key-sha3-384: meokUyTBYzujY2PRL1LQnLZD4VFJuv023xqqZGvSHvZBuTOvJ82jiDZUI4WdsiKZ
account-id: canonical
since: 2016-04-01T00:00:00.0Z
body-length: 717
sign-key-sha3-384: meokUyTBYzujY2PRL1LQnLZD4VFJuv023xqqZGvSHvZBuTOvJ82jiDZUI4WdsiKZ

AcbBTQQAAAABARAAwZSFPSQWmN6HPgZ5pu03Lg0WOWwS7OSSBEmpAz9kKgopoaP2r5t1ikMiCEky
vn/R9fGR1FyAnR8tjeneBg5ygRnYZiCKofSIxPYfbWXg0nKjAXOWoJg9Y3ZSeFRG9blBHxYIdoIf
vBDEeGLKQOL2Bc76+uyPgFyfFlVYyE1qdpm4Ob7Xghk9NwXAliKoaWkkmVBG0bzhTHef81CGZxvi
w9RRHi72NpsHAuuwMX8hZzHfmsadPgFavAL3fUu0WnC9O0OXkgONi81kBIf3nPtdFUWT5c//SaIT
3dQfqW3NtFT2A97sF9Nu31cJFDEtAlwFy/Gwx+hMnrSuJj2y35hBVAkfkjPaPI7i2AwYYrKfF9T2
dma6HjQTsFQDBJEvyShCj+0U0tNbCIH5HiMtFl33QAPNZgLBJHU15PMHLAOBf/I1kfQN/NcOHP6i
UwsILOKMNxiClHEKiA8aJQyahJAFPQmzdBYwAATvormaiixItvPnuT2kV3C45yPHIiqIUL9vxIfL
e1ZICC3sPGKR/DKq29BvJ9pWqNZ+n/q60HWo7Q0VnDCd2yII0zhyCMstCj7SQFIQhTvLFc8Obuii
ZDgw5zaOrEJzFGezA+JPNhwOj3KLerOF8xuabxCaLdvnbTb5St8VunSJn9sHqwHf4/VncGrqVaps
EeFdqJT4SQ4NCuUAEQEAAQ==

AcLBXAQAAQoABgUCV6NAaQAKCRCL+nyrE4tzh0r2EADBfgSGyPm9LmazTi+tEA/rLHEhF/ZsNKrL
6AQKP6de3j5cqCg/6I6bnEPeVol2zLCaqCq8UEhpoAIQergmESXsl7ZtrhIIzQaSLRCXZfyyFu/q
zO7nPXwyXCP5koJXrCn4gwFXA9p0z63vBso+PtzmGODbpiQxBdSnU1zC6rBbKERYUMpXKbw9D1hc
5bLNtSGaYpi3wc9HPTGlMaPEKT74T+Fza35uSGpQdxiqTeDODZnQ43v4fPIFHINMqMDjsalIJIH+
FzekNbbcVG1g68kkxzNhcET2lEfnwj/itKkD218ddVdVuprMFpBTpBal1dF2QT1pd/ehxh4mIdYB
OEQZpCUPrW8QAQCmgNO3fpK0A/pGkQeRtD+f0ne40kYp7SBFCal3k/16VwkYArUng0tgEN8ioOgA
BGusuPXPYtbeL1/cGeN4igE0O2ANBTF1H8tExGBQ/2QiIPj9NTX9gULyz/XB7sjyS5C84J3mBL3z
w+9j4WujuDE3d1OmSzeDvwzTuhiups/OWdCd1mUTa5q1/LhPBiWj/nqrpx42XHesyHKwppBiKEs4
qOZcYOIicrnQQJIkMZcPW4JUVMJoCZnVJ40Naja7j/KN3MExQh01tdgiews28nHbd8z0QK4CeORG
pd/me29aVFIRnw5bYOOWZAn+/0YXSYK8A595jw6h/g==`
)

var trustedAssertions []asserts.Assertion

func init() {
	canonicalAccount, err := asserts.Decode([]byte(encodedCanonicalAccount))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	canonicalRootAccountKey, err := asserts.Decode([]byte(encodedCanonicalRootAccountKey))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	trustedAssertions = []asserts.Assertion{canonicalAccount, canonicalRootAccountKey}
}

// Trusted returns a copy of the current set of trusted assertions as used by Open.
func Trusted() []asserts.Assertion {
	return append([]asserts.Assertion(nil), trustedAssertions...)
}

// InjectTrusted injects further assertions into the trusted set for Open.
// Returns a restore function to reinstate the previous set. Useful
// for tests or called globally without worrying about restoring.
func InjectTrusted(extra []asserts.Assertion) (restore func()) {
	prev := trustedAssertions
	trustedAssertions = make([]asserts.Assertion, len(prev)+len(extra))
	copy(trustedAssertions, prev)
	copy(trustedAssertions[len(prev):], extra)
	return func() {
		trustedAssertions = prev
	}
}
