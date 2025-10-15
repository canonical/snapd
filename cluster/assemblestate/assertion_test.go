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

package assemblestate_test

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/cluster/assemblestate"
	"gopkg.in/check.v1"
)

func (s *assembleSuite) TestAssertionDevices(c *check.C) {
	_, signing := mockAssertDB(c)

	const secret = "secret"

	type expected struct {
		brand     string
		model     string
		serial    string
		addresses []any
	}

	var identities []assemblestate.Identity
	var rdts []assemblestate.DeviceToken

	expectedByRDT := make(map[assemblestate.DeviceToken]expected)
	addrsByRDT := map[assemblestate.DeviceToken][]any{
		"device-0": {"10.0.1.1:8080"},
		"device-1": {"10.0.2.1:8080", "10.0.2.2:8080"},
		"device-2": {"10.0.3.1:8080"},
	}

	for i := 0; i < 3; i++ {
		serial, bundle, key := createTestSerialBundle(c, signing)
		rdt := assemblestate.DeviceToken(fmt.Sprintf("device-%d", i))
		fp := assemblestate.CalculateFP([]byte(fmt.Sprintf("certificate-%d", i)))

		hmac := assemblestate.CalculateHMAC(rdt, fp, secret)
		proof, err := asserts.RawSignWithKey(hmac, key)
		c.Assert(err, check.IsNil)

		identities = append(identities, assemblestate.Identity{
			RDT:          rdt,
			FP:           fp,
			SerialBundle: bundle,
			SerialProof:  proof,
		})

		expectedByRDT[rdt] = expected{
			brand:     serial.BrandID(),
			model:     serial.Model(),
			serial:    serial.Serial(),
			addresses: addrsByRDT[rdt],
		}
		rdts = append(rdts, rdt)
	}

	routes := assemblestate.Routes{
		Devices: []assemblestate.DeviceToken{rdts[0], rdts[1], rdts[2]},
		Addresses: []string{
			"10.0.2.1:8080",
			"10.0.2.2:8080",
			"10.0.3.1:8080",
			"10.0.1.1:8080",
		},
		Routes: []int{
			0, 1, 0,
			2, 1, 1,
			0, 2, 2,
			1, 2, 2,
			2, 0, 3,
			1, 0, 3,
		},
	}

	devices, err := assemblestate.AssertionDevices(identities, routes)
	c.Assert(err, check.IsNil)

	c.Assert(devices, check.HasLen, len(identities))

	for i, raw := range devices {
		dev, ok := raw.(map[string]any)
		c.Assert(ok, check.Equals, true)

		c.Assert(dev["id"], check.Equals, strconv.Itoa(i+1))

		exp := expectedByRDT[identities[i].RDT]

		c.Assert(dev["brand-id"], check.Equals, exp.brand)
		c.Assert(dev["model"], check.Equals, exp.model)
		c.Assert(dev["serial"], check.Equals, exp.serial)

		addrs, ok := dev["addresses"].([]any)
		c.Assert(ok, check.Equals, true)
		c.Assert(addrs, check.DeepEquals, exp.addresses)
	}

	// make sure that the output of assemblestate.AssertionDevices matches the
	// expected format of asserts.Cluster
	assertDevicesFormCluster(c, devices)
}

func assertDevicesFormCluster(c *check.C, devices []any) {
	key, _ := assertstest.GenerateKey(752)
	signing := assertstest.NewSigningDB("authority-id", key)

	headers := map[string]any{
		"type":       "cluster",
		"cluster-id": "cluster-id",
		"sequence":   "1",
		"devices":    devices,
		"timestamp":  time.Now().Format(time.RFC3339),
	}

	as, err := signing.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	cluster := as.(*asserts.Cluster)
	c.Assert(cluster.Devices(), check.HasLen, len(devices))
}

func (s *assembleSuite) TestAssertionDevicesMissingAddresses(c *check.C) {
	_, signing := mockAssertDB(c)

	const secret = "secret"

	_, bundle, key := createTestSerialBundle(c, signing)
	rdt := assemblestate.DeviceToken("device-0")
	fp := assemblestate.CalculateFP([]byte("certificate-0"))

	hmac := assemblestate.CalculateHMAC(rdt, fp, secret)
	proof, err := asserts.RawSignWithKey(hmac, key)
	c.Assert(err, check.IsNil)

	identities := []assemblestate.Identity{{
		RDT:          rdt,
		FP:           fp,
		SerialBundle: bundle,
		SerialProof:  proof,
	}}

	routes := assemblestate.Routes{
		Devices: []assemblestate.DeviceToken{rdt},
	}

	devices, err := assemblestate.AssertionDevices(identities, routes)
	c.Assert(err, check.ErrorMatches, `no addresses available for device "device-0"`)
	c.Assert(devices, check.IsNil)
}

func (s *assembleSuite) TestAssertionDevicesErrors(c *check.C) {
	_, signing := mockAssertDB(c)

	id := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-0"),
		assemblestate.CalculateFP([]byte("certificate-0")),
		"secret",
	)

	copyID := func() assemblestate.Identity {
		return assemblestate.Identity{
			RDT:          id.RDT,
			FP:           id.FP,
			SerialBundle: id.SerialBundle,
			SerialProof:  append([]byte(nil), id.SerialProof...),
		}
	}

	cases := []struct {
		name  string
		setup func() ([]assemblestate.Identity, assemblestate.Routes)
		err   string
	}{
		{
			name: "serial bundle missing serial assertion",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				identity := copyID()
				identity.SerialBundle = stripSerialAssertion(c, identity.SerialBundle)
				return []assemblestate.Identity{identity}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{0, 0, 0},
				}
			},
			err: ".*serial assertion not found in bundle",
		},
		{
			name: "serial bundle invalid",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				identity := copyID()
				identity.SerialBundle = "invalid"
				return []assemblestate.Identity{identity}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{0, 0, 0},
				}
			},
			err: ".*cannot decode serial bundle: unexpected EOF",
		},
		{
			name: "invalid routes length",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				identity := copyID()
				return []assemblestate.Identity{identity}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{0, 0},
				}
			},
			err: "routes array length must be multiple of 3",
		},
		{
			name: "source index out of range",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				identity := copyID()
				return []assemblestate.Identity{identity}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{1, 0, 0},
				}
			},
			err: "invalid source device index 1 in routes",
		},
		{
			name: "destination index out of range",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				identity := copyID()
				return []assemblestate.Identity{identity}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{0, 1, 0},
				}
			},
			err: "invalid destination device index 1 in routes",
		},
		{
			name: "address index out of range",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				identity := copyID()
				return []assemblestate.Identity{identity}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{0, 0, 1},
				}
			},
			err: "invalid address index 1 in routes",
		},
	}

	for _, tc := range cases {
		ids, routes := tc.setup()
		_, err := assemblestate.AssertionDevices(ids, routes)
		c.Check(err, check.ErrorMatches, tc.err, check.Commentf("test name: %s", tc.name))
	}
}

func stripSerialAssertion(c *check.C, bundle string) string {
	dec := asserts.NewDecoder(strings.NewReader(bundle))
	buf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(buf)
	for {
		a, err := dec.Decode()
		if err != nil {
			if err == io.EOF {
				break
			}
			c.Fatalf("unexpected decode error: %v", err)
		}

		if a.Type() == asserts.SerialType {
			continue
		}

		c.Assert(enc.Encode(a), check.IsNil)
	}
	return buf.String()
}
