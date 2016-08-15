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
timestamp: 2016-08-11T18:35:58+02:00
username: canonical
validation: certified
sign-key-sha3-384: X6havsrdOOwrut1RRbAJ8r0r28dbv-voCBat3kUTgY0Qs3eOsaNUVGugUfNYCzqn

AcLBUgQAAQoABgUCV6ypbgAAaHwQAE8MVU3+dmv55oMCrIBAksheYNQZ9Yb92CL+gKFc2Vvo2gDM
LTwy4VZJYL/NyedrFH6dkhdDVXFiwyLc/gxdChLOM0SGdKvjn4dNrxedltmL7+E/vtwMiTMuV2Be
z48mT8eNVSHLgcGQMxlj08G1SmKNIqcL1U6nkC23ANAQfBMqR+QhFXIRbxq6l2DqWKeShwupRJVk
MmYNjxy8IRndb5n7FN3j4bljTakPWiIrf87VbZ+Ogoy3uDOHTso2g95DJqUqGVt06i4vlOOrRAju
ZTxwf/GH/Tft+3Xoq00Yc80MKnEKlqO5olJ/OyDkq4n1qWzU26upLZYCbSdxQ+4hTu9k9wZ+s0Om
mwKuPnFPGgvwPyFygQC70L5nnK1NW+vYuECVmt8XHLaNmhW5ftOve92/ZoPAZGggBBjotKpfmr1m
xIKX211lMwQSnxepWaAd9QMzOTe2axMnMl1yyMylcK2lL5aJDw5e6hT7m+qHVP88eGf/D0bKikfM
zjoQNoKxU5VrBpzf7URky2of3Gvjst1Ird0kwC30/u8TbHhzRxRJHrO5zK0OBVd+opeF40Gf8MbC
r66s+bqq9qEAXaf4jBhKqVDsucdErQmrsKFRbszYQdHfut8aXOJ1rWy8OvHRrdlAzvBZNu+RJV6O
Kjgz1G+xXdopdFdqO1xPAXz4RNoR
`

	encodedCanonicalRootAccountKey = `type: account-key
authority-id: canonical
public-key-sha3-384: X6havsrdOOwrut1RRbAJ8r0r28dbv-voCBat3kUTgY0Qs3eOsaNUVGugUfNYCzqn
account-id: canonical
since: 2016-08-11T18:35:58+02:00
body-length: 717
sign-key-sha3-384: X6havsrdOOwrut1RRbAJ8r0r28dbv-voCBat3kUTgY0Qs3eOsaNUVGugUfNYCzqn

AcbBTQRWhcGAARAAzQYWcVYpvOkKM1YVOZ37wZ9PfpATafOa2Xm1QiwinrLozijkX33f6lGP7bNN
zboCyWP3fMt8aPsfFKSuXkUI2ktsoVrIFWsYjuQjD7ngHGXHWlu6BND+rsnUm+4renZm8FZtiJPH
d7rWsnmkl5wAIxCaPtrCm8NgMYPcD77J2b1hAYc1wviqevWmZ5+6aFcOlo1WBIe+bvZ8qJe88i/2
RJ8r/82+zO012VMs98D7iOrtevAaejjRJZjUFF8MJg89zbk4Xz+te/hhj8jLkz/+r6Dp1Fv+0VpE
FS8tolBJkRbJPtdgQj83YJSgLN57BYLNmGQQA+nJzd+2ev6FOB0CsDipHddk7w+UehXvQ8QQ4OMD
De2A/SsRkBaygLVlLrxL0eZITI6ghIZTN+kGKc5eSoTrZx1tSS7b9jb4rtSbqFvqhpdJXNrlngVw
GfqiG1bB6iYIRlWbgNJox0D1y1S+gTgv8VIIKyZzr4LC1Yx7ciDklW+D4vUyiDWKdDdD2N3/L3MK
qbBRDeji3VAEyRBNIEVCc4E3dJ1O4ksKy4hjV9u7P+fuDQvRYK4vshy5wY38NcXPfUHBELz695II
MlDUv6HVo7FZh046r3Y+LazxGaPmCYtjzJ8DZrbdOtxncBzNrU0bguGPBuE0RU+YeOhqQMPamteQ
muzEQ3tXGtPPBlsAEQEAAQ==

AcLBUgQAAQoABgUCV6ypbgAABuYQAHOvCrzhMlRh/QgwHt3szfKaWYr5Q7srE1iB+GRyu3iOuIUq
qmnmmBFDoUCFXRPkXSaYZ/AvnkTORr6rwaW3jG0WrWEsubxnwAdfb/vsVT2Ng0ICTxdIMoJzAzto
/xh/WrZxcOiOjk0IKZFUk7WZGXEgeVR50pYzDht5XhCnkZh4EAw6NbuR7YYTHOkLZXQqRK/aWkrz
0fNpWpCl8k4NSAxLxpxoQMUnnpXf/X5ASzlEKlJ4nROSNcuoUGw5pLOhUPTaRmmZD9SDlMoH9xbY
Ka0242VmZibCe7+YT/gG5XVkAQoFQA6Hg5LRAw3yEWx1X3c7UsiHfBK6PU4LhxmhvGpBNMJzqpix
SZ/d2TmNt/E0MR8EsC/FoBMjJlw9x6eMHw19PBrNC+8zoBFTmXsw1GRtg75TlJton/BbQgbvjksS
+kpNueSeSNUC/V5FwOWcVasXv/axhkutkppbNQD6fIKjRrAfR0F5KyYZMIXzsjz67KviIDiRTIot
B0F1GemGY8W+FYc/qs8ohzNIaSXADpoP6CB5AW+2W2+St1uDRVTVDuQr9aYLt9Kxy5CAPnnGuHB1
yx7Yflp/8bSN+gDnsQLjl5tvbhcD2jcyoSCT5tYyeQLsll21mtPpAlQSttOAKBNqGkfizZLic+uh
H7YzXSE30X26wsgACbgyVMRAgxqP
`
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
