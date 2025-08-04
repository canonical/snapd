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
	"sort"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/testutil"
)

type deviceTrackerSuite struct{}

var _ = check.Suite(&deviceTrackerSuite{})

func (s *deviceTrackerSuite) TestDeviceTrackerLookup(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	c.Assert(dt.Identified("self"), check.Equals, true)

	c.Assert(dt.Identified("other"), check.Equals, false)

	id, ok := dt.Lookup("self")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, self)

	_, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, false)

	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.Identify(other)

	c.Assert(dt.Identified("other"), check.Equals, true)
	id, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, other)
}

func (s *deviceTrackerSuite) TestDeviceTrackerQueries(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.Identify(other)

	// peer queries us for devices
	dt.Query("peer", []assemblestate.DeviceToken{"self", "other"})

	// should signal responses channel
	c.Assert(hasSignal(dt.ResponsesAvailable()), check.Equals, true)

	ids, ack := dt.QueryResponses("peer")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.DeviceToken, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"self", "other"})

	// ack should remove the queries
	ack()

	// should have no more responses
	ids, _ = dt.QueryResponses("peer")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerSources(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	// peers tells us they know about some devices
	dt.UpdateSource("peer", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.UpdateSource("other", []assemblestate.DeviceToken{"device-1", "device-2"})

	// should signal queries channel (we don't know these devices)
	c.Assert(hasSignal(dt.QueriesAvailable()), check.Equals, true)

	// should have queries for this peer
	unknown, ack := dt.QueryableFrom("peer")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should mark as in flight
	ack()

	// since we have queries in flight, we shouldn't report new queries from
	// this peer
	unknown, _ = dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 0)

	unknown, _ = dt.QueryableFrom("other")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerTimeout(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}

	// expected calls from: QueryableFrom, ack, QueryableFrom, QueryableFrom
	// this mocks time passing, enabling us to test queries for device IDs that
	// are in flight
	called := 0
	now := time.Now()
	results := []time.Time{now, now, now.Add(time.Second / 2), now.Add(time.Second)}
	clock := func() time.Time {
		t := results[called]
		called++
		return t
	}

	dt := assemblestate.NewDeviceQueryTracker(self, time.Second, clock, assemblestate.DeviceQueryTrackerData{})

	// peer tells us they know about device
	dt.UpdateSource("peer", []assemblestate.DeviceToken{"device-1"})

	unknown, ack := dt.QueryableFrom("peer")
	c.Assert(len(unknown), check.Equals, 1)

	// ack marks as in flight
	ack()

	// since the query is in flight, we shouldn't return anything here
	unknown, _ = dt.QueryableFrom("peer")
	c.Assert(len(unknown), check.Equals, 0)

	// should return query again after the timeout expires
	unknown, _ = dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerChannels(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	responses := dt.ResponsesAvailable()
	queries := dt.QueriesAvailable()

	dt.RetryQueries()
	c.Assert(hasSignal(queries), check.Equals, true)

	dt.RetryResponses()
	c.Assert(hasSignal(responses), check.Equals, true)

	// multiple retry calls don't ever block
	dt.RetryQueries()
	dt.RetryQueries()
	dt.RetryResponses()
	dt.RetryResponses()
}

func (s *deviceTrackerSuite) TestDeviceTrackerEmptyQueries(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	// empty query shouldn't signal channel
	dt.Query("peer", []assemblestate.DeviceToken{})
	c.Assert(hasSignal(dt.ResponsesAvailable()), check.Equals, false)

	// should handle empty peer queries correctly
	ids, _ := dt.QueryResponses("unknown")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerUnknownDevices(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	// query for unknown device should be ignored
	dt.Query("peer", []assemblestate.DeviceToken{"unknown"})
	c.Assert(hasSignal(dt.ResponsesAvailable()), check.Equals, false)

	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.Identify(other)

	dt.UpdateSource("peer", []assemblestate.DeviceToken{"self", "other", "unknown"})

	// should skip devices we already know
	unknown, _ := dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("unknown"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerNoMissingDevices(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	// add device we know about
	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.Identify(other)

	// source update with only known devices shouldn't signal
	dt.UpdateSource("peer", []assemblestate.DeviceToken{"self", "other"})
	c.Assert(hasSignal(dt.QueriesAvailable()), check.Equals, false)

	// should return empty when all devices are known
	unknown, _ := dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededIDs(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	one := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-1")}
	two := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-2")}

	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			"device-1": one,
			"device-2": two,
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, data)

	// preseeded devices should be identified
	c.Assert(dt.Identified("device-1"), check.Equals, true)
	c.Assert(dt.Identified("device-2"), check.Equals, true)

	// should be able to lookup preseeded devices
	id, ok := dt.Lookup("device-1")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, one)

	id, ok = dt.Lookup("device-2")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, two)

	// self should still be identified
	c.Assert(dt.Identified("self"), check.Equals, true)
	id, ok = dt.Lookup("self")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, self)
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededUnknowns(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	one := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-1")}
	two := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-2")}

	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			"device-1": one,
			"device-2": two,
		},
		Unknowns: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-1"},
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, data)

	// should have responses for preseeded unknowns
	ids, ack := dt.QueryResponses("peer-1")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.DeviceToken, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should clear the responses
	ack()
	ids, _ = dt.QueryResponses("peer-1")
	c.Assert(ids, check.HasLen, 0)

	// peer-2 should have responses for device-1 only
	ids, _ = dt.QueryResponses("peer-2")
	c.Assert(ids, check.HasLen, 1)
	c.Assert(ids[0].RDT, check.Equals, assemblestate.DeviceToken("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededSources(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}

	data := assemblestate.DeviceQueryTrackerData{
		Sources: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-3"},
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, data)

	// should be able to query from preseeded sources
	unknown, ack := dt.QueryableFrom("peer-1")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should mark as in flight
	ack()

	// should not return same queries while in flight
	unknown, _ = dt.QueryableFrom("peer-1")
	c.Assert(unknown, check.HasLen, 0)

	// peer-2 should have separate queries
	unknown, _ = dt.QueryableFrom("peer-2")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-3"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerExport(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	one := assemblestate.Identity{
		RDT:         assemblestate.DeviceToken("device-1"),
		FP:          assemblestate.Fingerprint{1, 2, 3},
		Serial:      "serial-1",
		SerialProof: assemblestate.Proof{7, 8, 9},
	}
	two := assemblestate.Identity{
		RDT:         assemblestate.DeviceToken("device-2"),
		FP:          assemblestate.Fingerprint{4, 5, 6},
		Serial:      "serial-2",
		SerialProof: assemblestate.Proof{10, 11, 12},
	}

	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, assemblestate.DeviceQueryTrackerData{})

	dt.Identify(one)
	dt.Identify(two)
	dt.Query("peer-1", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.Query("peer-2", []assemblestate.DeviceToken{"self"})
	dt.UpdateSource("peer-1", []assemblestate.DeviceToken{"device-3", "device-4"})
	dt.UpdateSource("peer-2", []assemblestate.DeviceToken{"device-5"})

	exported := dt.Export()
	normalizeDeviceExport(exported)

	expected := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			"self":     self,
			"device-1": one,
			"device-2": two,
		},
		Unknowns: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"self"},
		},
		Sources: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-3", "device-4"},
			"peer-2": {"device-5"},
		},
	}

	c.Assert(exported, check.DeepEquals, expected)
}

func (s *deviceTrackerSuite) TestDeviceTrackerExportRoundtrip(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	one := assemblestate.Identity{
		RDT:         assemblestate.DeviceToken("device-1"),
		FP:          assemblestate.Fingerprint{1, 2, 3},
		Serial:      "serial-1",
		SerialProof: assemblestate.Proof{7, 8, 9},
	}
	two := assemblestate.Identity{
		RDT:         assemblestate.DeviceToken("device-2"),
		FP:          assemblestate.Fingerprint{4, 5, 6},
		Serial:      "serial-2",
		SerialProof: assemblestate.Proof{10, 11, 12},
	}

	initial := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			"device-1": one,
			"device-2": two,
		},
		Unknowns: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-1"},
		},
		Sources: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-3", "device-4"},
			"peer-2": {"device-5", "device-6"},
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, initial)

	exported := dt.Export()
	dt = assemblestate.NewDeviceQueryTracker(self, time.Minute, time.Now, exported)
	exported = dt.Export()
	normalizeDeviceExport(exported)

	initial.IDs["self"] = self
	c.Assert(exported, check.DeepEquals, initial)
}

func normalizeDeviceExport(d assemblestate.DeviceQueryTrackerData) {
	for _, devices := range d.Unknowns {
		sort.Slice(devices, func(i, j int) bool {
			return devices[i] < devices[j]
		})
	}
	for _, devices := range d.Sources {
		sort.Slice(devices, func(i, j int) bool {
			return devices[i] < devices[j]
		})
	}
}

func hasSignal(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
