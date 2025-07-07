package assemblestate

import (
	"errors"
	"sort"
	"time"

	"github.com/snapcore/snapd/cluster/assemblestate/bimap"
	"github.com/snapcore/snapd/cluster/assemblestate/bitset"
	"github.com/snapcore/snapd/randutil"
)

// RouteSelector keeps track of which routes we've seen and helps pick which
// peers to publish routes to.
type RouteSelector interface {
	// AddAuthoritativeRoute records a route from this local node to the given
	// [RDT]. This route will be published to the given peer, regardless of our
	// knowledge of that peer's identity.
	AddAuthoritativeRoute(from DeviceToken, via string)

	// AddRoutes records a set of routes from the given [RDT]. The given
	// function identified is used to determine which new routes can be
	// considered for publication.
	AddRoutes(from DeviceToken, r Routes, identified func(DeviceToken) bool) (int, int, error)

	// VerifyRoutes re-calculates which routes are available for publication.
	// The given function identified is used to determine which routes should be
	// considered.
	VerifyRoutes(identified func(DeviceToken) bool)

	// Select selects a subset of routes that the specified peer needs to receive.
	//
	// Returns the routes to send, an acknowledgment function that should be
	// called after successful transmission, and whether routes were selected.
	// The ack function must be called once the selected routes are published
	// so that they will not be selected for publication again.
	Select(to DeviceToken, count int) (routes Routes, ack func(), ok bool)

	// Routes returns all routes that are currently valid for publication.
	Routes() Routes
}

// PrioritySelector implements [RouteSelector].
//
// This implementation randomly selects one peer for route publication.
// Additionally, we prioritize routes that this local node has witnessed and
// published the smallest number of times.
type PrioritySelector struct {
	// self is this node's RDT.
	self DeviceToken

	// rng is the random number generator that is used for peer selection.
	rng *randutil.Rand

	// rdts keeps track of all RDTs that we've seen and maps them to a [peerID].
	rdts *bimap.Bimap[DeviceToken, peerID]

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
	authoritative map[DeviceToken]edgeID
}

func NewPrioritySelector(self DeviceToken, source randutil.Source) *PrioritySelector {
	if source == nil {
		source = randutil.NewSource(time.Now().UnixNano())
	}

	return &PrioritySelector{
		self:          self,
		rng:           randutil.New(source),
		rdts:          bimap.New[DeviceToken, peerID](),
		edges:         bimap.New[edge, edgeID](),
		addresses:     bimap.New[string, addrID](),
		verified:      &bitset.Bitset[edgeID]{},
		known:         make(map[peerID]*bitset.Bitset[edgeID]),
		sources:       make(map[edgeID]int),
		authoritative: make(map[DeviceToken]edgeID),
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

func (m *PrioritySelector) peerID(rdt DeviceToken) peerID {
	if pid, ok := m.rdts.IndexOf(rdt); ok {
		return pid
	}

	pid := m.rdts.Add(rdt)
	m.known[pid] = &bitset.Bitset[edgeID]{}

	return pid
}

func (m *PrioritySelector) addrID(a string) addrID {
	return m.addresses.Add(a)
}

func (m *PrioritySelector) edgeID(e edge) (edgeID, bool) {
	if eid, ok := m.edges.IndexOf(e); ok {
		return eid, true
	}

	eid := m.edges.Add(e)

	return eid, false
}

// AddAuthoritativeRoute informs the selector of an authoritative route from
// this local node to the given peer. This route can safely be published to the
// given peer, regardless of our knowledge of that peer's identity.
func (m *PrioritySelector) AddAuthoritativeRoute(to DeviceToken, via string) {
	eid, _ := m.edgeID(edge{
		from: m.peerID(m.self),
		to:   m.peerID(to),
		via:  m.addrID(via),
	})
	m.authoritative[to] = eid
}

// AddRoutes records all give routes and marks them as known to the given [DeviceToken].
// The provided identified function is used to verify routes and mark them as
// safe to publish if all we know all devices involved in a route.
func (m *PrioritySelector) AddRoutes(source DeviceToken, r Routes, identified func(DeviceToken) bool) (added int, total int, err error) {
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
func (p *PrioritySelector) VerifyRoutes(identified func(DeviceToken) bool) {
	for eid, edge := range p.edges.Values() {
		fromRDT := p.rdts.Value(edge.from)
		toRDT := p.rdts.Value(edge.to)

		if identified(fromRDT) && identified(toRDT) {
			p.verified.Set(edgeID(eid))
		}
	}
}

// Select selects routes that should be published to the specified peer.
//
// We prioritize routes that this local node has witnessed and published fewer
// times.
func (p *PrioritySelector) Select(to DeviceToken, count int) (routes Routes, ack func(), ok bool) {
	selected, exists := p.rdts.IndexOf(to)
	if !exists {
		return Routes{}, nil, false
	}

	if to == p.self {
		return Routes{}, nil, false
	}

	peerKnown := p.known[selected]
	unknown := p.verified.Diff(peerKnown)

	// max possible needed size is all unknown routes + the route from this
	// local node to the destination peer
	sending := make([]edgeID, 0, unknown.Count()+1)

	// only consider routes that we don't think that this peer knows about
	unknown.Range(func(eid edgeID) bool {
		sending = append(sending, eid)
		return true
	})

	// prioritize routes that we think fewer peers know about. this takes into
	// account copies of routes we've seen from our peers and copies that we've
	// sent to our peers
	sort.Slice(sending, func(i, j int) bool { return p.sources[sending[i]] < p.sources[sending[j]] })
	sending = sending[:min(len(sending), count)]

	// if we have an authoritative route for this peer, make sure to include it
	if eid, ok := p.authoritative[to]; ok && !peerKnown.Has(eid) {
		sending = append(sending, eid)
	}

	if len(sending) == 0 {
		return Routes{}, nil, false
	}

	routes = p.edgesToRoutes(sending)

	// create ack function that updates source counts when called
	ack = func() {
		for _, eid := range sending {
			// the peer might know about the route already since selection time.
			// if that has happened, then we don't want to double count that
			// peer as a source.
			if !peerKnown.Has(eid) {
				peerKnown.Set(eid)
				p.sources[eid]++
			}
		}
	}

	return routes, ack, true
}

// Routes returns routes that we've seen for which both peers in the route
// identified.
func (p *PrioritySelector) Routes() Routes {
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

	converted := make([]DeviceToken, 0, len(devices))
	for _, d := range devices {
		converted = append(converted, DeviceToken(d))
	}

	return Routes{
		Devices:   converted,
		Addresses: addresses,
		Routes:    routes,
	}
}

func (p *PrioritySelector) edgesToRoutes(edges []edgeID) Routes {
	rdts := bimap.New[DeviceToken, int]()
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

func min(x int, y int) int {
	if x < y {
		return x
	}
	return y
}
