// -*- Mode: Go; indent-tabs-mode: t -*-
// +build withtestkeys withstagingkeys

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
	encodedStagingTrustedAccount = `type: account
authority-id: canonical
account-id: canonical
display-name: Canonical
timestamp: 2016-04-01T00:00:00.0Z
username: canonical
validation: certified
sign-key-sha3-384: e2r8on4LgdxQSaW5T8mBD5oC2fSTktTVxOYa5w3kIP4_nNF6L7mt6fOJShdOGkKu

AcLBXAQAAQoABgUCV640ggAKCRAHKljtl9kuLrQtEADBji8VwAuislurkFORTmcXV/DOkvyvAYEN
mB/MLniK4MlLX+RDncDBmF38IK9SRkxbwwJuKgvsjwsYJ3w1P7SGvVfNyU2hLRFtdxDMVC7+A9g3
N1VW9W+IOWmYeBgXiveqAlSJ9GUvLQiBgUWRBkbyAT6aLkSZrTSjxGRGW/uoNfjj+CbAR4HGbRnn
IOxDuQyw6rOXQZKfZvkD1NiH+0QzXLv0RivE8+V5uVN+ooUFRoVQmqbj7orvPS9iTY5AMVjCgfo0
UiWiN6NyCfDBDz0bZhIZlBU4JF5W0I/sEwsuYCxIhFi5uPNmQXqqb5d9Y3bsxIUdMR0+pai1A3eI
HQmYX12wCnb276R5Adz4iol19oKAR2Qf3VJBvPccdIFU7Qu5FOOihQdMRxULBBXGn1HQF1uW+ue3
ZQ3x6e8s3XjdDQE/kHCDUkmzhbk1SErgndg6Q1ipKJ+4G6dOc16s66bSFA4QzW53Y40NP0HRWxe2
tK9VOJ+z9GvGYp5H1ZXbbs78t9bUwL7L6z/eXM6BRho6YY9X7nImpByIkdcV47dCyVFol6NrM5NS
NSpdtRStGqo7tjPaBf86p2vLOAbwFUuaE3rwf5g/agz4S/v5G5E2tKmfQs6vGYrfVtlOzr8gEoXH
+/hOEC3wYEJjpXmFRjUjJwr0Fbej2TpoITpfzbySpg==
`
	encodedStagingRootAccountKey = `type: account-key
authority-id: canonical
revision: 3
public-key-sha3-384: e2r8on4LgdxQSaW5T8mBD5oC2fSTktTVxOYa5w3kIP4_nNF6L7mt6fOJShdOGkKu
account-id: canonical
name: staging-root
since: 2016-04-01T00:00:00.0Z
body-length: 717
sign-key-sha3-384: e2r8on4LgdxQSaW5T8mBD5oC2fSTktTVxOYa5w3kIP4_nNF6L7mt6fOJShdOGkKu

AcbBTQRWhcGAARAA4wh+b9nyRdZj9gNKuHz8BTNZsLOVv2VJseHBoMNc4aA8EgmLwMF/aP+q1tAQ
VOeynhfSecIK/2aWKKX+dmU/rfAbnbdHX1NT8OnG2z3qdYdqw1EreN8LcY4DBDfa1RNKcjFvBu+Q
jxpU289m1yUjjc7yHie84BoYRgDl0icar8KF7vKx44wNhzbca+lw4xGSA5gpDZ1i1smdxdpOSsUY
WT70ZcJBN1oyKiiiCJUNLwCPzaPsH1i3WwDSaGsbjl8gjf2+LFNFPwdsWRbn3RLlFcFbET2bFe5y
v6UN+0cSh9qLJeLR2h0WDaVBp5Gx4PAYAfpIIF8EH3YbvI8uuTmBza8Ni0yozOZ2cXCSdezLGW2m
b6itOq/taBhgl8gzhKqki9jAOWmDBeBIbe2rUuNJrfHVH8+lWTzuzJIcHSHeAjFG1xid+HOOsw0e
Ag3JMjJaqCGCp0Oc9/WBtHV6jB30jLzht5QjJZ6izIKswRrvt0nCowp74FZ1l1ekXZPhhkA5MBMb
AoTiz9UvRZAWBPa5gX4R7eaekGjCPWI8NpJ7pT3Xh3NIHIsjyf0JcysoH2V1+A9qT1LOCyczf1Uc
9d8PXap1zhhQuczZcnw7vAwLEIwldfp08x6klsgiP6jqIB4XKJCjBDu/gn682ydWzfLT8echVHpg
uI62X67Ns1ZbFWMAEQEAAQ==

AcLBXAQAAQoABgUCV86jSgAKCRAHKljtl9kuLpV6EADO8Q1WKJwoTfeIpBpQfDhdhqJLmW86Qrjq
P9ZsndN8eA4uSbo08yg9jxi6Q3J/A5QK6rhTz5Nu41frKVpgFr80BpIx8cHsY2dZNyKCm70Jjy4h
cxteK7mwdAzdWG/Dg7Nr4fhOmpepsh1gIXvjWhTkT226DIO6l45o6N2hMKKkWmqJYqVD6i7UE4Ed
xmC+IoluhnKGGwM6JpyOw0RViXbLjVDR58n4q1xmK7cFduMoLKszVY4/KGmKT8gA6D4pUOa62F84
Ejh6akRum7uqygBibYT/DP+KA+MhHvpQ8XZem7IVIEnMJr7U2gde3brbVr0oiCl7FzfiBNy6qw92
cTsE8o3JV0Lc106SWU28GuWPgyXjoH8imzSmWlpQtlPlKEDwMQt31XDKUKp0ZKiEax3cQ6VjMv1f
PV3bHNjD+tBq5e1xm/UWyGu7J2N4VPLgUK7F4TPUJk5lwKjmII8KD3KA/IeHnZVN6vmC2nKfhGvw
+rJllQQ0IWY9RfIdzFHpVvthe48g27ok5yEgovAc/s7xWZ6CBgyzYWLQMNFvENj04CzGvxirKwuJ
Fy5UJIEKB0j0R2qnCz6HZkyQrUsz5HiIIlks18FfOZwuIc4GGPbwwQBoXW7a6KQg0aa62BPj5Iww
3w60rtTSUsjINkZ/GXLodfzPglOl6VLF7bWx2hGesg==
`
)

func init() {
	stagingTrustedAccount, err := asserts.Decode([]byte(encodedStagingTrustedAccount))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	stagingRootAccountKey, err := asserts.Decode([]byte(encodedStagingRootAccountKey))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	trustedStagingAssertions = []asserts.Assertion{stagingTrustedAccount, stagingRootAccountKey}
}
