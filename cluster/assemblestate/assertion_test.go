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
	const secret = "secret"

	type expected struct {
		device    string
		addresses []any
	}

	brands := []string{"brand-b", "brand-a", "brand-a", "brand-a"}
	models := []string{"model-b", "model-b", "model-a", "model-b"}
	serials := []string{"serial-1", "serial-2", "serial-3", "serial-0"}
	rdts := []assemblestate.DeviceToken{
		assemblestate.DeviceToken("device-0"),
		assemblestate.DeviceToken("device-1"),
		assemblestate.DeviceToken("device-2"),
		assemblestate.DeviceToken("device-3"),
	}
	addrs := [][]string{
		{"10.0.1.1:8080"},
		{"10.0.2.1:8080", "10.0.2.2:8080"},
		{"10.0.3.1:8080"},
		{"10.0.4.1:8080"},
	}

	var identities []assemblestate.Identity
	expectedByRDT := make(map[assemblestate.DeviceToken]expected)
	for i := range rdts {
		serial, bundle, key := makeBundleWithID(c, brands[i], models[i], serials[i])
		fp := assemblestate.CalculateFP([]byte(fmt.Sprintf("certificate-%d", i)))

		hmac := assemblestate.CalculateHMAC(rdts[i], fp, secret)
		proof, err := asserts.RawSignWithKey(hmac, key)
		c.Assert(err, check.IsNil)

		identities = append(identities, assemblestate.Identity{
			RDT:          rdts[i],
			FP:           fp,
			SerialBundle: bundle,
			SerialProof:  proof,
		})

		addresses := make([]any, 0, len(addrs[i]))
		for _, addr := range addrs[i] {
			addresses = append(addresses, addr)
		}

		expectedByRDT[rdts[i]] = expected{
			device:    serial.DeviceID().String(),
			addresses: addresses,
		}
	}

	routes := assemblestate.Routes{
		Devices: []assemblestate.DeviceToken{rdts[0], rdts[1], rdts[2], rdts[3]},
		Addresses: []string{
			"10.0.2.1:8080",
			"10.0.2.2:8080",
			"10.0.3.1:8080",
			"10.0.1.1:8080",
			"10.0.4.1:8080",
		},
		Routes: []int{
			0, 1, 0,
			2, 1, 1,
			0, 2, 2,
			1, 2, 2,
			2, 0, 3,
			1, 0, 3,
			0, 3, 4,
			1, 3, 4,
		},
	}

	devices, err := assemblestate.AssertionDevices(identities, routes)
	c.Assert(err, check.IsNil)

	c.Assert(devices, check.HasLen, len(identities))

	expectedDeviceOrder := []assemblestate.DeviceToken{
		rdts[2], // brand-a / model-a / serial-3
		rdts[3], // brand-a / model-b / serial-0
		rdts[1], // brand-a / model-b / serial-2
		rdts[0], // brand-b / model-b / serial-1
	}

	for i, raw := range devices {
		dev, ok := raw.(map[string]any)
		c.Assert(ok, check.Equals, true)

		c.Assert(dev["id"], check.Equals, strconv.Itoa(i+1))

		exp := expectedByRDT[expectedDeviceOrder[i]]

		deviceID, ok := dev["device"].(string)
		c.Assert(ok, check.Equals, true)
		c.Assert(deviceID, check.Equals, exp.device)

		addrs, ok := dev["addresses"].([]any)
		c.Assert(ok, check.Equals, true)
		c.Assert(addrs, check.DeepEquals, exp.addresses)
	}

	// make sure that the output of [assemblestate.Routes] matches the expected
	// format of [asserts.Cluster]
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

func makeBundleWithID(
	c *check.C,
	brand string,
	model string,
	serial string,
) (*asserts.Serial, string, asserts.PrivateKey) {
	signing := assertstest.NewStoreStack(brand, nil)

	deviceKey, _ := assertstest.GenerateKey(752)
	pubkey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	headers := map[string]any{
		"authority-id":        brand,
		"brand-id":            brand,
		"model":               model,
		"serial":              serial,
		"device-key":          string(pubkey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}

	assertion, err := signing.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	serialAssertion := assertion.(*asserts.Serial)
	bundle := buildSerialBundle(c, serialAssertion, signing.Database)

	return serialAssertion, bundle, deviceKey
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
			name: "duplicate rdt",
			setup: func() ([]assemblestate.Identity, assemblestate.Routes) {
				first := copyID()
				second := copyID()
				return []assemblestate.Identity{first, second}, assemblestate.Routes{
					Devices:   []assemblestate.DeviceToken{"device-0"},
					Addresses: []string{"10.0.0.1:443"},
					Routes:    []int{0, 0, 0},
				}
			},
			err: `duplicate device token found in identities: "device-0"`,
		},
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
