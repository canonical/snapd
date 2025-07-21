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
	// [DeviceToken]. This route will be published to the given peer, regardless
	// of our knowledge of that peer's identity.
	//
	// We treat these routes slightly differently because of how the protocol
	// defines what peers should do upon receipt of routes. If we send a route
	// to our peer that includes devices that they do not yet know, it is
	// expected that they can request identifying information for those devices
	// from us. Since a peer always knows about itself, it is safe for us to
	// publish an authoritative route to the peer at the route's destination.
	AddAuthoritativeRoute(to DeviceToken, via string)

	// RecordRoutes records a set of routes from the given [RDT]. If a route's
	// origin and destination both are reported as known by the given identified
	// function, then those routes can be considered for publication.
	RecordRoutes(from DeviceToken, r Routes, identified func(DeviceToken) bool) (int, int, error)

	// VerifyRoutes re-calculates which routes are available for publication.
	// For all routes that are already known, they will be marked as available
	// for publication if the given identified function reports that both the
	// origin and destination of the route are known.
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

	// knownByPeers keeps track which routes each peer knows about. A route is
	// considered known by a peer if either they have sent it to us, or we've
	// sent it to them.
	knownByPeers map[peerID]*bitset.Bitset[edgeID]

	// edgeSources keeps track of how many unique peers we've seen an edge from
	// and sent an edge to. This helps us prioritize which routes to send to our
	// peers.
	edgeSources map[edgeID]int

	// verifiedEdges keeps track of which edges are safe to publish. These routes
	// only include RDTs for devices that are reported as identified by our
	// caller.
	verifiedEdges *bitset.Bitset[edgeID]

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
		verifiedEdges: &bitset.Bitset[edgeID]{},
		knownByPeers:  make(map[peerID]*bitset.Bitset[edgeID]),
		edgeSources:   make(map[edgeID]int),
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

func (p *PrioritySelector) peerID(rdt DeviceToken) peerID {
	if pid, ok := p.rdts.IndexOf(rdt); ok {
		return pid
	}

	pid := p.rdts.Add(rdt)
	p.knownByPeers[pid] = &bitset.Bitset[edgeID]{}

	return pid
}

func (p *PrioritySelector) addrID(a string) addrID {
	return p.addresses.Add(a)
}

func (p *PrioritySelector) edgeID(e edge) (id edgeID, existed bool) {
	if eid, ok := p.edges.IndexOf(e); ok {
		return eid, true
	}

	return p.edges.Add(e), false
}

// AddAuthoritativeRoute informs the selector of an authoritative route from
// this local node to the given peer. This route can safely be published to the
// given peer, regardless of our knowledge of that peer's identity.
func (p *PrioritySelector) AddAuthoritativeRoute(to DeviceToken, via string) {
	eid, _ := p.edgeID(edge{
		from: p.peerID(p.self),
		to:   p.peerID(to),
		via:  p.addrID(via),
	})
	p.authoritative[to] = eid
}

// RecordRoutes records all give routes and marks them as known to the given [DeviceToken].
// The provided identified function is used to verify routes and mark them as
// safe to publish if all we know all devices involved in a route.
func (p *PrioritySelector) RecordRoutes(source DeviceToken, r Routes, identified func(DeviceToken) bool) (added int, total int, err error) {
	pid := p.peerID(source)

	if len(r.Routes)%3 != 0 {
		return 0, 0, errors.New("length of routes list must be a multiple of three")
	}

	// r.Routes is a slice triplets, where each triplet represents a route
	// between two devices in our cluster. the values in triplet are indexes
	// into the other slices in the [Routes] message.
	//   - r.Routes[n] is an index into r.Devices, representing the origin of the
	//     route
	//   - r.Routes[n+1] is an index into r.Devices, representing the
	//     destination of the route
	//   - r.Routes[n+1] is an index into r.Addresses, representing the address
	//     that r.Routes[n] used to reach r.Routes[n+1]
	for i := 0; i+2 < len(r.Routes); i += 3 {
		if r.Routes[i] < 0 || r.Routes[i+1] < 0 || r.Routes[i+2] < 0 {
			return 0, 0, errors.New("route contains negative index")
		}

		if r.Routes[i] >= len(r.Devices) || r.Routes[i+1] >= len(r.Devices) || r.Routes[i+2] >= len(r.Addresses) {
			return 0, 0, errors.New("route index exceeds available devices or addresses")
		}

		fromRDT := r.Devices[r.Routes[i]]
		toRDT := r.Devices[r.Routes[i+1]]

		fromID := p.peerID(fromRDT)
		toID := p.peerID(toRDT)
		viaID := p.addrID(r.Addresses[r.Routes[i+2]])

		eid, existed := p.edgeID(edge{
			from: fromID,
			to:   toID,
			via:  viaID,
		})
		if !existed {
			added++
		}

		// if we aren't aware that this peer knows about this route already,
		// increment our counter of sources for that edge
		if !p.knownByPeers[pid].Has(eid) {
			p.edgeSources[eid]++
		}

		// record that the peer who sent this Routes message knows about this
		// edge
		p.knownByPeers[pid].Set(eid)

		// if we have the identities of both the from and to devices, then we
		// know can verify this route. verified routes can published to our
		// peers.
		if identified(fromRDT) && identified(toRDT) {
			p.verifiedEdges.Set(eid)
		}
	}

	return added, len(p.edges.Values()), nil
}

// VerifyRoutes uses the given identified function to mark any routes that
// involve devices that we know as safe to publish.
func (p *PrioritySelector) VerifyRoutes(identified func(DeviceToken) bool) {
	for eid, edge := range p.edges.Values() {
		fromRDT := p.rdts.Value(edge.from)
		toRDT := p.rdts.Value(edge.to)

		if identified(fromRDT) && identified(toRDT) {
			p.verifiedEdges.Set(edgeID(eid))
		}
	}
}

// Select n selects routes that should be published to the specified peer.
//
// We prioritize routes that this local node has witnessed and published fewer
// times.
func (p *PrioritySelector) Select(to DeviceToken, n int) (routes Routes, ack func(), ok bool) {
	selected, exists := p.rdts.IndexOf(to)
	if !exists {
		return Routes{}, nil, false
	}

	if to == p.self {
		return Routes{}, nil, false
	}

	peerKnown := p.knownByPeers[selected]
	unknown := p.verifiedEdges.Diff(peerKnown)

	// max possible needed size is all unknown routes + the route from this
	// local node to the destination peer
	edgesToSend := make([]edgeID, 0, unknown.Count()+1)

	// only consider routes that we don't think that this peer knows about
	unknown.Range(func(eid edgeID) bool {
		edgesToSend = append(edgesToSend, eid)
		return true
	})

	// prioritize routes that we think fewer peers know about. this takes into
	// account copies of routes we've seen from our peers and copies that we've
	// sent to our peers
	sort.Slice(edgesToSend, func(i, j int) bool { return p.edgeSources[edgesToSend[i]] < p.edgeSources[edgesToSend[j]] })

	// discard any edges causing us to exceed the given threshold. thus, we pick
	// the n least frequently seen routes.
	edgesToSend = edgesToSend[:min(len(edgesToSend), n)]

	// if we have an authoritative route for this peer, make sure to include it
	if eid, ok := p.authoritative[to]; ok && !peerKnown.Has(eid) {
		edgesToSend = append(edgesToSend, eid)
	}

	if len(edgesToSend) == 0 {
		return Routes{}, nil, false
	}

	routes = p.edgesToRoutes(edgesToSend)

	// create ack function that updates source counts when called
	ack = func() {
		for _, eid := range edgesToSend {
			// the peer might know about the route already since selection time.
			// if that has happened, then we don't want to double count that
			// peer as a source.
			if !peerKnown.Has(eid) {
				peerKnown.Set(eid)
				p.edgeSources[eid]++
			}
		}
	}

	return routes, ack, true
}

// Routes returns routes that we've seen for which both peers in the route
// identified.
func (p *PrioritySelector) Routes() Routes {
	eids := p.verifiedEdges.All()

	devs := make(map[string]bool)
	addrs := make(map[string]bool)

	for _, eid := range eids {
		edge := p.edges.Value(eid)
		devs[string(p.rdts.Value(edge.from))] = true
		devs[string(p.rdts.Value(edge.to))] = true
		addrs[p.addresses.Value(edge.via)] = true
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

// keys returns a slice of the keys in the given map.
//
// TODO: remove once we are on go>=1.21
func keys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// min returns the smaller of the two given values.
//
// TODO: remove once we are on go>=1.21
func min(x int, y int) int {
	if x < y {
		return x
	}
	return y
}
