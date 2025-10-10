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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
)

// DeviceQueryTracker manages device identity information and query/response
// lifecycle for unknown devices in the cluster.
type DeviceQueryTracker struct {
	// pendingResponses is used to signal when there are device query responses
	// ready to be sent to peers.
	pendingResponses chan struct{}

	// pendingOutgoingQueries is used to send notifications that indicate that
	// we should query a peer for device information.
	pendingOutgoingQueries chan struct{}

	// inflight keeps track of queries for device information that we have
	// in-flight. We keep track of a mapping of device RDT to time at which the
	// query was sent.
	inflight map[DeviceToken]time.Time

	// timeout is the amount of time we will wait before sending a query for
	// device information again.
	timeout time.Duration

	// queries keeps track of which devices each peer has queried us for. Each
	// DeviceToken maps to the set of devices that are unknown to the peer with
	// that DeviceToken.
	queries map[DeviceToken]map[DeviceToken]struct{}

	// known keeps track of which devices we know each peer knows about. Each
	// DeviceToken maps to the set of devices that the peer with that
	// DeviceToken has identifying information for.
	known map[DeviceToken]map[DeviceToken]struct{}

	// ids is our collection of device identities that we've heard from other
	// peers.
	ids map[DeviceToken]Identity

	// assertDB is used to verify the cryptographic signatures and format
	// of serial assertions in device identities
	assertDB asserts.RODatabase

	// secret is the shared secret used for HMAC calculation during SerialProof validation
	secret string

	// clock enables injecting an implementation that returns the current time.
	clock func() time.Time
}

// NewDeviceQueryTracker creates a new DeviceQueryTracker with the given initial
// data. A timeout can be provided, which will prevent queries from being
// re-sent for the duration of that timeout. Additionally, a clock function can
// can be provided in the case that time.Now needs to be overridden.
// The assertDB parameter is required and must not be nil - all device identities
// will be validated against this assertion database.
func NewDeviceQueryTracker(
	data DeviceQueryTrackerData,
	timeout time.Duration,
	clock func() time.Time,
	assertDB asserts.RODatabase,
	secret string,
) (DeviceQueryTracker, error) {
	dt := DeviceQueryTracker{
		timeout:                timeout,
		pendingResponses:       make(chan struct{}, 1),
		pendingOutgoingQueries: make(chan struct{}, 1),
		queries:                make(map[DeviceToken]map[DeviceToken]struct{}),
		known:                  make(map[DeviceToken]map[DeviceToken]struct{}),
		inflight:               make(map[DeviceToken]time.Time),
		ids:                    make(map[DeviceToken]Identity),
		assertDB:               assertDB,
		secret:                 secret,
		clock:                  clock,
	}

	// seed with any provided data
	for _, identity := range data.IDs {
		if err := dt.RecordIdentity(identity); err != nil {
			return DeviceQueryTracker{}, fmt.Errorf("cannot initialize device query tracker: %w", err)
		}
	}

	for peer, devices := range data.Queries {
		dt.RecordIncomingQuery(peer, devices)
	}

	for peer, devices := range data.Known {
		dt.RecordDevicesKnownBy(peer, devices)
	}

	return dt, nil
}

// PendingResponses returns a channel that signals when there are device query
// responses ready to be sent to peers. After reading from this channel, it is
// expected that all responses are handled via calls to
// [DeviceQueryTracker.ResponsesTo].
func (d *DeviceQueryTracker) PendingResponses() <-chan struct{} {
	return d.pendingResponses
}

// PendingOutgoingQueries returns a channel that signals when there are outgoing
// device queries ready to be sent to peers. After reading from this channel, it
// is expected that all queries are handled via calls to
// [DeviceQueryTracker.OutgoingQueriesTo].
func (d *DeviceQueryTracker) PendingOutgoingQueries() <-chan struct{} {
	return d.pendingOutgoingQueries
}

// RecordIncomingQuery records an incoming query from a peer for unknown device
// identities. If we have identity information for the requested devices, a
// response will be queued.
func (d *DeviceQueryTracker) RecordIncomingQuery(from DeviceToken, unknowns []DeviceToken) {
	if d.queries[from] == nil {
		d.queries[from] = make(map[DeviceToken]struct{})
	}

	changes := false
	for _, u := range unknowns {
		// if we don't know the requested device's id, then we drop the query
		if _, ok := d.ids[u]; !ok {
			continue
		}

		changes = true
		d.queries[from][u] = struct{}{}
	}

	// if we got some queries we can answer, let the other side know there is
	// new data to handle
	if changes {
		select {
		case d.pendingResponses <- struct{}{}:
		default:
		}
	}
}

// ResponsesTo returns the device identities that should be sent to the
// specified peer, along with an acknowledgment function that must be called
// after successful transmission. ack(true) indicates that the responses were
// sent, ack(false) indicates that the responses could not be sent.
func (d *DeviceQueryTracker) ResponsesTo(to DeviceToken) (ids []Identity, ack func(bool)) {
	ids = make([]Identity, 0, len(d.queries[to]))
	for rdt := range d.queries[to] {
		id, ok := d.ids[rdt]
		if !ok {
			// this case shouldn't be possible, since we don't insert into
			// d.queries unless we have the identity of the device
			continue
		}
		ids = append(ids, id)
	}

	ack = func(success bool) {
		// on success, we mark these responses as sent. on failure, we make sure
		// that the response is re-queued
		if success {
			for _, id := range ids {
				delete(d.queries[to], id.RDT)
			}
		} else {
			select {
			case d.pendingResponses <- struct{}{}:
			default:
			}
		}
	}

	return ids, ack
}

// OutgoingQueriesTo returns unknown device RDTs that should be sent as queries
// to the given peer, along with an acknowledgment function that must be called
// after successful transmission to track inflight queries. ack(true) indicates
// that the queries were sent, ack(false) indicates that the queries could not
// be sent.
func (d *DeviceQueryTracker) OutgoingQueriesTo(to DeviceToken) (unknown []DeviceToken, ack func(bool)) {
	for rdt := range d.known[to] {
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
			case d.pendingOutgoingQueries <- struct{}{}:
			default:
			}
		}
	}

	return unknown, ack
}

// RecordDevicesKnownBy records that the given peer has knowledge of the
// specified device RDTs. If any of these devices are unknown to us, a query
// will be queued.
func (d *DeviceQueryTracker) RecordDevicesKnownBy(source DeviceToken, rdts []DeviceToken) {
	if d.known[source] == nil {
		d.known[source] = make(map[DeviceToken]struct{})
	}

	missing := false
	for _, rdt := range rdts {
		d.known[source][rdt] = struct{}{}
		if _, ok := d.ids[rdt]; !ok {
			missing = true
		}
	}

	// if we are missing some of these devices, let the other side know we have
	// data to request
	if missing {
		select {
		case d.pendingOutgoingQueries <- struct{}{}:
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

func verifySerialBundle(bundle string, db asserts.RODatabase) (*asserts.Serial, error) {
	if bundle == "" {
		return nil, errors.New("serial bundle is empty")
	}

	tmpDB := db.WithStackedBackstore(asserts.NewMemoryBackstore())
	batch := asserts.NewBatch(nil)

	if _, err := batch.AddStream(strings.NewReader(bundle)); err != nil {
		return nil, err
	}

	var serials []*asserts.Serial
	observe := func(a asserts.Assertion) {
		if s, ok := a.(*asserts.Serial); ok {
			serials = append(serials, s)
		}
	}

	if err := batch.CommitToAndObserve(tmpDB, observe, nil); err != nil {
		return nil, err
	}

	if len(serials) != 1 {
		return nil, errors.New("exactly one serial assertion expected in bundle")
	}

	return serials[0], nil
}

func (d *DeviceQueryTracker) validateID(id Identity) error {
	serial, err := verifySerialBundle(id.SerialBundle, d.assertDB)
	if err != nil {
		return fmt.Errorf("invalid identity for device %s: %w", id.RDT, err)
	}

	if len(id.SerialProof) == 0 {
		return fmt.Errorf("device %s has empty serial proof", id.RDT)
	}

	// extract device public key from serial assertion
	key := serial.DeviceKey()

	// calculate the HMAC that should have been signed
	expectedHMAC := CalculateHMAC(id.RDT, id.FP, d.secret)

	// verify the SerialProof is a valid signature of the HMAC
	if err := asserts.RawVerifyWithKey(expectedHMAC, id.SerialProof, key); err != nil {
		return fmt.Errorf("serial proof verification failed for device %s: %w", id.RDT, err)
	}

	return nil
}

// RecordIdentity records identity information for a device.
func (d *DeviceQueryTracker) RecordIdentity(id Identity) error {
	if err := d.validateID(id); err != nil {
		return fmt.Errorf("invalid serial assertion for device %s: %w", id.RDT, err)
	}

	d.ids[id.RDT] = id
	return nil
}

// DeviceQueryTrackerData represents the serializable state of
// DeviceQueryTracker.
type DeviceQueryTrackerData struct {
	Queries map[DeviceToken][]DeviceToken `json:"unknowns,omitempty"`
	Known   map[DeviceToken][]DeviceToken `json:"sources,omitempty"`
	IDs     []Identity                    `json:"ids,omitempty"`
}

// Export returns the serializable state of the DeviceQueryTracker.
func (d *DeviceQueryTracker) Export() DeviceQueryTrackerData {
	data := DeviceQueryTrackerData{
		Queries: make(map[DeviceToken][]DeviceToken),
		Known:   make(map[DeviceToken][]DeviceToken),
		IDs:     make([]Identity, 0, len(d.ids)),
	}

	for peer, devices := range d.queries {
		for device := range devices {
			data.Queries[peer] = append(data.Queries[peer], device)
		}
	}

	for peer, devices := range d.known {
		for device := range devices {
			data.Known[peer] = append(data.Known[peer], device)
		}
	}

	for _, identity := range d.ids {
		data.IDs = append(data.IDs, identity)
	}

	// if anything is empty, just clear it out. makes testing easier.
	if len(data.Queries) == 0 {
		data.Queries = nil
	}
	if len(data.Known) == 0 {
		data.Known = nil
	}
	if len(data.IDs) == 0 {
		data.IDs = nil
	}

	return data
}
