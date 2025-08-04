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
	"fmt"
	"testing"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/randutil"
	"gopkg.in/check.v1"
)

type SelectorSuite struct{}

var _ = check.Suite(&SelectorSuite{})

func Test(t *testing.T) { check.TestingT(t) }

type mockSource struct {
	values []int64
	index  int
}

func (s *mockSource) Int63() int64 {
	if s.index >= len(s.values) {
		panic("unexpected usage of random source")
	}

	val := s.values[s.index]
	s.index++

	return val
}

func (s *mockSource) Seed(seed int64) {}

// newMockSource creates a deterministic randutil.Source that returns the given
// values in sequence.
func newMockSource(values ...int64) randutil.Source {
	return &mockSource{values: values}
}

func (s *SelectorSuite) TestAddRoutesValidation(c *check.C) {
	// not relevant for this test
	identified := func(r assemblestate.DeviceToken) bool { return true }
	selector := assemblestate.NewPrioritySelector("self", nil, identified)

	invalid := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1},
	}

	_, _, err := selector.RecordRoutes("peer", invalid)
	c.Assert(err, check.ErrorMatches, "length of routes list must be a multiple of three")

	negative := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, -1, 0},
	}

	_, _, err = selector.RecordRoutes("peer", negative)
	c.Assert(err, check.ErrorMatches, "route contains negative index")

	oob := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{10, 1, 0},
	}

	_, _, err = selector.RecordRoutes("peer", oob)
	c.Assert(err, check.ErrorMatches, "route index exceeds available devices or addresses")
}

func (s *SelectorSuite) TestAddRoutesCounts(c *check.C) {
	// not relevant for this test
	identified := func(r assemblestate.DeviceToken) bool { return true }
	sel := assemblestate.NewPrioritySelector("self", nil, identified)

	r := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"self", "peer"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}

	added, total, err := sel.RecordRoutes("peer", r)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 1)
	c.Assert(total, check.Equals, 1)

	// adding same route again should not increase our total count
	added, total, err = sel.RecordRoutes("peer", r)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 0)
	c.Assert(total, check.Equals, 1)

	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"peer", "self"},
		Addresses: []string{"ip"},
		Routes:    []int{1, 0, 0},
	}
	c.Assert(sel.Routes(), check.DeepEquals, expected)
}

func (s *SelectorSuite) TestVerifyRoutes(c *check.C) {
	identified := false
	sel := assemblestate.NewPrioritySelector("self", nil, func(_ assemblestate.DeviceToken) bool {
		return identified
	})

	r := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}

	// identified returns false, so nothing should become verified
	_, _, err := sel.RecordRoutes("peer", r)
	c.Assert(err, check.IsNil)

	routes := sel.Routes()
	c.Assert(routes.Devices, check.HasLen, 0)
	c.Assert(routes.Addresses, check.HasLen, 0)
	c.Assert(routes.Routes, check.HasLen, 0)

	// make all devices report as identified; the route should appear
	identified = true
	sel.VerifyRoutes()
	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}
	c.Assert(sel.Routes(), check.DeepEquals, expected)
}

func (s *SelectorSuite) TestAddAuthoritativeRouteAndSelect(c *check.C) {
	self := assemblestate.DeviceToken("self")
	one := assemblestate.DeviceToken("one")
	two := assemblestate.DeviceToken("two")

	// use deterministic source that always selects index 0 (first peer)
	source := newMockSource(0)
	sel := assemblestate.NewPrioritySelector(self, source, func(_ assemblestate.DeviceToken) bool { return true })

	// self->one authoritative route via ip-1
	sel.AddAuthoritativeRoute(one, "ip-1")

	r := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{self, two},
		Addresses: []string{"ip-2"},
		Routes:    []int{0, 1, 0},
	}
	_, _, err := sel.RecordRoutes(two, r)
	c.Assert(err, check.IsNil)

	routes, ack, ok := sel.Select(one, 100)
	c.Assert(ok, check.Equals, true)
	ack()

	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{self, two, one},
		Addresses: []string{"ip-2", "ip-1"},
		Routes: []int{
			0, 1, 0, // self -> two via ip-2
			0, 2, 1, // self -> one via ip-1 (authoritative)
		},
	}

	c.Assert(routes, check.DeepEquals, expected)
}

func (s *SelectorSuite) TestSelectSelectsLowSourceRoutes(c *check.C) {
	self := assemblestate.DeviceToken("self")
	peer := assemblestate.DeviceToken("peer")

	// use deterministic source to ensure we select peer
	source := newMockSource(0)
	sel := assemblestate.NewPrioritySelector(self, source, func(assemblestate.DeviceToken) bool { return true })

	// add peer first so it gets a lower peerID than the ephemeral sources
	_, _, err := sel.RecordRoutes(peer, assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{peer, self},
		Addresses: []string{"peer-addr"},
		Routes:    []int{0, 1, 0},
	})
	c.Assert(err, check.IsNil)

	// helper to add the same route n times to increment its "seen" counter
	add := func(addr string, bumps int) {
		r := assemblestate.Routes{
			Devices:   []assemblestate.DeviceToken{self, peer},
			Addresses: []string{addr},
			Routes:    []int{0, 1, 0}, // self -> dest via addr
		}

		for i := 0; i < bumps; i++ {
			// we use a different origin each time, so that it looks like
			// multiple of our peers have sent us this route
			src := assemblestate.DeviceToken(fmt.Sprintf("src-%d-%s", i, addr))
			_, _, err := sel.RecordRoutes(src, r)
			c.Assert(err, check.IsNil)
		}
	}

	// add some routes multiple times. since ip-5 is added the most, we should
	// expect to see that it is omitted during publication
	add("ip-0", 1)
	add("ip-1", 1)
	add("ip-2", 2)
	add("ip-3", 2)
	add("ip-4", 3)
	add("ip-5", 4)

	routes, ack, ok := sel.Select(peer, 5)
	c.Assert(ok, check.Equals, true)
	ack()

	// should send the 5 routes with lowest source counts: ip-0, ip-1, ip-2, ip-3, ip-4
	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{self, peer},
		Addresses: []string{"ip-0", "ip-1", "ip-2", "ip-3", "ip-4"},
		Routes: []int{
			0, 1, 0, // self -> peer via ip-0
			0, 1, 1, // self -> peer via ip-1
			0, 1, 2, // self -> peer via ip-2
			0, 1, 3, // self -> peer via ip-3
			0, 1, 4, // self -> peer via ip-4
		},
	}

	c.Assert(routes, check.DeepEquals, expected)
}

func (s *SelectorSuite) TestRoutes(c *check.C) {
	sel := assemblestate.NewPrioritySelector("self", nil, func(assemblestate.DeviceToken) bool { return true })

	in := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"d", "a", "c", "b"},
		Addresses: []string{"ip-3", "ip-1", "ip-2"},
		Routes: []int{
			0, 1, 2, // d->a via ip-2
			1, 3, 0, // a->b via ip-3
			1, 2, 2, // a->c via ip-2
			1, 3, 1, // a->b via ip-1
		},
	}

	_, _, err := sel.RecordRoutes("peer", in)
	c.Assert(err, check.IsNil)

	got := sel.Routes()

	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b", "c", "d"},
		Addresses: []string{"ip-1", "ip-2", "ip-3"},
		Routes: []int{
			0, 1, 0, // a -> b via ip-1
			0, 1, 2, // a -> b via ip-3
			0, 2, 1, // a -> c via ip-2
			3, 0, 1, // d -> a via ip-2
		},
	}

	c.Assert(got, check.DeepEquals, expected)
}

func (s *SelectorSuite) TestRoutesOnlyIdentified(c *check.C) {
	// identified returns false for "unknown" only
	identified := false
	sel := assemblestate.NewPrioritySelector("self", nil, func(r assemblestate.DeviceToken) bool {
		return identified || r != "unknown"
	})

	in := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"unknown", "a", "b"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // unknown->a via ip-1  (should be omitted)
			1, 2, 0, // a->b via ip-1  (should remain)
		},
	}

	_, _, err := sel.RecordRoutes("peer", in)
	c.Assert(err, check.IsNil)

	got := sel.Routes()

	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // a->b via ip-1
		},
	}

	c.Assert(got, check.DeepEquals, expected)

	// update the identified function to return true for all
	identified = true
	sel.VerifyRoutes()

	got = sel.Routes()
	expected = assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b", "unknown"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // a->b via ip-1
			2, 0, 0, // unknown->a via ip-1
		},
	}
	c.Assert(got, check.DeepEquals, expected)
}

func (s *SelectorSuite) TestSelectWithNoPeers(c *check.C) {
	self := assemblestate.DeviceToken("self")
	sel := assemblestate.NewPrioritySelector(self, nil, func(assemblestate.DeviceToken) bool { return true })

	_, _, ok := sel.Select(assemblestate.DeviceToken("nonexistent"), 100)
	c.Assert(ok, check.Equals, false)
}

func (s *SelectorSuite) TestSelectForOurself(c *check.C) {
	self := assemblestate.DeviceToken("self")
	sel := assemblestate.NewPrioritySelector(self, nil, func(assemblestate.DeviceToken) bool { return true })

	_, _, ok := sel.Select(assemblestate.DeviceToken("self"), 100)
	c.Assert(ok, check.Equals, false)
}

func (s *SelectorSuite) TestSelectEverythingKnown(c *check.C) {
	self := assemblestate.DeviceToken("self")
	sel := assemblestate.NewPrioritySelector(self, nil, func(assemblestate.DeviceToken) bool { return true })

	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // a->b via ip-1
		},
	}

	_, _, err := sel.RecordRoutes("peer-1", expected)
	c.Assert(err, check.IsNil)

	_, _, err = sel.RecordRoutes("peer-2", assemblestate.Routes{})
	c.Assert(err, check.IsNil)

	// first time, peer-2 should be sent the routes we know about
	routes, ack, ok := sel.Select(assemblestate.DeviceToken("peer-2"), 100)
	c.Assert(ok, check.Equals, true)
	c.Assert(routes, check.DeepEquals, expected)

	// ack those routes, they shouldn't be sent to peer-2 again
	ack()

	// nothing to send to peer-2
	_, _, ok = sel.Select(assemblestate.DeviceToken("peer-2"), 100)
	c.Assert(ok, check.Equals, false)
}

func (s *SelectorSuite) TestSelectRouteSelectionDeterministic(c *check.C) {
	self := assemblestate.DeviceToken("self")
	peer := assemblestate.DeviceToken("peer")

	// use deterministic source that always selects index 0 (first peer, which is "peer")
	source := newMockSource(0)
	sel := assemblestate.NewPrioritySelector(self, source, func(assemblestate.DeviceToken) bool { return true })

	_, _, err := sel.RecordRoutes(peer, assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{peer, self},
		Addresses: []string{"peer-addr"},
		Routes:    []int{0, 1, 0},
	})
	c.Assert(err, check.IsNil)

	add := func(addr string, bumps int) {
		r := assemblestate.Routes{
			Devices:   []assemblestate.DeviceToken{self, peer},
			Addresses: []string{addr},
			Routes:    []int{0, 1, 0},
		}

		for i := 0; i < bumps; i++ {
			src := assemblestate.DeviceToken(fmt.Sprintf("src-%d-%s", i, addr))
			_, _, err := sel.RecordRoutes(src, r)
			c.Assert(err, check.IsNil)
		}
	}

	add("low-freq", 1)  // should be included
	add("high-freq", 5) // should be excluded

	routes, ack, ok := sel.Select(peer, 1) // limit to 1 route to test prioritization
	c.Assert(ok, check.Equals, true)
	ack()

	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{self, peer},
		Addresses: []string{"low-freq"},
		Routes:    []int{0, 1, 0}, // self -> peer via low-freq
	}

	c.Assert(routes, check.DeepEquals, expected)
}

func (s *SelectorSuite) TestRoutesNoDuplicates(c *check.C) {
	identified := func(r assemblestate.DeviceToken) bool { return true }
	sel := assemblestate.NewPrioritySelector("self", nil, identified)

	// add the same route from different peers to test deduplication
	dupe := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"192.168.1.1:8080"},
		Routes:    []int{0, 1, 0}, // a -> b via 192.168.1.1:8080
	}

	// add the same route from peer-1
	added, total, err := sel.RecordRoutes("peer-1", dupe)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 1)
	c.Assert(total, check.Equals, 1)

	// add the same route from peer-2, should be deduplicated
	added, total, err = sel.RecordRoutes("peer-2", dupe)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 0)
	c.Assert(total, check.Equals, 1)

	// add the same route from peer-3, should be deduplicated
	added, total, err = sel.RecordRoutes("peer-3", dupe)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 0)
	c.Assert(total, check.Equals, 1)

	// add different route combinations with same devices but different
	// addresses
	diffAddr := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"192.168.1.2:8080"},
		Routes:    []int{0, 1, 0}, // a -> b via 192.168.1.2:8080
	}

	added, total, err = sel.RecordRoutes("peer-1", diffAddr)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 1)
	c.Assert(total, check.Equals, 2)

	other := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "c"},
		Addresses: []string{"192.168.1.1:8080"},
		Routes:    []int{0, 1, 0}, // a -> c via 192.168.1.1:8080
	}

	added, total, err = sel.RecordRoutes("peer-2", other)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 1)
	c.Assert(total, check.Equals, 3)

	routes := sel.Routes()
	c.Assert(len(routes.Devices), check.Equals, 3)   // a, b, c
	c.Assert(len(routes.Addresses), check.Equals, 2) // 192.168.1.1:8080, 192.168.1.2:8080
	c.Assert(len(routes.Routes), check.Equals, 9)    // 3 routes * 3 ints each

	c.Assert(routes.Devices, check.DeepEquals, []assemblestate.DeviceToken{"a", "b", "c"})
	c.Assert(routes.Addresses, check.DeepEquals, []string{"192.168.1.1:8080", "192.168.1.2:8080"})

	expected := []int{
		0, 1, 0, // a -> b via 192.168.1.1:8080
		0, 1, 1, // a -> b via 192.168.1.2:8080
		0, 2, 0, // a -> c via 192.168.1.1:8080
	}
	c.Assert(routes.Routes, check.DeepEquals, expected)
}
