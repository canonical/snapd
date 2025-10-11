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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type clusterSuite struct {
	ts     time.Time
	tsLine string
}

var _ = Suite(&clusterSuite{})

func (cs *clusterSuite) SetUpSuite(c *C) {
	cs.ts = time.Now().Truncate(time.Second).UTC()
	cs.tsLine = "timestamp: " + cs.ts.Format(time.RFC3339) + "\n"
}

const (
	clusterExample = `type: cluster
authority-id: authority-id
cluster-id: bf3675f5-cffa-40f4-a119-7492ccc08e04
sequence: 3
devices:
  -
    id: 1
    brand-id: canonical
    model: ubuntu-core-24-amd64
    serial: 9cc45ad6-d01b-4efd-9f76-db55b76c076b
    addresses:
      - 192.168.1.10
      - 10.0.0.10
  -
    id: 2
    brand-id: canonical
    model: ubuntu-core-24-amd64
    serial: bc3c0a19-cdad-4cfc-a6f0-85e917bc6280
    addresses:
      - 192.168.1.20
subclusters:
  -
    name: default
    devices:
      - 1
      - 2
    snaps:
      -
        state: clustered
        instance: clustered-snap
        channel: stable
      -
        state: evacuated
        instance: evacuated-snap
        channel: edge
  -
    name: additional-cluter
    devices:
      - 2
    snaps:
      -
        state: removed
        instance: removed-snap
        channel: 24/stable
TSLINE` +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
)

func (cs *clusterSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(clusterExample, "TSLINE", cs.tsLine, 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ClusterType)

	cluster := a.(*asserts.Cluster)
	c.Check(cluster.AuthorityID(), Equals, "authority-id")
	c.Check(cluster.ClusterID(), Equals, "bf3675f5-cffa-40f4-a119-7492ccc08e04")
	c.Check(cluster.Sequence(), Equals, 3)

	devices := cluster.Devices()
	c.Assert(devices, HasLen, 2)

	c.Check(devices[0].ID, Equals, 1)
	c.Check(devices[0].BrandID, Equals, "canonical")
	c.Check(devices[0].Model, Equals, "ubuntu-core-24-amd64")
	c.Check(devices[0].Serial, Equals, "9cc45ad6-d01b-4efd-9f76-db55b76c076b")
	c.Check(devices[0].Addresses, DeepEquals, []string{"192.168.1.10", "10.0.0.10"})

	c.Check(devices[1].ID, Equals, 2)
	c.Check(devices[1].BrandID, Equals, "canonical")
	c.Check(devices[1].Model, Equals, "ubuntu-core-24-amd64")
	c.Check(devices[1].Serial, Equals, "bc3c0a19-cdad-4cfc-a6f0-85e917bc6280")
	c.Check(devices[1].Addresses, DeepEquals, []string{"192.168.1.20"})

	subclusters := cluster.Subclusters()
	c.Assert(subclusters, HasLen, 2)

	c.Check(subclusters[0].Name, Equals, "default")
	c.Check(subclusters[0].Devices, DeepEquals, []int{1, 2})
	c.Assert(subclusters[0].Snaps, HasLen, 2)
	c.Check(subclusters[0].Snaps[0].State, Equals, asserts.ClusterSnapStateClustered)
	c.Check(subclusters[0].Snaps[0].Instance, Equals, "clustered-snap")
	c.Check(subclusters[0].Snaps[0].Channel, Equals, "stable")
	c.Check(subclusters[0].Snaps[1].State, Equals, asserts.ClusterSnapStateEvacuated)
	c.Check(subclusters[0].Snaps[1].Instance, Equals, "evacuated-snap")
	c.Check(subclusters[0].Snaps[1].Channel, Equals, "edge")

	c.Check(subclusters[1].Name, Equals, "additional-cluter")
	c.Check(subclusters[1].Devices, DeepEquals, []int{2})
	c.Assert(subclusters[1].Snaps, HasLen, 1)
	c.Check(subclusters[1].Snaps[0].State, Equals, asserts.ClusterSnapStateRemoved)
	c.Check(subclusters[1].Snaps[0].Instance, Equals, "removed-snap")
	c.Check(subclusters[1].Snaps[0].Channel, Equals, "24/stable")
}

func (cs *clusterSuite) TestDecodeInvalidTopLevel(c *C) {
	encoded := strings.Replace(clusterExample, "TSLINE", cs.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"cluster-id: bf3675f5-cffa-40f4-a119-7492ccc08e04\n", "", `"cluster-id" header is mandatory`},
		{"cluster-id: bf3675f5-cffa-40f4-a119-7492ccc08e04\n", "cluster-id: \n", `"cluster-id" header should not be empty`},
		{"sequence: 3\n", "sequence: 0\n", `"sequence" must be >=1: 0`},
		{"devices:\n  -\n    id: 1\n    brand-id: canonical\n    model: ubuntu-core-24-amd64\n    serial: 9cc45ad6-d01b-4efd-9f76-db55b76c076b\n    addresses:\n      - 192.168.1.10\n      - 10.0.0.10\n  -\n    id: 2\n    brand-id: canonical\n    model: ubuntu-core-24-amd64\n    serial: bc3c0a19-cdad-4cfc-a6f0-85e917bc6280\n    addresses:\n      - 192.168.1.20\n", "devices: not-a-list\n", `"devices" header must be a list`},
		{"subclusters:\n  -\n    name: default\n    devices:\n      - 1\n      - 2\n    snaps:\n      -\n        state: clustered\n        instance: clustered-snap\n        channel: stable\n      -\n        state: evacuated\n        instance: evacuated-snap\n        channel: edge\n  -\n    name: additional-cluter\n    devices:\n      - 2\n    snaps:\n      -\n        state: removed\n        instance: removed-snap\n        channel: 24/stable\n", "subclusters: not-a-list\n", `"subclusters" header must be a list`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, ".*"+test.expectedErr)
	}
}

func (cs *clusterSuite) TestDecodeInvalidDevices(c *C) {
	encoded := strings.Replace(clusterExample, "TSLINE", cs.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"    id: 1\n", "    id: not-an-integer\n", `"id" header is not an integer: not-an-integer`},
		{"    id: 1\n", "    id: 0\n", `"id" header must be >=1: 0`},
		{"    id: 1\n", "    id: -1\n", `"id" header must be >=1: -1`},
		{"    brand-id: canonical\n", "", `"brand-id" header is mandatory`},
		{"    brand-id: canonical\n", "    brand-id: Canonical\n", `"brand-id" header contains invalid characters: "Canonical"`},
		{"    model: ubuntu-core-24-amd64\n", "", `"model" header is mandatory`},
		{"    model: ubuntu-core-24-amd64\n", "    model: Ubuntu-Core\n", `"model" header cannot contain uppercase letters`},
		{"    serial: 9cc45ad6-d01b-4efd-9f76-db55b76c076b\n", "", `"serial" header is mandatory`},
		{"    addresses:\n      - 192.168.1.10\n      - 10.0.0.10\n", "    addresses: not-a-list\n", `"addresses" header must be a list of strings`},
		{"  -\n    id: 1\n    brand-id: canonical\n    model: ubuntu-core-24-amd64\n    serial: 9cc45ad6-d01b-4efd-9f76-db55b76c076b\n    addresses:\n      - 192.168.1.10\n      - 10.0.0.10\n", "  - device-string\n", `"devices" field must be a list of maps`},
		{"    id: 2\n", "    id: 1\n", `"devices" field contains duplicate device id 1`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, ".*"+test.expectedErr)
	}
}

func (cs *clusterSuite) TestDecodeInvalidSubclusters(c *C) {
	encoded := strings.Replace(clusterExample, "TSLINE", cs.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"        state: clustered\n", "        state: invalid-state\n", `snap state must be one of: "clustered", "evacuated", "removed"`},
		{"      - 1\n", "      - not-a-number\n", `device id "not-a-number" is not an integer: not-a-number`},
		{"      - 1\n", "      - 0\n", `device id must be >=1: 0`},
		{"      - 1\n", "      - -1\n", `device id must be >=1: -1`},
		{"    name: default\n", "", `"name" header is mandatory`},
		{"        state: clustered\n", "", `"state" header is mandatory`},
		{"        instance: clustered-snap\n", "", `"instance" header is mandatory`},
		{"        instance: clustered-snap\n", "        instance: invalid instance\n", `invalid snap instance name: invalid snap name: "invalid instance"`},
		{"        channel: stable\n", "", `"channel" header is mandatory`},
		{"        channel: stable\n", "        channel: invalid//channel\n", `invalid channel name "invalid//channel": invalid risk in channel name: invalid//channel`},
		{"      -\n        state: clustered\n        instance: clustered-snap\n        channel: stable\n", "      - snap-string\n", `"snaps" field must be a list of maps`},
		{"  -\n    name: default\n    devices:\n      - 1\n      - 2\n    snaps:\n      -\n        state: clustered\n        instance: clustered-snap\n        channel: stable\n      -\n        state: evacuated\n        instance: evacuated-snap\n        channel: edge\n", "  - subcluster-string\n", `"subclusters" field must be a list of maps`},
		{"    devices:\n      - 1\n      - 2\n", "    devices: not-a-list\n", `"devices" header must be a list of strings`},
		{"    snaps:\n      -\n        state: clustered\n        instance: clustered-snap\n        channel: stable\n      -\n        state: evacuated\n        instance: evacuated-snap\n        channel: edge\n", "    snaps: not-a-list\n", `"snaps" header must be a list`},
		{"      - 2\n    snaps:", "      - 999\n    snaps:", `"subclusters" references unknown device id 999`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, ".*"+test.expectedErr)
	}
}
