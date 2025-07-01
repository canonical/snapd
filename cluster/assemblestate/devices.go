package assemblestate

import (
	"time"
)

type DeviceTracker struct {
	responses chan struct{}
	queries   chan struct{}

	inflight map[RDT]time.Time
	timeout  time.Duration

	unknowns map[RDT]map[RDT]struct{}
	sources  map[RDT]map[RDT]struct{}
	ids      map[RDT]Identity
}

func NewDeviceTracker(self Identity, timeout time.Duration) DeviceTracker {
	return DeviceTracker{
		timeout:   timeout,
		responses: make(chan struct{}, 1),
		queries:   make(chan struct{}, 1),
		unknowns:  make(map[RDT]map[RDT]struct{}),
		sources:   make(map[RDT]map[RDT]struct{}),
		inflight:  make(map[RDT]time.Time),
		ids: map[RDT]Identity{
			self.RDT: self,
		},
	}
}

func (d *DeviceTracker) Responses() <-chan struct{} {
	return d.responses
}

func (d *DeviceTracker) RetryResponses() {
	select {
	case d.responses <- struct{}{}:
	default:
	}
}

func (d *DeviceTracker) RetryQueries() {
	select {
	case d.queries <- struct{}{}:
	default:
	}
}

func (d *DeviceTracker) Queries() <-chan struct{} {
	return d.queries
}

func (d *DeviceTracker) Query(from RDT, unknowns []RDT) {
	if d.unknowns[from] == nil {
		d.unknowns[from] = make(map[RDT]struct{})
	}

	changes := false
	for _, u := range unknowns {
		// if we don't know the requested device's id, then we drop the query
		if _, ok := d.ids[u]; !ok {
			continue
		}

		changes = true
		d.unknowns[from][u] = struct{}{}
	}

	// if we got some queries we can answer, let the other side know there is
	// new data to handle
	if changes {
		select {
		case d.responses <- struct{}{}:
		default:
		}
	}
}

func (d *DeviceTracker) QueryResponses(from RDT) (ids []Identity, ack func()) {
	ids = make([]Identity, 0, len(d.unknowns[from]))
	for rdt := range d.unknowns[from] {
		id, ok := d.ids[rdt]
		if !ok {
			continue
		}
		ids = append(ids, id)
	}

	ack = func() {
		for _, id := range ids {
			delete(d.unknowns[from], id.RDT)
		}
	}

	return ids, ack
}

func (d *DeviceTracker) QueryableFrom(source RDT) (unknown []RDT, ack func()) {
	for rdt := range d.sources[source] {
		if _, ok := d.ids[rdt]; ok {
			continue
		}

		// if we have a query in-flight for this rdt, omit it from the queries
		// we will send
		if time.Since(d.inflight[rdt]) < d.timeout {
			continue
		}

		unknown = append(unknown, rdt)
	}

	ack = func() {
		sent := time.Now()
		for _, u := range unknown {
			d.inflight[u] = sent
		}
	}

	return unknown, ack
}

func (d *DeviceTracker) UpdateSource(source RDT, rdts []RDT) {
	if d.sources[source] == nil {
		d.sources[source] = make(map[RDT]struct{})
	}

	missing := false
	for _, rdt := range rdts {
		d.sources[source][rdt] = struct{}{}
		if _, ok := d.ids[rdt]; !ok {
			missing = true
		}
	}

	// if we are missing some of these routes, let the other side know we have
	// data to request
	if missing {
		select {
		case d.queries <- struct{}{}:
		default:
		}
	}
}

func (d *DeviceTracker) Identified(rdt RDT) bool {
	_, ok := d.ids[rdt]
	return ok
}

func (d *DeviceTracker) Lookup(rdt RDT) (Identity, bool) {
	id, ok := d.ids[rdt]
	return id, ok
}

func (d *DeviceTracker) Identify(id Identity) {
	d.ids[id.RDT] = id
}
