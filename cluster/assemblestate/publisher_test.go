package assemblestate_test

import (
	"fmt"
	"testing"

	"github.com/snapcore/snapd/cluster/assemblestate"
	"gopkg.in/check.v1"
)

type PublisherSuite struct{}

var _ = check.Suite(&PublisherSuite{})

func Test(t *testing.T) { check.TestingT(t) }

func (s *PublisherSuite) TestAddRoutesValidation(c *check.C) {
	publisher := assemblestate.NewPriorityPublisher("self")

	// not relevant for this test
	identified := func(r assemblestate.RDT) bool { return true }

	invalid := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1},
	}

	_, _, err := publisher.AddRoutes("peer", invalid, identified)
	c.Assert(err, check.ErrorMatches, "length of routes list must be a multiple of three")

	negative := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, -1, 0},
	}

	_, _, err = publisher.AddRoutes("peer", negative, identified)
	c.Assert(err, check.ErrorMatches, "invalid index in routes")

	oob := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{10, 1, 0},
	}

	_, _, err = publisher.AddRoutes("peer", oob, identified)
	c.Assert(err, check.ErrorMatches, "invalid index in routes")
}

func (s *PublisherSuite) TestAddRoutesCounts(c *check.C) {
	p := assemblestate.NewPriorityPublisher("self")

	// not relevant for this test
	identified := func(r assemblestate.RDT) bool { return true }

	r := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"self", "peer"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}

	added, total, err := p.AddRoutes("peer", r, identified)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 1)
	c.Assert(total, check.Equals, 1)

	// adding same route again should not increase our total count
	added, total, err = p.AddRoutes("peer", r, identified)
	c.Assert(err, check.IsNil)
	c.Assert(added, check.Equals, 0)
	c.Assert(total, check.Equals, 1)

	expected := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"peer", "self"},
		Addresses: []string{"ip"},
		Routes:    []int{1, 0, 0},
	}
	c.Assert(p.Routes(), check.DeepEquals, expected)
}

func (s *PublisherSuite) TestVerifyRoutes(c *check.C) {
	p := assemblestate.NewPriorityPublisher("self")

	r := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}

	// identified returns false, so nothing should become verified
	_, _, err := p.AddRoutes("peer", r, func(_ assemblestate.RDT) bool { return false })
	c.Assert(err, check.IsNil)

	routes := p.Routes()
	c.Assert(routes.Devices, check.HasLen, 0)
	c.Assert(routes.Addresses, check.HasLen, 0)
	c.Assert(routes.Routes, check.HasLen, 0)

	// verify all devices; the route should appear
	p.VerifyRoutes(func(_ assemblestate.RDT) bool { return true })
	expected := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b"},
		Addresses: []string{"ip"},
		Routes:    []int{0, 1, 0},
	}
	c.Assert(p.Routes(), check.DeepEquals, expected)
}

func (s *PublisherSuite) TestAddAuthoritativeRouteAndPublish(c *check.C) {
	self := assemblestate.RDT("self")
	one := assemblestate.RDT("one")
	two := assemblestate.RDT("two")

	p := assemblestate.NewPriorityPublisher(self)

	// self->one authoritative route via ip-1
	p.AddAuthoritativeRoute(one, "ip-1")

	r := assemblestate.Routes{
		Devices:   []assemblestate.RDT{self, two},
		Addresses: []string{"ip-2"},
		Routes:    []int{0, 1, 0},
	}
	_, _, err := p.AddRoutes(two, r, func(_ assemblestate.RDT) bool { return true })
	c.Assert(err, check.IsNil)

	// these shouldn't matter for this test, make them bigger than we need
	const (
		maxPeers  = 10
		maxRoutes = 100
	)

	var sent *assemblestate.Routes
	p.Publish(func(to assemblestate.RDT, routes assemblestate.Routes) error {
		if to != one {
			return nil
		}
		sent = &routes
		return nil
	}, maxPeers, maxRoutes)

	// ensure we sent something to one
	c.Assert(sent, check.NotNil)

	expected := assemblestate.Routes{
		Devices:   []assemblestate.RDT{self, two, one}, // appearance order in edges
		Addresses: []string{"ip-2", "ip-1"},            // appearance order in edges
		Routes: []int{
			0, 1, 0, // self->two via ip-2
			0, 2, 1, // self->one via ip-1
		},
	}

	c.Assert(*sent, check.DeepEquals, expected)
}

func (s *PublisherSuite) TestPublishPrioritizesLowSourceRoutes(c *check.C) {
	self := assemblestate.RDT("self")
	peer := assemblestate.RDT("peer")

	p := assemblestate.NewPriorityPublisher(self)

	// helper to add the same route n times to increment its "seen" counter
	add := func(addr string, bumps int) {
		r := assemblestate.Routes{
			Devices:   []assemblestate.RDT{self, peer},
			Addresses: []string{addr},
			Routes:    []int{0, 1, 0}, // self -> dest via addr
		}

		for i := 0; i < bumps; i++ {
			// we use a different origin each time, so that it looks like
			// multiple of our peers have sent us this route
			src := assemblestate.RDT(fmt.Sprintf("src-%d-%s", i, addr))
			_, _, err := p.AddRoutes(src, r, func(assemblestate.RDT) bool { return true })
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

	const (
		// the 1 peer that is picked should be our peer that doesn't know
		// anything
		maxPeers = 1
		// the 5 routes that are picked should be the routes that we've seen the
		// fewest number of times. thus,
		maxRoutes = 5
	)

	var sent *assemblestate.Routes
	p.Publish(func(to assemblestate.RDT, routes assemblestate.Routes) error {
		if to != peer {
			return nil
		}
		sent = &routes
		return nil
	}, maxPeers, maxRoutes)

	// we expect that we'll send this route to "peer", since it is missing the
	// most routes
	c.Assert(sent, check.NotNil)

	expected := assemblestate.Routes{
		Devices:   []assemblestate.RDT{self, peer},
		Addresses: []string{"ip-0", "ip-1", "ip-2", "ip-3", "ip-4"},
		Routes: []int{
			0, 1, 0,
			0, 1, 1,
			0, 1, 2,
			0, 1, 3,
			0, 1, 4,
		},
	}

	c.Assert(*sent, check.DeepEquals, expected)
}

func (s *PublisherSuite) TestRoutes(c *check.C) {
	p := assemblestate.NewPriorityPublisher("self")

	in := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"d", "a", "c", "b"},
		Addresses: []string{"ip-3", "ip-1", "ip-2"},
		Routes: []int{
			0, 1, 2, // d->a via ip-2
			1, 3, 0, // a->b via ip-3
			1, 2, 2, // a->c via ip-2
			1, 3, 1, // a->b via ip-1
		},
	}

	_, _, err := p.AddRoutes("peer", in, func(assemblestate.RDT) bool { return true })
	c.Assert(err, check.IsNil)

	got := p.Routes()

	expected := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b", "c", "d"},
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

func (s *PublisherSuite) TestRoutesOnlyIdentified(c *check.C) {
	p := assemblestate.NewPriorityPublisher("self")

	in := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"unknown", "a", "b"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // unknown->a via ip-1  (should be omitted)
			1, 2, 0, // a->b via ip-1  (should remain)
		},
	}

	// identified returns false for "unknown" only
	identified := func(r assemblestate.RDT) bool { return r != "unknown" }

	_, _, err := p.AddRoutes("peer", in, identified)
	c.Assert(err, check.IsNil)

	got := p.Routes()

	expected := assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // a->b via ip-1
		},
	}

	c.Assert(got, check.DeepEquals, expected)

	p.VerifyRoutes(func(assemblestate.RDT) bool { return true })

	got = p.Routes()
	expected = assemblestate.Routes{
		Devices:   []assemblestate.RDT{"a", "b", "unknown"},
		Addresses: []string{"ip-1"},
		Routes: []int{
			0, 1, 0, // a->b via ip-1
			2, 0, 0, // unknown->a via ip-1
		},
	}
	c.Assert(got, check.DeepEquals, expected)
}
