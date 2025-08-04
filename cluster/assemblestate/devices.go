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
	inflight map[DeviceToken]time.Time

	// timeout is the amount of time we will wait before sending a query for
	// device information again.
	timeout time.Duration

	// unknowns keeps track of which devices each peer has queried us for.
	unknowns map[DeviceToken]map[DeviceToken]struct{}

	// sources keeps track of which devices we know each peer knows about.
	sources map[DeviceToken]map[DeviceToken]struct{}

	// ids is our collection of device identities that we've heard from other
	// peers.
	ids map[DeviceToken]Identity

	clock func() time.Time
}

// NewDeviceQueryTracker creates a new DeviceQueryTracker with the given
// identity, timeout for inflight queries, and initial data.
func NewDeviceQueryTracker(
	self Identity,
	timeout time.Duration,
	clock func() time.Time,
	data DeviceQueryTrackerData,
) DeviceQueryTracker {
	dt := DeviceQueryTracker{
		timeout:   timeout,
		responses: make(chan struct{}, 1),
		queries:   make(chan struct{}, 1),
		unknowns:  make(map[DeviceToken]map[DeviceToken]struct{}),
		sources:   make(map[DeviceToken]map[DeviceToken]struct{}),
		inflight:  make(map[DeviceToken]time.Time),
		ids: map[DeviceToken]Identity{
			self.RDT: self,
		},
		clock: clock,
	}

	// seed with any provided data
	for _, identity := range data.IDs {
		dt.RecordIdentity(identity)
	}

	for peer, devices := range data.Unknowns {
		dt.RecordQuery(peer, devices)
	}

	for peer, devices := range data.Sources {
		dt.UpdateSource(peer, devices)
	}

	return dt
}

// PendingResponses returns a channel that signals when there are device
// query responses ready to be sent to peers. After reading from this channel,
// it is expected that all responses are handled via calls to
// [DeviceQueryTracker.ResponsesTo].
func (d *DeviceQueryTracker) PendingResponses() <-chan struct{} {
	return d.responses
}

// PendingQueries returns a channel that signals when there are device
// queries ready to be sent to peers. After reading from this channel, it is
// expected that all queries are handled via calls to
// [DeviceQueryTracker.QueriesTo].
func (d *DeviceQueryTracker) PendingQueries() <-chan struct{} {
	return d.queries
}

// RecordQuery records a query from a peer for unknown device identities. If we
// have identity information for the requested devices, a response will be
// queued.
func (d *DeviceQueryTracker) RecordQuery(from DeviceToken, unknowns []DeviceToken) {
	if d.unknowns[from] == nil {
		d.unknowns[from] = make(map[DeviceToken]struct{})
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

// ResponsesTo returns the device identities that should be sent to the
// specified peer, along with an acknowledgment function that must be called
// after successful transmission. ack(true) indicates that they responses were
// sent, ack(false) indicates that the responses could not be sent.
func (d *DeviceQueryTracker) ResponsesTo(to DeviceToken) (ids []Identity, ack func(bool)) {
	ids = make([]Identity, 0, len(d.unknowns[to]))
	for rdt := range d.unknowns[to] {
		id, ok := d.ids[rdt]
		if !ok {
			continue
		}
		ids = append(ids, id)
	}

	ack = func(success bool) {
		// on success, we mark these responses as sent. on failure, we make sure
		// that the response is re-queued
		if success {
			for _, id := range ids {
				delete(d.unknowns[to], id.RDT)
			}
		} else {
			select {
			case d.responses <- struct{}{}:
			default:
			}
		}
	}

	return ids, ack
}

// QueriesTo returns unknown device RDTs that should be sent as queries to the
// given peer, along with an acknowledgment function that must be called after
// successful transmission to track inflight queries. ack(true) indicates that
// they queries were sent, ack(false) indicates that the queries could not be
// sent.
func (d *DeviceQueryTracker) QueriesTo(to DeviceToken) (unknown []DeviceToken, ack func(bool)) {
	for rdt := range d.sources[to] {
		if _, ok := d.ids[rdt]; ok {
			continue
		}

		// if we have a query in-flight for this rdt, omit it from the queries
		// we will send
		if d.inflight[rdt].Add(d.timeout).After(d.clock()) {
			continue
		}

		unknown = append(unknown, rdt)
	}

	ack = func(success bool) {
		// on success, we mark these queries as in-flight. on failure, we make
		// sure that the query is re-queued
		if success {
			sent := d.clock()
			for _, u := range unknown {
				d.inflight[u] = sent
			}
		} else {
			select {
			case d.queries <- struct{}{}:
			default:
			}
		}
	}

	return unknown, ack
}

// UpdateSource records that a source peer has knowledge of the specified device
// RDTs. If any of these devices are unknown to us, a query will be queued.
func (d *DeviceQueryTracker) UpdateSource(source DeviceToken, rdts []DeviceToken) {
	if d.sources[source] == nil {
		d.sources[source] = make(map[DeviceToken]struct{})
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
func (d *DeviceQueryTracker) Identified(rdt DeviceToken) bool {
	_, ok := d.ids[rdt]
	return ok
}

// Lookup returns the identity information for the specified device RDT, or
// false if the device is unknown.
func (d *DeviceQueryTracker) Lookup(rdt DeviceToken) (Identity, bool) {
	id, ok := d.ids[rdt]
	return id, ok
}

// RecordIdentity records identity information for a device.
func (d *DeviceQueryTracker) RecordIdentity(id Identity) {
	d.ids[id.RDT] = id
}

// DeviceQueryTrackerData represents the serializable state of
// DeviceQueryTracker.
type DeviceQueryTrackerData struct {
	Unknowns map[DeviceToken][]DeviceToken `json:"unknowns,omitempty"`
	Sources  map[DeviceToken][]DeviceToken `json:"sources,omitempty"`
	IDs      map[DeviceToken]Identity      `json:"ids,omitempty"`
}

// Export returns the serializable state of the DeviceQueryTracker.
func (d *DeviceQueryTracker) Export() DeviceQueryTrackerData {
	data := DeviceQueryTrackerData{
		Unknowns: make(map[DeviceToken][]DeviceToken),
		Sources:  make(map[DeviceToken][]DeviceToken),
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
