package assemblestate

import (
	"cmp"
	"errors"
	"sort"

	"github.com/snapcore/snapd/cluster/assemblestate/bimap"
	"github.com/snapcore/snapd/cluster/assemblestate/bitset"
)

// RoutePublisher keeps track of which routes we've seen and helps pick which
// peers to publish routes to.
type RoutePublisher interface {
	// AddAuthoritativeRoute records a route from this local node to the given
	// [RDT]. This route will be published to the given peer, regardless of our
	// knowledge of that peer's identity.
	AddAuthoritativeRoute(from RDT, via string)

	// AddRoutes records a set of routes from the given [RDT]. The given
	// function identified is used to determine which new routes can be
	// considered for publication.
	AddRoutes(from RDT, r Routes, identified func(RDT) bool) (int, int, error)

	// VerifyRoutes re-calculates which routes are available for publication.
	// The given function identified is used to determine which routes should be
	// considered.
	VerifyRoutes(identified func(RDT) bool)

	// Publish picks a subset of known (not trusted, the [RoutePublisher] does
	// not deal with trust) peers that routes should be published to. For each
	// peer selected, a subset of routes that they need to receive is picked.
	//
	// The given send function is called for each selected peer with their
	// selected routes.
	Publish(send func(to RDT, r Routes) error, maxPeers, maxRoutes int)

	// Routes returns all routes that are currently valid for publication.
	Routes() Routes
}

// PriorityPublisher implements [RoutePublisher].
//
// This implementation prioritizes peers that this local node believes are
// missing the most routes. Additionally, we prioritize sending routes that this
// local node has witnessed and published the smallest number of times.
type PriorityPublisher struct {
	// self is this node's RDT.
	self RDT

	// rdts keeps track of all RDTs that we've seen and maps them to a [peerID].
	rdts *bimap.Bimap[RDT, peerID]

	// edges keeps track of all edges we've seen and maps them to an [edgeID].
	edges *bimap.Bimap[edge, edgeID]

	// addresses keeps track of all addresses we've seen and maps them to an
	// [addrID].
	addresses *bimap.Bimap[string, addrID]

	// peers keeps track which routes each peer knows about. A route is
	// considered known by a peer if either they have sent it to us, or we've
	// sent it to them.
	known map[peerID]*bitset.Bitset[edgeID]

	// sources keeps track of how many unqiue peers we've seen an edge from and
	// sent an edge to. This helps us prioritize which routes to send to our
	// peers.
	sources map[edgeID]int

	// verified keeps track of which edges are safe to publish. These routes
	// only include RDTs for devices that are reported as identified by our
	// caller.
	verified *bitset.Bitset[edgeID]

	// authoritative keeps track of the set of edges from this local node to
	// each other peer. Each of these can be safely sent to the destination node
	// in the edge.
	authoritative map[RDT]edgeID
}

func NewPriorityPublisher(self RDT) *PriorityPublisher {
	return &PriorityPublisher{
		self:          self,
		rdts:          bimap.New[RDT, peerID](),
		edges:         bimap.New[edge, edgeID](),
		addresses:     bimap.New[string, addrID](),
		verified:      &bitset.Bitset[edgeID]{},
		known:         make(map[peerID]*bitset.Bitset[edgeID]),
		sources:       make(map[edgeID]int),
		authoritative: make(map[RDT]edgeID),
	}
}

type (
	// peerID is an opaque identifier that represents a peer. This is used to
	// intern our strings to limit memory usage.
	peerID int
	// edgeID is an opaque identifier that represents an edge. This is used to
	// intern our strings to limit memory usage.
	edgeID int
	// addrID is an opaque identifier that represents an address. This is used
	// to intern our strings to limit memory usage.
	addrID int
)

type edge struct {
	from, to peerID
	via      addrID
}

func (m *PriorityPublisher) peerID(rdt RDT) peerID {
	if pid, ok := m.rdts.IndexOf(rdt); ok {
		return pid
	}

	pid := m.rdts.Add(rdt)
	m.known[pid] = &bitset.Bitset[edgeID]{}

	return pid
}

func (m *PriorityPublisher) addrID(a string) addrID {
	return m.addresses.Add(a)
}

func (m *PriorityPublisher) edgeID(e edge) (edgeID, bool) {
	if eid, ok := m.edges.IndexOf(e); ok {
		return eid, true
	}

	eid := m.edges.Add(e)

	return eid, false
}

// AddAuthoritativeRoute informs the publisher of an authoritative route from
// this local node to the given peer. This route can safely be published to the
// given peer, regardless of our knowledge of that peer's identity.
func (m *PriorityPublisher) AddAuthoritativeRoute(to RDT, via string) {
	eid, _ := m.edgeID(edge{
		from: m.peerID(m.self),
		to:   m.peerID(to),
		via:  m.addrID(via),
	})
	m.authoritative[to] = eid
}

// AddRoutes records all give routes and marks them as known to the given [RDT].
// The provided identified function is used to verify routes and mark them as
// safe to publish if all we know all devices involved in a route.
func (m *PriorityPublisher) AddRoutes(source RDT, r Routes, identified func(RDT) bool) (added int, total int, err error) {
	pid := m.peerID(source)

	if len(r.Routes)%3 != 0 {
		return 0, 0, errors.New("length of routes list must be a multiple of three")
	}

	for i := 0; i+2 < len(r.Routes); i += 3 {
		if r.Routes[i] < 0 || r.Routes[i+1] < 0 || r.Routes[i+2] < 0 {
			return 0, 0, errors.New("invalid index in routes")
		}

		if r.Routes[i] >= len(r.Devices) || r.Routes[i+1] >= len(r.Devices) || r.Routes[i+2] >= len(r.Addresses) {
			return 0, 0, errors.New("invalid index in routes")
		}

		fromRDT := r.Devices[r.Routes[i]]
		toRDT := r.Devices[r.Routes[i+1]]

		fromID := m.peerID(fromRDT)
		toID := m.peerID(toRDT)
		viaID := m.addrID(r.Addresses[r.Routes[i+2]])

		eid, existed := m.edgeID(edge{
			from: fromID,
			to:   toID,
			via:  viaID,
		})
		if !existed {
			added++
		}

		if !m.known[pid].Has(eid) {
			m.sources[eid]++
		}

		m.known[pid].Set(eid)

		if identified(fromRDT) && identified(toRDT) {
			m.verified.Set(eid)
		}
	}

	return added, len(m.edges.Values()), nil
}

// VerifyRoutes uses the given identified function to mark any routes that
// involve devices that we know as safe to publish.
func (p *PriorityPublisher) VerifyRoutes(identified func(RDT) bool) {
	for eid, edge := range p.edges.Values() {
		fromRDT := p.rdts.Value(edge.from)
		toRDT := p.rdts.Value(edge.to)

		if identified(fromRDT) && identified(toRDT) {
			p.verified.Set(edgeID(eid))
		}
	}
}

// Publish calls the given send function for a subset of our peers, with routes
// that should be published to that peer.
//
// This implementation prioritizes peers that this node believes are missing the
// most routes. Additionally, we prioritize routes that this local node has
// witnessed and published fewer times.
func (p *PriorityPublisher) Publish(send func(RDT, Routes) error, maxPeers, maxRoutes int) {
	type pi struct {
		id      peerID
		missing int
	}

	var peers []pi
	for pid, bs := range p.known {
		if p.rdts.Value(pid) == p.self {
			continue
		}

		peers = append(peers, pi{
			id:      pid,
			missing: p.verified.Count() - bs.Count(),
		})
	}

	if len(peers) == 0 {
		return
	}

	// prioritize peers that are missing more of our known routes. we might want
	// to consider getting rid of this, since this would result in a late joiner
	// getting a bunch of routes immediately from every peer.
	sort.Slice(peers, func(i, j int) bool { return peers[i].missing > peers[j].missing })

	// sending is pre-allocated and reused here between iterations to reduce
	// allocations. max possible needed size is all verified routes + the route
	// from this local node to the destination peer
	sending := make([]edgeID, 0, p.verified.Count()+1)
	for _, pinfo := range peers[:min(maxPeers, len(peers))] {
		sending = sending[:0]

		peerKnown := p.known[pinfo.id]
		peerRDT := p.rdts.Value(pinfo.id)

		// only consider routes that we don't think that this peer knows about
		unknown := p.verified.Diff(peerKnown)
		unknown.Range(func(eid edgeID) bool {
			// if we've verified more routes than we originally allocated for,
			// just handle them on the next publication. check len+2 since we
			// add the route here and the authoritative route below.
			if len(sending)+2 > cap(sending) {
				return false
			}

			sending = append(sending, eid)
			return true
		})

		// prioritize routes that we think fewer peers know about. this takes
		// into account copies of routes we've seen from our peers and copies
		// that we've sent to our peers
		sort.Slice(sending, func(i, j int) bool { return p.sources[sending[i]] < p.sources[sending[j]] })
		sending = sending[:min(len(sending), maxRoutes)]

		// if we have an authoritative route for this peer, make sure to include
		// it
		if eid, ok := p.authoritative[peerRDT]; ok && !peerKnown.Has(eid) {
			sending = append(sending, eid)
		}

		if len(sending) == 0 {
			continue
		}

		// note that send unlocks the lock that protects our internal data
		// structures. it is relocked before send returns. things might have
		// changed on our next iteration of the loop. this is ok, since edge and
		// peer IDs are never invalidated.
		//
		// we could invert this to return a [Routes], but that would increase
		// complexity in other ways (and would be a bit wasteful in terms of
		// performance).
		if err := send(peerRDT, p.edgesToRoutes(sending)); err != nil {
			continue
		}

		for _, eid := range sending {
			// the peer might know about the route already due to the unlocking
			// that happens in send. if that has happened, then we don't want to
			// double count that peer as a source.
			if !peerKnown.Has(eid) {
				peerKnown.Set(eid)
				p.sources[eid]++
			}
		}
	}
}

// Routes returns routes that we've seen for which both peers in the route
// identified.
func (p *PriorityPublisher) Routes() Routes {
	eids := p.verified.All()

	devs := make(map[string]struct{})
	addrs := make(map[string]struct{})

	for _, eid := range eids {
		edge := p.edges.Value(eid)
		devs[string(p.rdts.Value(edge.from))] = struct{}{}
		devs[string(p.rdts.Value(edge.to))] = struct{}{}
		addrs[p.addresses.Value(edge.via)] = struct{}{}
	}

	devices := keys(devs)
	sort.Strings(devices)

	addresses := keys(addrs)
	sort.Strings(addresses)

	sort.Slice(eids, func(i, j int) bool {
		a, b := p.edges.Value(eids[i]), p.edges.Value(eids[j])

		if p.rdts.Value(a.from) != p.rdts.Value(b.from) {
			return p.rdts.Value(a.from) < p.rdts.Value(b.from)
		}

		if p.rdts.Value(a.to) != p.rdts.Value(b.to) {
			return p.rdts.Value(a.to) < p.rdts.Value(b.to)
		}

		return p.addresses.Value(a.via) < p.addresses.Value(b.via)
	})

	routes := make([]int, 0, len(eids)*3)
	for _, eid := range eids {
		edge := p.edges.Value(eid)
		from := sort.SearchStrings(devices, string(p.rdts.Value(edge.from)))
		to := sort.SearchStrings(devices, string(p.rdts.Value(edge.to)))
		via := sort.SearchStrings(addresses, p.addresses.Value(edge.via))

		routes = append(routes,
			from,
			to,
			via,
		)
	}

	converted := make([]RDT, 0, len(devices))
	for _, d := range devices {
		converted = append(converted, RDT(d))
	}

	return Routes{
		Devices:   converted,
		Addresses: addresses,
		Routes:    routes,
	}
}

func (p *PriorityPublisher) edgesToRoutes(edges []edgeID) Routes {
	rdts := bimap.New[RDT, int]()
	addrs := bimap.New[string, int]()

	routes := make([]int, 0, len(edges)*3)
	for _, eid := range edges {
		edge := p.edges.Value(eid)

		from := p.rdts.Value(edge.from)
		to := p.rdts.Value(edge.to)
		address := p.addresses.Value(edge.via)

		routes = append(routes,
			rdts.Add(from),
			rdts.Add(to),
			addrs.Add(address),
		)
	}

	return Routes{
		Devices:   rdts.Values(),
		Addresses: addrs.Values(),
		Routes:    routes,
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func min[T cmp.Ordered](x T, y T) T {
	if x < y {
		return x
	}
	return y
}
