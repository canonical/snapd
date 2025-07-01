package assemblestate_test

import (
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/testutil"
)

type deviceTrackerSuite struct{}

var _ = check.Suite(&deviceTrackerSuite{})

func (s *deviceTrackerSuite) TestDeviceTrackerLookup(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	c.Assert(dt.Identified("self"), check.Equals, true)

	c.Assert(dt.Identified("other"), check.Equals, false)

	id, ok := dt.Lookup("self")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, self)

	_, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, false)

	other := assemblestate.Identity{RDT: assemblestate.RDT("other")}
	dt.Identify(other)

	c.Assert(dt.Identified("other"), check.Equals, true)
	id, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, other)
}

func (s *deviceTrackerSuite) TestDeviceTrackerQueries(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	other := assemblestate.Identity{RDT: assemblestate.RDT("other")}
	dt.Identify(other)

	// peer queries us for devices
	dt.Query("peer", []assemblestate.RDT{"self", "other"})

	// should signal responses channel
	c.Assert(hasSignal(dt.Responses()), check.Equals, true)

	ids, ack := dt.QueryResponses("peer")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.RDT, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.RDT{"self", "other"})

	// ack should remove the queries
	ack()

	// should have no more responses
	ids, _ = dt.QueryResponses("peer")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerSources(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	// peers tells us they know about some devices
	dt.UpdateSource("peer", []assemblestate.RDT{"device-1", "device-2"})
	dt.UpdateSource("other", []assemblestate.RDT{"device-1", "device-2"})

	// should signal queries channel (we don't know these devices)
	c.Assert(hasSignal(dt.Queries()), check.Equals, true)

	// should have queries for this peer
	unknown, ack := dt.QueryableFrom("peer")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.RDT{"device-1", "device-2"})

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
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	timeout := time.Second
	dt := assemblestate.NewDeviceTracker(self, timeout)

	// peer tells us they know about device
	dt.UpdateSource("peer", []assemblestate.RDT{"device-1"})

	unknown, ack := dt.QueryableFrom("peer")
	c.Assert(len(unknown), check.Equals, 1)

	// ack marks as in flight
	ack()

	// since the query is in flight, we shouldn't return anything here
	unknown, _ = dt.QueryableFrom("peer")
	c.Assert(len(unknown), check.Equals, 0)

	time.Sleep(timeout + time.Millisecond)

	// should return query again after the timeout expires
	unknown, _ = dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.RDT("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerChannels(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	responses := dt.Responses()
	queries := dt.Queries()

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
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	// empty query shouldn't signal channel
	dt.Query("peer", []assemblestate.RDT{})
	c.Assert(hasSignal(dt.Responses()), check.Equals, false)

	// should handle empty peer queries correctly
	ids, _ := dt.QueryResponses("unknown")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerUnknownDevices(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	// query for unknown device should be ignored
	dt.Query("peer", []assemblestate.RDT{"unknown"})
	c.Assert(hasSignal(dt.Responses()), check.Equals, false)

	other := assemblestate.Identity{RDT: assemblestate.RDT("other")}
	dt.Identify(other)

	dt.UpdateSource("peer", []assemblestate.RDT{"self", "other", "unknown"})

	// should skip devices we already know
	unknown, _ := dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.RDT("unknown"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerNoMissingDevices(c *check.C) {
	self := assemblestate.Identity{RDT: assemblestate.RDT("self")}
	dt := assemblestate.NewDeviceTracker(self, time.Minute)

	// add device we know about
	other := assemblestate.Identity{RDT: assemblestate.RDT("other")}
	dt.Identify(other)

	// source update with only known devices shouldn't signal
	dt.UpdateSource("peer", []assemblestate.RDT{"self", "other"})
	c.Assert(hasSignal(dt.Queries()), check.Equals, false)

	// should return empty when all devices are known
	unknown, _ := dt.QueryableFrom("peer")
	c.Assert(unknown, check.HasLen, 0)
}

func hasSignal(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
