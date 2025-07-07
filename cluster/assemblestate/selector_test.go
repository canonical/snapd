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
	selector := assemblestate.NewPrioritySelector("self", nil)

	// not relevant for this test
	identified := func(r assemblestate.DeviceToken) bool { return true }

	invalid := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1},
	}

	_, _, err := selector.RecordRoutes("peer", invalid, identified)
	c.Assert(err, check.ErrorMatches, "length of routes list must be a multiple of three")

	negative := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, -1, 0},
	}

	_, _, err = selector.RecordRoutes("peer", negative, identified)
	c.Assert(err, check.ErrorMatches, "invalid index in routes")

	oob := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{10, 1, 0},
	}

	_, _, err = selector.RecordRoutes("peer", oob, identified)
	c.Assert(err, check.ErrorMatches, "invalid index in routes")
}

func (s *SelectorSuite) TestAddRoutesCounts(c *check.C) {
	sel := assemblestate.NewPrioritySelector("self", nil)

	// not relevant for this test
	identified := func(r assemblestate.DeviceToken) bool { return true }

	r := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"self", "peer"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}

	added, total, err := sel.RecordRoutes("peer", r, identified)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 1)
	c.Assert(total, check.Equals, 1)

	// adding same route again should not increase our total count
	added, total, err = sel.RecordRoutes("peer", r, identified)
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
	sel := assemblestate.NewPrioritySelector("self", nil)

	r := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}

	// identified returns false, so nothing should become verified
	_, _, err := sel.RecordRoutes("peer", r, func(_ assemblestate.DeviceToken) bool { return false })
	c.Assert(err, check.IsNil)

	routes := sel.Routes()
	c.Assert(routes.Devices, check.HasLen, 0)
	c.Assert(routes.Addresses, check.HasLen, 0)
	c.Assert(routes.Routes, check.HasLen, 0)

	// verify all devices; the route should appear
	sel.VerifyRoutes(func(_ assemblestate.DeviceToken) bool { return true })
	expected := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}
	c.Assert(sel.Routes(), check.DeepEquals, expected)
}

func (s *SelectorSuite) TestAddAuthoritativeRouteAndPublish(c *check.C) {
	self := assemblestate.DeviceToken("self")
	one := assemblestate.DeviceToken("one")
	two := assemblestate.DeviceToken("two")

	// use deterministic source that always selects index 0 (first peer)
	source := newMockSource(0)
	sel := assemblestate.NewPrioritySelector(self, source)

	// self->one authoritative route via ip-1
	sel.AddAuthoritativeRoute(one, "ip-1")

	r := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{self, two},
		Addresses: []string{"ip-2"},
		Routes:    []int{0, 1, 0},
	}
	_, _, err := sel.RecordRoutes(two, r, func(_ assemblestate.DeviceToken) bool { return true })
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

	c.Assert(&routes, check.DeepEquals, &expected)
}

func (s *SelectorSuite) TestPublishSelectsLowSourceRoutes(c *check.C) {
	self := assemblestate.DeviceToken("self")
	peer := assemblestate.DeviceToken("peer")

	// use deterministic source to ensure we select peer
	source := newMockSource(0)
	sel := assemblestate.NewPrioritySelector(self, source)

	// add peer first so it gets a lower peerID than the ephemeral sources
	_, _, err := sel.RecordRoutes(peer, assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{peer, self},
		Addresses: []string{"peer-addr"},
		Routes:    []int{0, 1, 0},
	}, func(assemblestate.DeviceToken) bool { return true })
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
			_, _, err := sel.RecordRoutes(src, r, func(assemblestate.DeviceToken) bool { return true })
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

	c.Assert(&routes, check.DeepEquals, &expected)
}

func (s *SelectorSuite) TestRoutes(c *check.C) {
	sel := assemblestate.NewPrioritySelector("self", nil)

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

	_, _, err := sel.RecordRoutes("peer", in, func(assemblestate.DeviceToken) bool { return true })
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
	sel := assemblestate.NewPrioritySelector("self", nil)

	in := assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{"unknown", "a", "b"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // unknown->a via ip-1  (should be omitted)
			1, 2, 0, // a->b via ip-1  (should remain)
		},
	}

	// identified returns false for "unknown" only
	identified := func(r assemblestate.DeviceToken) bool { return r != "unknown" }

	_, _, err := sel.RecordRoutes("peer", in, identified)
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

	sel.VerifyRoutes(func(assemblestate.DeviceToken) bool { return true })

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

func (s *SelectorSuite) TestPublishWithNoPeers(c *check.C) {
	self := assemblestate.DeviceToken("self")
	sel := assemblestate.NewPrioritySelector(self, nil)

	_, _, ok := sel.Select(assemblestate.DeviceToken("nonexistent"), 100)
	c.Assert(ok, check.Equals, false)
}

func (s *SelectorSuite) TestPublishRouteSelectionDeterministic(c *check.C) {
	self := assemblestate.DeviceToken("self")
	peer := assemblestate.DeviceToken("peer")

	// use deterministic source that always selects index 0 (first peer, which is "peer")
	source := newMockSource(0)
	sel := assemblestate.NewPrioritySelector(self, source)

	_, _, err := sel.RecordRoutes(peer, assemblestate.Routes{
		Devices:   []assemblestate.DeviceToken{peer, self},
		Addresses: []string{"peer-addr"},
		Routes:    []int{0, 1, 0},
	}, func(assemblestate.DeviceToken) bool { return true })
	c.Assert(err, check.IsNil)

	add := func(addr string, bumps int) {
		r := assemblestate.Routes{
			Devices:   []assemblestate.DeviceToken{self, peer},
			Addresses: []string{addr},
			Routes:    []int{0, 1, 0},
		}

		for i := 0; i < bumps; i++ {
			src := assemblestate.DeviceToken(fmt.Sprintf("src-%d-%s", i, addr))
			_, _, err := sel.RecordRoutes(src, r, func(assemblestate.DeviceToken) bool { return true })
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

	c.Assert(&routes, check.DeepEquals, &expected)
}
