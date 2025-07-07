package assemblestate

import (
	"time"
)

// DeviceQueryTracker manages device identity information and query/response
// lifecycle for unknown devices in the cluster.
type DeviceQueryTracker struct {
	// responses is used to send notifications that indicate that we are able to
	// respond to a peer's query for device information.
	responses chan struct{}

	// queries is used to send notifications that indicate that we should query
	// a peer for device information.
	queries chan struct{}

	// inflight keeps track of queries for device information that we have
	// in-flight. We keep track of a mapping of device RDT to time at which the
	// query was sent.
	inflight map[RDT]time.Time

	// timeout is the amount of time we will wait before sending a query for
	// device information again.
	timeout time.Duration

	// unknowns keeps track of which devices each peer has queried us for.
	unknowns map[RDT]map[RDT]struct{}

	// sources keeps track of which devices we know each peer knows about.
	sources map[RDT]map[RDT]struct{}

	// ids is our collection of device identities that we've heard from other
	// peers.
	ids map[RDT]Identity
}

// NewDeviceQueryTracker creates a new DeviceQueryTracker with the given
// identity, timeout for inflight queries, and initial data.
func NewDeviceQueryTracker(self Identity, timeout time.Duration, data DeviceQueryTrackerData) DeviceQueryTracker {
	dt := DeviceQueryTracker{
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

	// seed with any provided data
	for rdt, identity := range data.IDs {
		dt.ids[rdt] = identity
	}

	for peer, devices := range data.Unknowns {
		dt.unknowns[peer] = make(map[RDT]struct{}, len(devices))
		for _, device := range devices {
			dt.unknowns[peer][device] = struct{}{}
		}
	}

	for peer, devices := range data.Sources {
		dt.sources[peer] = make(map[RDT]struct{}, len(devices))
		for _, device := range devices {
			dt.sources[peer][device] = struct{}{}
		}
	}

	return dt
}

// Responses returns a channel that signals when there are device query
// responses ready to be sent to peers.
func (d *DeviceQueryTracker) Responses() <-chan struct{} {
	return d.responses
}

// RetryResponses signals that device query responses should be retried. This
// should be called when a response transmission fails.
func (d *DeviceQueryTracker) RetryResponses() {
	select {
	case d.responses <- struct{}{}:
	default:
	}
}

// Queries returns a channel that signals when there are device queries ready to
// be sent to peers.
func (d *DeviceQueryTracker) Queries() <-chan struct{} {
	return d.queries
}

// RetryQueries signals that device queries should be retried. This should be
// called when a query transmission fails.
func (d *DeviceQueryTracker) RetryQueries() {
	select {
	case d.queries <- struct{}{}:
	default:
	}
}

// Query records a query from a peer for unknown device identities. If we have
// identity information for the requested devices, a response will be queued.
func (d *DeviceQueryTracker) Query(from RDT, unknowns []RDT) {
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

// QueryResponses returns the device identities that should be sent to the
// specified peer, along with an acknowledgment function that must be called
// after successful transmission.
func (d *DeviceQueryTracker) QueryResponses(from RDT) (ids []Identity, ack func()) {
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

// QueryableFrom returns unknown device RDTs that should be queried from the
// specified source peer, along with an acknowledgment function that must be
// called after successful transmission to track inflight queries.
func (d *DeviceQueryTracker) QueryableFrom(source RDT) (unknown []RDT, ack func()) {
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

// UpdateSource records that a source peer has knowledge of the specified device
// RDTs. If any of these devices are unknown to us, a query will be queued.
func (d *DeviceQueryTracker) UpdateSource(source RDT, rdts []RDT) {
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

// Identified returns whether we have identity information for the specified
// device RDT.
func (d *DeviceQueryTracker) Identified(rdt RDT) bool {
	_, ok := d.ids[rdt]
	return ok
}

// Lookup returns the identity information for the specified device RDT, or
// false if the device is unknown.
func (d *DeviceQueryTracker) Lookup(rdt RDT) (Identity, bool) {
	id, ok := d.ids[rdt]
	return id, ok
}

// Identify records identity information for a device.
func (d *DeviceQueryTracker) Identify(id Identity) {
	d.ids[id.RDT] = id
}

// DeviceQueryTrackerData represents the serializable state of
// DeviceQueryTracker.
type DeviceQueryTrackerData struct {
	Unknowns map[RDT][]RDT    `json:"unknowns"`
	Sources  map[RDT][]RDT    `json:"sources"`
	IDs      map[RDT]Identity `json:"ids"`
}

// Export returns the serializable state of the DeviceQueryTracker.
func (d *DeviceQueryTracker) Export() DeviceQueryTrackerData {
	data := DeviceQueryTrackerData{
		Unknowns: make(map[RDT][]RDT),
		Sources:  make(map[RDT][]RDT),
		IDs:      d.ids,
	}

	for peer, devices := range d.unknowns {
		for device := range devices {
			data.Unknowns[peer] = append(data.Unknowns[peer], device)
		}
	}

	for peer, devices := range d.sources {
		for device := range devices {
			data.Sources[peer] = append(data.Sources[peer], device)
		}
	}

	return data
}
