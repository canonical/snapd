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
	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			self.RDT: self,
		},
	}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	c.Assert(dt.Identified("self"), check.Equals, true)

	c.Assert(dt.Identified("other"), check.Equals, false)

	id, ok := dt.Lookup("self")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, self)

	_, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, false)

	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.RecordIdentity(other)

	c.Assert(dt.Identified("other"), check.Equals, true)
	id, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, other)
}

func (s *deviceTrackerSuite) TestDeviceTrackerQueries(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			self.RDT: self,
		},
	}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.RecordIdentity(other)

	// peer queries us for devices
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"self", "other"})

	// should signal responses channel
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, true)

	ids, ack := dt.ResponsesTo("peer")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.DeviceToken, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"self", "other"})

	// ack should remove the queries
	const success = true
	ack(success)

	// should have no more responses
	ids, _ = dt.ResponsesTo("peer")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerDropUnknownQuery(c *check.C) {
	dt := assemblestate.NewDeviceQueryTracker(assemblestate.DeviceQueryTrackerData{}, time.Minute, time.Now)

	// peer queries us for unknown device
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"unknown"})

	// should not signal responses channel
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	// query should not be recorded at all
	c.Assert(dt.Export(), check.DeepEquals, assemblestate.DeviceQueryTrackerData{
		Queries: make(map[assemblestate.DeviceToken][]assemblestate.DeviceToken),
		Known:   make(map[assemblestate.DeviceToken][]assemblestate.DeviceToken),
		IDs:     make(map[assemblestate.DeviceToken]assemblestate.Identity),
	})
}

func (s *deviceTrackerSuite) TestDeviceTrackerSources(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	// peers tells us they know about some devices
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.RecordDevicesKnownBy("other", []assemblestate.DeviceToken{"device-1", "device-2"})

	// should signal queries channel (we don't know these devices)
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, true)

	// should have queries for this peer
	unknown, ack := dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should mark as in flight
	const success = true
	ack(success)

	// since we have queries in flight, we shouldn't report new queries from
	// this peer
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 0)

	unknown, _ = dt.OutgoingQueriesTo("other")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerTimeout(c *check.C) {

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

	data := assemblestate.DeviceQueryTrackerData{}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Second, clock)

	// peer tells us they know about device
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"device-1"})

	unknown, ack := dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 1)

	// ack marks as in flight
	const success = true
	ack(success)

	// since the query is in flight, we shouldn't return anything here
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 0)

	// should return query again after the timeout expires
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerFailedQueryAck(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Hour, time.Now)

	// peer tells us they know about device
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"device-1"})

	unknown, ack := dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))

	// ack with false indicates that we could not send the query
	const failure = false
	ack(failure)

	// signal indicates that there are still queries available to send
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, true)

	// since we failed to send the query, it should be returned again here
	unknown, ack = dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))

	const success = true
	ack(success)

	// signal indicates that there is nothing to send
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, false)

	// now that we've marked it as a successful send, it should not be returned
	// here
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerFailedResponseAck(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	one := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-1")}

	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			self.RDT:   self,
			"device-1": one,
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(data, time.Hour, time.Now)

	// peer tells us they need info about a device
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"device-1"})

	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, true)

	ids, ack := dt.ResponsesTo("peer")
	c.Assert(len(ids), check.Equals, 1)
	c.Assert(ids[0], check.Equals, one)

	// ack with false indicates that we could not send the query
	const failure = false
	ack(failure)

	// signal indicates that there are still responses available to send
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, true)

	// since we failed to send the response, it should be returned again here
	ids, ack = dt.ResponsesTo("peer")
	c.Assert(len(ids), check.Equals, 1)
	c.Assert(ids[0], check.Equals, one)

	const success = true
	ack(success)

	// now no response signal
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	ids, _ = dt.ResponsesTo("peer")
	c.Assert(len(ids), check.Equals, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerEmptyQueries(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	// empty query shouldn't signal channel
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{})
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	// should handle empty peer queries correctly
	ids, _ := dt.ResponsesTo("unknown")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerUnknownDevices(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	// query for unknown device should be ignored
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"unknown"})
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.RecordIdentity(other)

	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"other", "unknown"})

	// should skip devices we already know
	unknown, _ := dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("unknown"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerNoMissingDevices(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	// add device we know about
	other := assemblestate.Identity{RDT: assemblestate.DeviceToken("other")}
	dt.RecordIdentity(other)

	// source update with only known devices shouldn't signal
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"other"})
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, false)

	// should return empty when all devices are known
	unknown, _ := dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededIDs(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.DeviceToken("self")}
	one := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-1")}
	two := assemblestate.Identity{RDT: assemblestate.DeviceToken("device-2")}

	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			self.RDT:   self,
			"device-1": one,
			"device-2": two,
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

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
			self.RDT:   self,
			"device-1": one,
			"device-2": two,
		},
		Queries: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-1"},
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	// should have responses for preseeded unknowns
	ids, ack := dt.ResponsesTo("peer-1")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.DeviceToken, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should clear the responses
	const success = true
	ack(success)

	ids, _ = dt.ResponsesTo("peer-1")
	c.Assert(ids, check.HasLen, 0)

	// peer-2 should have responses for device-1 only
	ids, _ = dt.ResponsesTo("peer-2")
	c.Assert(ids, check.HasLen, 1)
	c.Assert(ids[0].RDT, check.Equals, assemblestate.DeviceToken("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededSources(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{
		Known: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-3"},
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	// should be able to query from preseeded sources
	unknown, ack := dt.OutgoingQueriesTo("peer-1")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should mark as in flight
	const success = true
	ack(success)

	// should not return same queries while in flight
	unknown, _ = dt.OutgoingQueriesTo("peer-1")
	c.Assert(unknown, check.HasLen, 0)

	// peer-2 should have separate queries
	unknown, _ = dt.OutgoingQueriesTo("peer-2")
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

	data := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			self.RDT: self,
		},
	}
	dt := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now)

	dt.RecordIdentity(one)
	dt.RecordIdentity(two)
	dt.RecordIncomingQuery("peer-1", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.RecordIncomingQuery("peer-2", []assemblestate.DeviceToken{"self"})
	dt.RecordDevicesKnownBy("peer-1", []assemblestate.DeviceToken{"device-3", "device-4"})
	dt.RecordDevicesKnownBy("peer-2", []assemblestate.DeviceToken{"device-5"})

	exported := dt.Export()
	normalizeDeviceExport(exported)

	expected := assemblestate.DeviceQueryTrackerData{
		IDs: map[assemblestate.DeviceToken]assemblestate.Identity{
			"self":     self,
			"device-1": one,
			"device-2": two,
		},
		Queries: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"self"},
		},
		Known: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
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
			self.RDT:   self,
			"device-1": one,
			"device-2": two,
		},
		Queries: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-1"},
		},
		Known: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-3", "device-4"},
			"peer-2": {"device-5", "device-6"},
		},
	}

	dt := assemblestate.NewDeviceQueryTracker(initial, time.Minute, time.Now)

	exported := dt.Export()
	dt = assemblestate.NewDeviceQueryTracker(exported, time.Minute, time.Now)
	exported = dt.Export()
	normalizeDeviceExport(exported)

	c.Assert(exported, check.DeepEquals, initial)
}

func normalizeDeviceExport(d assemblestate.DeviceQueryTrackerData) {
	for _, devices := range d.Queries {
		sort.Slice(devices, func(i, j int) bool {
			return devices[i] < devices[j]
		})
	}
	for _, devices := range d.Known {
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
