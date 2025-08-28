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
	"sort"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/cluster/assemblestate"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/testutil"
)

type deviceTrackerSuite struct{}

var _ = check.Suite(&deviceTrackerSuite{})

// mockAssertDB creates a mock assertion database for testing
func mockAssertDB(c *check.C) (*asserts.Database, *assertstest.StoreStack) {
	signing := assertstest.NewStoreStack("canonical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   signing.Trusted,
	})
	c.Assert(err, check.IsNil)

	err = db.Add(signing.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	return db, signing
}

func createTestIdentity(
	c *check.C,
	signing *assertstest.StoreStack,
	rdt assemblestate.DeviceToken,
	fp assemblestate.Fingerprint,
	secret string,
) assemblestate.Identity {
	assertion, key := createTestSerial(c, signing)

	// create SerialProof using the same device key
	hmac := assemblestate.CalculateHMAC(rdt, fp, secret)
	proof, err := asserts.RawSignWithKey(hmac, key)
	c.Assert(err, check.IsNil)

	return assemblestate.Identity{
		RDT:         rdt,
		FP:          fp,
		Serial:      string(asserts.Encode(assertion)),
		SerialProof: proof,
	}
}

// createTestSerial creates a mock serial assertion and device key
func createTestSerial(
	c *check.C,
	signing *assertstest.StoreStack,
) (*asserts.Serial, asserts.PrivateKey) {
	// create a device key for the serial assertion
	key, _ := assertstest.GenerateKey(752)
	pubkey, err := asserts.EncodePublicKey(key.PublicKey())
	c.Assert(err, check.IsNil)

	headers := map[string]any{
		"authority-id":        "canonical",
		"brand-id":            "canonical",
		"model":               "test-model",
		"serial":              randutil.RandomString(10),
		"device-key":          string(pubkey),
		"device-key-sha3-384": key.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}

	assertion, err := signing.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	s, ok := assertion.(*asserts.Serial)
	c.Assert(ok, check.Equals, true)
	return s, key
}

// createExpiredSerial creates a serial assertion with an expired timestamp
func createExpiredSerial(c *check.C, signing *assertstest.StoreStack) (asserts.Assertion, asserts.PrivateKey) {
	// create a device key for the serial assertion
	key, _ := assertstest.GenerateKey(752)
	pubkey, err := asserts.EncodePublicKey(key.PublicKey())
	c.Assert(err, check.IsNil)

	headers := map[string]any{
		"authority-id":        "canonical",
		"brand-id":            "canonical",
		"model":               "test-model",
		"serial":              randutil.RandomString(10),
		"device-key":          string(pubkey),
		"device-key-sha3-384": key.PublicKey().ID(),
		"timestamp":           time.Now().AddDate(-2, 0, 0).Format(time.RFC3339),
	}

	assertion, err := signing.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	s, ok := assertion.(*asserts.Serial)
	c.Assert(ok, check.Equals, true)
	return s, key
}

func (s *deviceTrackerSuite) TestDeviceTrackerLookup(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// initially no devices are identified
	c.Assert(dt.Identified("self"), check.Equals, false)
	c.Assert(dt.Identified("other"), check.Equals, false)

	// add a device identity
	self := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("self"),
		assemblestate.CalculateFP([]byte("self")),
		"test-secret",
	)
	err = dt.RecordIdentity(self)
	c.Assert(err, check.IsNil)

	c.Assert(dt.Identified("self"), check.Equals, true)
	c.Assert(dt.Identified("other"), check.Equals, false)

	id, ok := dt.Lookup("self")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, self)

	_, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, false)

	// add another device identity
	other := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("other"),
		assemblestate.CalculateFP([]byte("other")),
		"test-secret",
	)
	err = dt.RecordIdentity(other)
	c.Assert(err, check.IsNil)

	c.Assert(dt.Identified("other"), check.Equals, true)
	id, ok = dt.Lookup("other")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, other)
}

func (s *deviceTrackerSuite) TestDeviceTrackerQueries(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	self := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("self"),
		assemblestate.CalculateFP([]byte("self")),
		"test-secret",
	)
	err = dt.RecordIdentity(self)
	c.Assert(err, check.IsNil)

	other := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("other"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	err = dt.RecordIdentity(other)
	c.Assert(err, check.IsNil)

	// peer queries us for devices
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"self", "other"})

	// should signal responses channel
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, true)

	ids, ack := dt.ResponsesTo("peer")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.DeviceToken, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"self", "other"})

	// ack should remove the queries
	const success = true
	ack(success)

	// should have no more responses
	ids, _ = dt.ResponsesTo("peer")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerDropUnknownQuery(c *check.C) {
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(assemblestate.DeviceQueryTrackerData{}, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// peer queries us for unknown device
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"unknown"})

	// should not signal responses channel
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	// query should not be recorded at all
	c.Assert(dt.Export(), check.DeepEquals, assemblestate.DeviceQueryTrackerData{})
}

func (s *deviceTrackerSuite) TestDeviceTrackerSources(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// peers tells us they know about some devices
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.RecordDevicesKnownBy("other", []assemblestate.DeviceToken{"device-1", "device-2"})

	// should signal queries channel (we don't know these devices)
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, true)

	// should have queries for this peer
	unknown, ack := dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should mark as in flight
	const success = true
	ack(success)

	// since we have queries in flight, we shouldn't report new queries from
	// this peer
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 0)

	unknown, _ = dt.OutgoingQueriesTo("other")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerTimeout(c *check.C) {

	// expected calls from: QueryableFrom, ack, QueryableFrom, QueryableFrom
	// this mocks time passing, enabling us to test queries for device IDs that
	// are in flight
	called := 0
	now := time.Now()
	results := []time.Time{now, now, now.Add(time.Second / 2), now.Add(time.Second)}
	clock := func() time.Time {
		t := results[called]
		called++
		return t
	}

	data := assemblestate.DeviceQueryTrackerData{}
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Second, clock, db, "test-secret")
	c.Assert(err, check.IsNil)

	// peer tells us they know about device
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"device-1"})

	unknown, ack := dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 1)

	// ack marks as in flight
	const success = true
	ack(success)

	// since the query is in flight, we shouldn't return anything here
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 0)

	// should return query again after the timeout expires
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerFailedQueryAck(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Hour, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// peer tells us they know about device
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"device-1"})

	unknown, ack := dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))

	// ack with false indicates that we could not send the query
	const failure = false
	ack(failure)

	// signal indicates that there are still queries available to send
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, true)

	// since we failed to send the query, it should be returned again here
	unknown, ack = dt.OutgoingQueriesTo("peer")
	c.Assert(len(unknown), check.Equals, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-1"))

	const success = true
	ack(success)

	// signal indicates that there is nothing to send
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, false)

	// now that we've marked it as a successful send, it should not be returned
	// here
	unknown, _ = dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerFailedResponseAck(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Hour, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	self := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("self"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	err = dt.RecordIdentity(self)
	c.Assert(err, check.IsNil)

	one := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-1"),
		assemblestate.Fingerprint{6, 7, 8, 9, 10},
		"test-secret",
	)
	err = dt.RecordIdentity(one)
	c.Assert(err, check.IsNil)

	// peer tells us they need info about a device
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"device-1"})

	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, true)

	ids, ack := dt.ResponsesTo("peer")
	c.Assert(len(ids), check.Equals, 1)
	c.Assert(ids[0].RDT, check.Equals, one.RDT)
	c.Assert(ids[0].Serial, check.Equals, one.Serial)

	// ack with false indicates that we could not send the query
	const failure = false
	ack(failure)

	// signal indicates that there are still responses available to send
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, true)

	// since we failed to send the response, it should be returned again here
	ids, ack = dt.ResponsesTo("peer")
	c.Assert(len(ids), check.Equals, 1)
	c.Assert(ids[0].RDT, check.Equals, one.RDT)
	c.Assert(ids[0].Serial, check.Equals, one.Serial)

	const success = true
	ack(success)

	// now no response signal
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	ids, _ = dt.ResponsesTo("peer")
	c.Assert(len(ids), check.Equals, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerEmptyQueries(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// empty query shouldn't signal channel
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{})
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	// should handle empty peer queries correctly
	ids, _ := dt.ResponsesTo("unknown")
	c.Assert(ids, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerUnknownDevices(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// query for unknown device should be ignored
	dt.RecordIncomingQuery("peer", []assemblestate.DeviceToken{"unknown"})
	c.Assert(hasSignal(dt.PendingResponses()), check.Equals, false)

	other := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("other"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	err = dt.RecordIdentity(other)
	c.Assert(err, check.IsNil)

	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"other", "unknown"})

	// should skip devices we already know
	unknown, _ := dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("unknown"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerNoMissingDevices(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// add device we know about
	other := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("other"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	err = dt.RecordIdentity(other)
	c.Assert(err, check.IsNil)

	// source update with only known devices shouldn't signal
	dt.RecordDevicesKnownBy("peer", []assemblestate.DeviceToken{"other"})
	c.Assert(hasSignal(dt.PendingOutgoingQueries()), check.Equals, false)

	// should return empty when all devices are known
	unknown, _ := dt.OutgoingQueriesTo("peer")
	c.Assert(unknown, check.HasLen, 0)
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededIDs(c *check.C) {
	db, signing := mockAssertDB(c)

	self := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("self"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	one := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-1"),
		assemblestate.Fingerprint{6, 7, 8, 9, 10},
		"test-secret",
	)
	two := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-2"),
		assemblestate.Fingerprint{11, 12, 13, 14, 15},
		"test-secret",
	)

	data := assemblestate.DeviceQueryTrackerData{
		IDs: []assemblestate.Identity{
			self,
			one,
			two,
		},
	}
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// preseeded devices should be identified
	c.Assert(dt.Identified("device-1"), check.Equals, true)
	c.Assert(dt.Identified("device-2"), check.Equals, true)

	// should be able to lookup preseeded devices
	id, ok := dt.Lookup("device-1")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, one)

	id, ok = dt.Lookup("device-2")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, two)

	// self should still be identified
	c.Assert(dt.Identified("self"), check.Equals, true)
	id, ok = dt.Lookup("self")
	c.Assert(ok, check.Equals, true)
	c.Assert(id, check.DeepEquals, self)
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededUnknowns(c *check.C) {
	db, signing := mockAssertDB(c)
	one := createTestIdentity(
		c,
		signing,
		assemblestate.DeviceToken("device-1"),
		assemblestate.Fingerprint{6, 7, 8, 9, 10},
		"test-secret",
	)
	two := createTestIdentity(
		c,
		signing,
		assemblestate.DeviceToken("device-2"),
		assemblestate.Fingerprint{11, 12, 13, 14, 15},
		"test-secret",
	)

	data := assemblestate.DeviceQueryTrackerData{
		IDs: []assemblestate.Identity{
			one,
			two,
		},
		Queries: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-1"},
		},
	}

	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// should have responses for preseeded unknowns
	ids, ack := dt.ResponsesTo("peer-1")
	c.Assert(ids, check.HasLen, 2)

	rdts := make([]assemblestate.DeviceToken, 0, len(ids))
	for _, id := range ids {
		rdts = append(rdts, id.RDT)
	}
	c.Assert(rdts, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should clear the responses
	const success = true
	ack(success)

	ids, _ = dt.ResponsesTo("peer-1")
	c.Assert(ids, check.HasLen, 0)

	// peer-2 should have responses for device-1 only
	ids, _ = dt.ResponsesTo("peer-2")
	c.Assert(ids, check.HasLen, 1)
	c.Assert(ids[0].RDT, check.Equals, assemblestate.DeviceToken("device-1"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerPreseededSources(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{
		Known: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"device-3"},
		},
	}

	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// should be able to query from preseeded sources
	unknown, ack := dt.OutgoingQueriesTo("peer-1")
	c.Assert(unknown, testutil.DeepUnsortedMatches, []assemblestate.DeviceToken{"device-1", "device-2"})

	// ack should mark as in flight
	const success = true
	ack(success)

	// should not return same queries while in flight
	unknown, _ = dt.OutgoingQueriesTo("peer-1")
	c.Assert(unknown, check.HasLen, 0)

	// peer-2 should have separate queries
	unknown, _ = dt.OutgoingQueriesTo("peer-2")
	c.Assert(unknown, check.HasLen, 1)
	c.Assert(unknown[0], check.Equals, assemblestate.DeviceToken("device-3"))
}

func (s *deviceTrackerSuite) TestDeviceTrackerExport(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	self := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("self"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	err = dt.RecordIdentity(self)
	c.Assert(err, check.IsNil)

	one := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-1"),
		assemblestate.Fingerprint{6, 7, 8, 9, 10},
		"test-secret",
	)
	err = dt.RecordIdentity(one)
	c.Assert(err, check.IsNil)

	two := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-2"),
		assemblestate.Fingerprint{11, 12, 13, 14, 15},
		"test-secret",
	)
	err = dt.RecordIdentity(two)
	c.Assert(err, check.IsNil)

	dt.RecordIncomingQuery("peer-1", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.RecordIncomingQuery("peer-2", []assemblestate.DeviceToken{"self"})
	dt.RecordDevicesKnownBy("peer-1", []assemblestate.DeviceToken{"device-3", "device-4"})
	dt.RecordDevicesKnownBy("peer-2", []assemblestate.DeviceToken{"device-5"})

	exported := dt.Export()
	normalizeDeviceExport(exported)

	expected := assemblestate.DeviceQueryTrackerData{
		IDs: []assemblestate.Identity{
			one,
			two,
			self,
		},
		Queries: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-1", "device-2"},
			"peer-2": {"self"},
		},
		Known: map[assemblestate.DeviceToken][]assemblestate.DeviceToken{
			"peer-1": {"device-3", "device-4"},
			"peer-2": {"device-5"},
		},
	}

	c.Assert(exported, check.DeepEquals, expected)
}

func (s *deviceTrackerSuite) TestDeviceTrackerExportRoundtrip(c *check.C) {
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(assemblestate.DeviceQueryTrackerData{}, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	self := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("self"),
		assemblestate.Fingerprint{1, 2, 3, 4, 5},
		"test-secret",
	)
	err = dt.RecordIdentity(self)
	c.Assert(err, check.IsNil)

	one := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-1"),
		assemblestate.Fingerprint{6, 7, 8, 9, 10},
		"test-secret",
	)
	err = dt.RecordIdentity(one)
	c.Assert(err, check.IsNil)

	two := createTestIdentity(
		c, signing,
		assemblestate.DeviceToken("device-2"),
		assemblestate.Fingerprint{11, 12, 13, 14, 15},
		"test-secret",
	)
	err = dt.RecordIdentity(two)
	c.Assert(err, check.IsNil)

	dt.RecordIncomingQuery("peer-1", []assemblestate.DeviceToken{"device-1", "device-2"})
	dt.RecordIncomingQuery("peer-2", []assemblestate.DeviceToken{"device-1"})
	dt.RecordDevicesKnownBy("peer-1", []assemblestate.DeviceToken{"device-3", "device-4"})
	dt.RecordDevicesKnownBy("peer-2", []assemblestate.DeviceToken{"device-5", "device-6"})

	// export the current state
	initial := dt.Export()
	normalizeDeviceExport(initial)

	// create a new tracker from the exported data
	dt, err = assemblestate.NewDeviceQueryTracker(initial, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// export again and verify it matches
	exported := dt.Export()
	normalizeDeviceExport(exported)

	c.Assert(exported, check.DeepEquals, initial)
}

func (s *deviceTrackerSuite) TestDeviceTrackerWithAssertionValidation(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// should reject empty serial assertion
	empty := assemblestate.Identity{
		RDT:    assemblestate.DeviceToken("empty-serial"),
		Serial: "",
	}
	err = dt.RecordIdentity(empty)
	c.Assert(err, check.ErrorMatches, ".*cannot decode serial assertion.*")

	malformed := assemblestate.Identity{
		RDT:    assemblestate.DeviceToken("malformed-serial"),
		Serial: "not-a-valid-assertion",
	}
	err = dt.RecordIdentity(malformed)
	c.Assert(err, check.ErrorMatches, "invalid serial assertion for device malformed-serial: .*")
}

func (s *deviceTrackerSuite) TestRecordIdentityWithValidAssertion(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	serial, key := createTestSerial(c, signing)
	rdt := assemblestate.DeviceToken("test-device")
	f := assemblestate.Fingerprint{1, 2, 3, 4, 5}

	hmac := assemblestate.CalculateHMAC(rdt, f, "test-secret")
	proof, err := asserts.RawSignWithKey(hmac, key)
	c.Assert(err, check.IsNil)

	identity := assemblestate.Identity{
		RDT:         rdt,
		FP:          f,
		Serial:      string(asserts.Encode(serial)),
		SerialProof: assemblestate.Proof(proof),
	}

	// should successfully record identity with valid assertion
	err = dt.RecordIdentity(identity)
	c.Assert(err, check.IsNil)

	// verify identity was recorded
	c.Assert(dt.Identified("test-device"), check.Equals, true)
	recorded, ok := dt.Lookup("test-device")
	c.Assert(ok, check.Equals, true)
	c.Assert(recorded.RDT, check.Equals, assemblestate.DeviceToken("test-device"))
	c.Assert(recorded.Serial, check.Equals, string(asserts.Encode(serial)))
}

func (s *deviceTrackerSuite) TestRecordIdentityUntrustedKeys(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, _ := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	// create assertion signed by untrusted authority
	untrusted := assertstest.NewStoreStack("untrusted-authority", nil)
	serial, _ := createTestSerial(c, untrusted)

	identity := assemblestate.Identity{
		RDT:    assemblestate.DeviceToken("test-device"),
		Serial: string(asserts.Encode(serial)),
	}
	err = dt.RecordIdentity(identity)

	// should now be rejected due to verification failure
	c.Assert(err, check.ErrorMatches, "invalid serial assertion for device test-device: .*")
	c.Assert(dt.Identified("test-device"), check.Equals, false)
}

func (s *deviceTrackerSuite) TestSerialProofValidation(c *check.C) {
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(
		assemblestate.DeviceQueryTrackerData{},
		time.Minute,
		time.Now,
		db,
		"test-secret",
	)
	c.Assert(err, check.IsNil)

	serial, key := createTestSerial(c, signing)
	cases := []struct {
		name     string
		identity func() assemblestate.Identity
		err      string
	}{
		{
			name: "valid proof",
			identity: func() assemblestate.Identity {
				rdt := assemblestate.DeviceToken("test-device")
				fp := assemblestate.Fingerprint{1, 2, 3, 4, 5}
				hmac := assemblestate.CalculateHMAC(rdt, fp, "test-secret")

				proof, err := asserts.RawSignWithKey(hmac, key)
				c.Assert(err, check.IsNil)

				return assemblestate.Identity{
					RDT:         rdt,
					FP:          fp,
					Serial:      string(asserts.Encode(serial)),
					SerialProof: proof,
				}
			},
			err: "",
		},
		{
			name: "empty proof",
			identity: func() assemblestate.Identity {
				return assemblestate.Identity{
					RDT:         assemblestate.DeviceToken("empty-proof"),
					FP:          assemblestate.Fingerprint{1, 2, 3, 4, 5},
					Serial:      string(asserts.Encode(serial)),
					SerialProof: nil,
				}
			},
			err: ".*empty serial proof.*",
		},
		{
			name: "invalid signature",
			identity: func() assemblestate.Identity {
				rdt := assemblestate.DeviceToken("invalid-sig")
				fp := assemblestate.Fingerprint{1, 2, 3, 4, 5}
				hmac := assemblestate.CalculateHMAC(rdt, fp, "test-secret")
				key, _ := assertstest.GenerateKey(1024)

				proof, err := asserts.RawSignWithKey(hmac, key)
				c.Assert(err, check.IsNil)

				return assemblestate.Identity{
					RDT:         rdt,
					FP:          fp,
					Serial:      string(asserts.Encode(serial)),
					SerialProof: proof,
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "wrong secret",
			identity: func() assemblestate.Identity {
				rdt := assemblestate.DeviceToken("wrong-secret")
				fp := assemblestate.Fingerprint{1, 2, 3, 4, 5}
				hmac := assemblestate.CalculateHMAC(rdt, fp, "wrong-secret")

				proof, err := asserts.RawSignWithKey(hmac, key)
				c.Assert(err, check.IsNil)

				return assemblestate.Identity{
					RDT:         rdt,
					FP:          fp,
					Serial:      string(asserts.Encode(serial)),
					SerialProof: proof,
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "wrong rdt",
			identity: func() assemblestate.Identity {
				rdt := assemblestate.DeviceToken("wrong-rdt-hmac")
				fp := assemblestate.Fingerprint{1, 2, 3, 4, 5}
				hmac := assemblestate.CalculateHMAC("wrong-rdt", fp, "test-secret")

				proof, err := asserts.RawSignWithKey(hmac, key)
				c.Assert(err, check.IsNil)

				return assemblestate.Identity{
					RDT:         rdt,
					FP:          fp,
					Serial:      string(asserts.Encode(serial)),
					SerialProof: proof,
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "wrong fingerprint",
			identity: func() assemblestate.Identity {
				rdt := assemblestate.DeviceToken("wrong-fp-hmac")
				fp := assemblestate.Fingerprint{1, 2, 3, 4, 5}
				wrongFp := assemblestate.Fingerprint{9, 8, 7, 6, 5}
				hmac := assemblestate.CalculateHMAC(rdt, wrongFp, "test-secret")

				proof, err := asserts.RawSignWithKey(hmac, key)
				c.Assert(err, check.IsNil)

				return assemblestate.Identity{
					RDT:         rdt,
					FP:          fp,
					Serial:      string(asserts.Encode(serial)),
					SerialProof: proof,
				}
			},
			err: ".*serial proof verification failed.*",
		},
		{
			name: "invalid signature",
			identity: func() assemblestate.Identity {
				return assemblestate.Identity{
					RDT:         assemblestate.DeviceToken("corrupted"),
					FP:          assemblestate.Fingerprint{1, 2, 3, 4, 5},
					Serial:      string(asserts.Encode(serial)),
					SerialProof: []byte{0x00, 0x01, 0x02, 0x03, 0xFF},
				}
			},
			err: ".*serial proof verification failed.*",
		},
	}

	for _, tc := range cases {
		err := dt.RecordIdentity(tc.identity())
		if tc.err == "" {
			c.Assert(err, check.IsNil, check.Commentf("test case %q", tc.name))
		} else {
			c.Assert(err, check.ErrorMatches, tc.err, check.Commentf("test case %q", tc.name))
		}
	}
}

func (s *deviceTrackerSuite) TestExpiredAssertion(c *check.C) {
	data := assemblestate.DeviceQueryTrackerData{}
	db, signing := mockAssertDB(c)
	dt, err := assemblestate.NewDeviceQueryTracker(data, time.Minute, time.Now, db, "test-secret")
	c.Assert(err, check.IsNil)

	serial, key := createExpiredSerial(c, signing)
	rdt := assemblestate.DeviceToken("test-device")
	f := assemblestate.Fingerprint{1, 2, 3, 4, 5}

	hmac := assemblestate.CalculateHMAC(rdt, f, "test-secret")
	proof, err := asserts.RawSignWithKey(hmac, key)
	c.Assert(err, check.IsNil)

	identity := assemblestate.Identity{
		RDT:         rdt,
		FP:          f,
		Serial:      string(asserts.Encode(serial)),
		SerialProof: assemblestate.Proof(proof),
	}

	err = dt.RecordIdentity(identity)
	c.Assert(err, check.ErrorMatches, "invalid serial assertion for device test-device: .*")
	c.Assert(dt.Identified("test-device"), check.Equals, false)
}

func normalizeDeviceExport(d assemblestate.DeviceQueryTrackerData) {
	for _, devices := range d.Queries {
		sort.Slice(devices, func(i, j int) bool {
			return devices[i] < devices[j]
		})
	}
	for _, devices := range d.Known {
		sort.Slice(devices, func(i, j int) bool {
			return devices[i] < devices[j]
		})
	}
	sort.Slice(d.IDs, func(i, j int) bool {
		return d.IDs[i].RDT < d.IDs[j].RDT
	})
}

func hasSignal(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
