// -*- Mode: Go; indent-tabs-mode: t -*-

// Copyright (c) 2025 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package notices_test

import (
	"context"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type noticesSuite struct {
	testutil.BaseTest

	st *state.State
}

var _ = Suite(&noticesSuite{})

func (s *noticesSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.st = state.New(nil)
}

type testNoticeBackend struct {
	// Send something over noticesChan to make BackendNotices() return it.
	noticesChan chan []*state.Notice
	// Send something over noticeChan to make BackendNotice() return it.
	noticeChan chan *state.Notice
	// Send something over waitNoticesChan to make BackendWaitNotices() return it.
	waitNoticesChan chan []*state.Notice
	// Store the NoticeFilters passed into BackendNotices.
	noticesFilterChan chan *state.NoticeFilter
	// Store the NoticeFilters passed into BackendWaitNotices.
	waitNoticesFilterChan chan *state.NoticeFilter
}

func newTestNoticeBackend() *testNoticeBackend {
	return &testNoticeBackend{
		noticesChan:     make(chan []*state.Notice, 1),
		noticeChan:      make(chan *state.Notice, 1),
		waitNoticesChan: make(chan []*state.Notice, 1),
		// Give noticesFilterChan capacity 2 so WaitNotices can call
		// BackendNotices twice, before and after calling BakendWaitNotices,
		// without blocking.
		noticesFilterChan:     make(chan *state.NoticeFilter, 2),
		waitNoticesFilterChan: make(chan *state.NoticeFilter, 1),
	}
}

func (b *testNoticeBackend) BackendNotices(filter *state.NoticeFilter) []*state.Notice {
	b.noticesFilterChan <- filter
	return <-b.noticesChan
}

func (b *testNoticeBackend) BackendNotice(id string) *state.Notice {
	return <-b.noticeChan
}

func (b *testNoticeBackend) BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	b.waitNoticesFilterChan <- filter
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case n := <-b.waitNoticesChan:
		return n, nil
	}
}

var _ = notices.NoticeBackend(&testNoticeBackend{})

func (s *noticesSuite) TestNewNoticeManagerStateBackend(c *C) {
	nm := notices.NewNoticeManager(s.st)
	c.Assert(nm, NotNil)

	userID := uint32(1000)

	s.st.Lock()
	id1, err := s.st.AddNotice(&userID, state.WarningNotice, "foo", nil)
	c.Assert(err, IsNil)
	id2, err := s.st.AddNotice(&userID, state.ChangeUpdateNotice, "bar", nil)
	c.Assert(err, IsNil)
	c.Check(id2, Not(Equals), id1)
	s.st.Unlock()

	// Check that state notices are queried with the lock held. If the lock
	// were not held, querying state notices would panic.
	notices := nm.Notices(nil)
	c.Check(notices, HasLen, 2)
	c.Check(notices[0].Type(), Equals, state.WarningNotice)
	c.Check(notices[1].Type(), Equals, state.ChangeUpdateNotice)

	filtered := nm.Notices(&state.NoticeFilter{Keys: []string{"bar"}})
	c.Check(filtered, HasLen, 1)
	c.Check(filtered[0].Type(), Equals, state.ChangeUpdateNotice)

	notice := nm.Notice(id1)
	c.Check(notice.Type(), Equals, state.WarningNotice)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	waited, err := nm.WaitNotices(ctx, &state.NoticeFilter{Keys: []string{"foo"}})
	c.Assert(err, IsNil)
	c.Check(waited, HasLen, 1)
	c.Check(waited[0].Type(), Equals, state.WarningNotice)

	waitChan := make(chan []*state.Notice)
	go func() {
		goWaited, goErr := nm.WaitNotices(ctx, &state.NoticeFilter{Keys: []string{"baz"}})
		c.Check(goErr, IsNil)
		waitChan <- goWaited
	}()

	time.Sleep(10 * time.Millisecond)

	s.st.Lock()
	_, err = s.st.AddNotice(&userID, state.InterfacesRequestsPromptNotice, "baz", nil)
	c.Assert(err, IsNil)
	s.st.Unlock()

	select {
	case waited = <-waitChan:
		// all good
	case <-time.NewTimer(time.Second).C:
		c.Fatal("failed to receive notice from WaitNotice")
	}
	c.Check(waited, HasLen, 1)
	c.Check(waited[0].Type(), Equals, state.InterfacesRequestsPromptNotice)
}

func (s *noticesSuite) TestRegisterBackend(c *C) {
	nm := notices.NewNoticeManager(s.st)
	c.Assert(nm, NotNil)

	bknd := newTestNoticeBackend()

	typ1 := state.WarningNotice
	namespace1 := "foo"
	// Send empty notices over bknd.noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd.noticesChan <- nil
	validateNotice1, drained, err := nm.RegisterBackend(bknd, typ1, namespace1, false)
	c.Assert(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter := <-bknd.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ1},
	})

	noticeID := "foo-123"
	noticeKey := "456"
	validateErr := validateNotice1(noticeID, typ1, noticeKey, nil)
	c.Assert(validateErr, IsNil)

	typ2 := state.ChangeUpdateNotice
	namespace2 := "bar"
	// Send empty notices over bknd.noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd.noticesChan <- nil
	validateNotice2, drained, err := nm.RegisterBackend(bknd, typ2, namespace2, true)
	c.Assert(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter = <-bknd.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ2},
	})

	noticeID = "bar-abc"
	noticeKey = "xyz"
	validateErr = validateNotice2(noticeID, typ2, noticeKey, nil)
	c.Assert(validateErr, IsNil)

	bknd2 := newTestNoticeBackend()
	// Send empty notices over bknd2.noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd2.noticesChan <- nil

	namespace3 := "baz"
	validateNotice3, drained, err := nm.RegisterBackend(bknd2, typ2, namespace3, false)
	c.Assert(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter = <-bknd2.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ2},
	})

	noticeID = "baz-a1"
	noticeKey = "-"
	validateErr = validateNotice3(noticeID, typ2, noticeKey, nil)
	c.Assert(validateErr, IsNil)
}

func (s *noticesSuite) TestRegisterBackendReportLastNoticeTimestamp(c *C) {
	nm := notices.NewNoticeManager(s.st)

	// bknd1 will set LastNoticeTimestamp
	bknd1 := newTestNoticeBackend()
	// bknd2 will have no notices
	bknd2 := newTestNoticeBackend()
	// bknd3 will have notice with earlier timestamp
	bknd3 := newTestNoticeBackend()
	// bknd4 will have notice with later timestamp (so update lastNoticeTimestamp)
	bknd4 := newTestNoticeBackend()

	userID := uint32(1000)
	key := "foo"
	// All backends can register for WarningNotice
	typ := state.WarningNotice
	// Set up unique namespaces for each
	ns1 := "1"
	ns2 := "2"
	ns3 := "3"
	ns4 := "4"

	// Make bknd1 have a notice with timestamp in the future
	time1 := time.Now().Add(24 * time.Hour)
	notice1 := state.NewNotice("1-abc", &userID, typ, key, time1, nil, 0, 0)
	bknd1.noticesChan <- []*state.Notice{notice1}
	// Make bknd2 have no notices
	bknd2.noticesChan <- nil
	// Make bknd3 have notice with timestamp earlier than bknd1, so the last
	// notice timestamp will not be updated
	time3 := time1.Add(-time.Hour)
	notice3 := state.NewNotice("3-abc", &userID, typ, key, time3, nil, 0, 0)
	bknd3.noticesChan <- []*state.Notice{notice3}
	// Make bknd4 have notice with timestamp later than bknd1, so the last
	// notice timestamp *will* be updated
	time4 := time1.Add(time.Hour)
	notice4 := state.NewNotice("4-abc", &userID, typ, key, time4, nil, 0, 0)
	bknd4.noticesChan <- []*state.Notice{notice4}

	// There's currently no way to get the last notice timestamp from the
	// state via an exported function, so use NextNoticeTimestamp and add
	// 1 nanosecond to the times being compared. We can do this since all
	// relevant times are in the future, so NextNoticeTimestamp will always be
	// 1 nanosecond later than the existing last notice timestamp.

	currLast := nm.NextNoticeTimestamp()
	c.Assert(currLast.Before(time1), Equals, true)

	_, drained, err := nm.RegisterBackend(bknd1, typ, ns1, true)
	c.Check(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter := <-bknd1.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ},
	})

	currLast = nm.NextNoticeTimestamp()
	c.Assert(currLast.Equal(time1.Add(time.Nanosecond)), Equals, true)

	_, drained, err = nm.RegisterBackend(bknd2, typ, ns2, false)
	c.Check(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter = <-bknd2.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ},
	})

	// No notices here, so timestamp should be 1ns later than before
	newLast := nm.NextNoticeTimestamp()
	c.Assert(newLast.Equal(currLast.Add(time.Nanosecond)), Equals, true)
	currLast = newLast

	_, drained, err = nm.RegisterBackend(bknd3, typ, ns3, true)
	c.Check(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter = <-bknd3.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ},
	})

	// Notice had earlier timestamp, so timestamp should be 1ns later than before
	newLast = nm.NextNoticeTimestamp()
	c.Assert(newLast.Equal(currLast.Add(time.Nanosecond)), Equals, true)
	currLast = newLast

	_, drained, err = nm.RegisterBackend(bknd4, typ, ns4, false)
	c.Check(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter = <-bknd4.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ},
	})

	// Notice had later timestamp, so timestamp should be 1ns later than time4
	newLast = nm.NextNoticeTimestamp()
	c.Assert(newLast.Equal(time4.Add(time.Nanosecond)), Equals, true)
}

func (s *noticesSuite) TestRegisterBackendDrainNotices(c *C) {
	uid := uint32(1000)

	// Create some notices before creating the manager
	s.st.Lock()
	id1, err := s.st.AddNotice(&uid, state.WarningNotice, "foo", nil)
	c.Assert(err, IsNil)
	id2, err := s.st.AddNotice(&uid, state.ChangeUpdateNotice, "bar", nil)
	c.Assert(err, IsNil)
	s.st.Unlock()

	nm := notices.NewNoticeManager(s.st)

	// Create some notices after creating the manager
	s.st.Lock()
	id3, err := s.st.AddNotice(&uid, state.ChangeUpdateNotice, "baz", nil)
	c.Assert(err, IsNil)
	id4, err := s.st.AddNotice(&uid, state.WarningNotice, "qux", nil)
	c.Assert(err, IsNil)
	s.st.Unlock()

	existing := nm.Notices(nil)
	c.Assert(existing, HasLen, 4)
	c.Assert(existing[0].ID(), Equals, id1)
	c.Assert(existing[0].Key(), Equals, "foo")
	c.Assert(existing[1].ID(), Equals, id2)
	c.Assert(existing[1].Key(), Equals, "bar")
	c.Assert(existing[2].ID(), Equals, id3)
	c.Assert(existing[2].Key(), Equals, "baz")
	c.Assert(existing[3].ID(), Equals, id4)
	c.Assert(existing[3].Key(), Equals, "qux")

	// Register a backend without draining notices
	bknd1 := newTestNoticeBackend()
	// Send empty notices over noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd1.noticesChan <- nil
	_, drained, err := nm.RegisterBackend(bknd1, state.WarningNotice, "something", false)
	c.Check(err, IsNil)
	c.Check(drained, HasLen, 0)

	// Check that the state still has all notices
	bknd1.noticesChan <- nil
	result := nm.Notices(nil)
	c.Check(result, HasLen, 4)
	<-bknd1.noticesFilterChan
	s.st.Lock()
	result = s.st.Notices(nil)
	s.st.Unlock()
	c.Check(result, HasLen, 4)

	// Register a backend and do drain notices
	bknd2 := newTestNoticeBackend()
	bknd2.noticesChan <- nil
	_, drained, err = nm.RegisterBackend(bknd2, state.WarningNotice, "another", true)
	c.Assert(err, IsNil)
	c.Assert(drained, HasLen, 2)
	c.Check(drained[0].ID(), Equals, id1)
	c.Check(drained[0].Key(), Equals, "foo")
	c.Check(drained[1].ID(), Equals, id4)
	c.Check(drained[1].Key(), Equals, "qux")

	// Check that the notices were successfully removed from state
	bknd1.noticesChan <- nil
	bknd2.noticesChan <- nil
	result = nm.Notices(nil)
	c.Assert(result, HasLen, 2)
	c.Check(result[0].ID(), Equals, id2)
	c.Check(result[0].Key(), Equals, "bar")
	c.Check(result[1].ID(), Equals, id3)
	c.Check(result[1].Key(), Equals, "baz")
	<-bknd1.noticesFilterChan
	<-bknd2.noticesFilterChan
	s.st.Lock()
	result = s.st.Notices(nil)
	s.st.Unlock()
	c.Check(result, HasLen, 2)

	// Register a different backend and try to drain notices, but they've
	// already been drained, so drained is empty.
	bknd3 := newTestNoticeBackend()
	bknd3.noticesChan <- nil
	_, drained, err = nm.RegisterBackend(bknd3, state.WarningNotice, "different", true)
	c.Check(err, IsNil)
	c.Check(drained, HasLen, 0)

	// Check that the notices are unchanged
	bknd1.noticesChan <- nil
	bknd2.noticesChan <- nil
	bknd3.noticesChan <- nil
	result = nm.Notices(nil)
	c.Assert(result, HasLen, 2)
	c.Check(result[0].ID(), Equals, id2)
	c.Check(result[0].Key(), Equals, "bar")
	c.Check(result[1].ID(), Equals, id3)
	c.Check(result[1].Key(), Equals, "baz")
	<-bknd1.noticesFilterChan
	<-bknd2.noticesFilterChan
	<-bknd3.noticesFilterChan
	s.st.Lock()
	result = s.st.Notices(nil)
	s.st.Unlock()
	c.Check(result, HasLen, 2)
}

func (s *noticesSuite) TestRegisterBackendErrors(c *C) {
	nm := notices.NewNoticeManager(s.st)

	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()
	// Send empty notices over noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd1.noticesChan <- nil
	bknd2.noticesChan <- nil

	namespace1 := "foo"
	typ1 := state.WarningNotice

	validate, drained, err := nm.RegisterBackend(bknd1, typ1, "", false)
	c.Assert(err, ErrorMatches, "internal error: cannot register notice backend with empty namespace")
	c.Assert(validate, IsNil)
	c.Assert(drained, HasLen, 0)
	select {
	case filter := <-bknd1.noticesFilterChan:
		c.Errorf("Unexpectedly called BackendNotices even though registration failed: %+v", filter)
	default:
		// all good
	}

	validate, drained, err = nm.RegisterBackend(bknd1, typ1, namespace1, false)
	c.Assert(err, IsNil)
	c.Assert(validate, NotNil)
	c.Assert(drained, HasLen, 0)
	filter := <-bknd1.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ1},
	})

	validate, drained, err = nm.RegisterBackend(bknd2, typ1, namespace1, false)
	c.Assert(err, ErrorMatches, "internal error: cannot register notice backend with namespace which is already registered to a different backend: .*")
	c.Assert(validate, IsNil)
	c.Assert(drained, HasLen, 0)
	select {
	case filter := <-bknd2.noticesFilterChan:
		c.Errorf("Unexpectedly called BackendNotices even though registration failed: %+v", filter)
	default:
		// all good
	}
}

func (s *noticesSuite) TestValidateNotice(c *C) {
	nm := notices.NewNoticeManager(s.st)

	bknd := newTestNoticeBackend()

	// Register backend as provider of two types with two namespaces
	typ1 := state.WarningNotice
	typ2 := state.ChangeUpdateNotice
	namespace1 := "foo"
	namespace2 := "bar"

	// Send empty notices over bknd.noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd.noticesChan <- nil
	validateNotice1, drained, err := nm.RegisterBackend(bknd, typ1, namespace1, false)
	c.Assert(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter := <-bknd.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ1},
	})

	// Register to another type as well, so we can check that the validation
	// closure only accepts notices matching its specific registration.
	// Send empty notices over bknd.noticesChan so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd.noticesChan <- nil
	validateNotice2, drained, err := nm.RegisterBackend(bknd, typ2, namespace2, true)
	c.Assert(err, IsNil)
	c.Check(drained, HasLen, 0)
	filter = <-bknd.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ2},
	})

	validID1 := "foo-123"
	validID2 := "bar-abc"
	validKey := "xyz"

	err = validateNotice1(validID1, typ1, validKey, nil)
	c.Check(err, IsNil)

	err = validateNotice2(validID2, typ2, validKey, nil)
	c.Check(err, IsNil)

	invalidTyp := state.NoticeType("invalid")
	err = validateNotice1(validID1, invalidTyp, validKey, nil)
	c.Check(err, ErrorMatches, "cannot add notice with invalid type.*")

	// Even if backend registered for another type, the closure will return an
	// error for any type other than the one with which it was specifically
	// registered.
	err = validateNotice1(validID1, typ2, validKey, nil)
	c.Check(err, ErrorMatches, `cannot add change-update notice to notice backend registered to provide warning notices`)

	noPrefixID := "something"
	onlyPrefixID := "foo" // still treated as no prefix
	for _, id := range []string{noPrefixID, onlyPrefixID} {
		err = validateNotice1(id, typ1, validKey, nil)
		c.Check(err, ErrorMatches, `cannot add notice without ID prefix to notice backend registered with namespace: "foo"`)
	}

	otherPrefixID := "bar-123"
	unrelatedPrefixID := "baz-123"
	for _, id := range []string{otherPrefixID, unrelatedPrefixID} {
		err = validateNotice1(id, typ1, validKey, nil)
		c.Check(err, ErrorMatches, `cannot add notice with ID prefix not matching the namespace registered to the notice backend: "ba." != "foo"`)
	}
}

func (s *noticesSuite) TestRelevantBackendsForFilter(c *C) {
	nm := notices.NewNoticeManager(s.st)

	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()
	bknd3 := newTestNoticeBackend()
	// Send empty notices over noticesChans so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd1.noticesChan <- nil
	bknd2.noticesChan <- nil
	bknd3.noticesChan <- nil

	typ1 := state.WarningNotice
	typ2 := state.WarningNotice
	typ3 := state.ChangeUpdateNotice

	_, _, err := nm.RegisterBackend(bknd1, typ1, "1", false)
	c.Assert(err, IsNil)
	_, _, err = nm.RegisterBackend(bknd2, typ2, "2", false)
	c.Assert(err, IsNil)
	_, _, err = nm.RegisterBackend(bknd3, typ3, "3", false)
	c.Assert(err, IsNil)

	filter := <-bknd1.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ1},
	})
	filter = <-bknd2.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ2},
	})
	filter = <-bknd3.noticesFilterChan
	c.Check(filter, DeepEquals, &state.NoticeFilter{
		Types: []state.NoticeType{typ3},
	})

	// With no filter, all registered backends, plus state, should be returned
	for _, filter := range []*state.NoticeFilter{nil, {}} {
		backends := notices.RelevantBackendsForFilter(nm, filter)
		assertBackendsEqual(c, backends, []notices.NoticeBackend{
			nm.StateBackend(),
			bknd1,
			bknd2,
			bknd3,
		})
	}

	// With filter with type which no backends registered, just get state backend
	filter = &state.NoticeFilter{Types: []state.NoticeType{state.InterfacesRequestsPromptNotice}}
	backends := notices.RelevantBackendsForFilter(nm, filter)
	assertBackendsEqual(c, backends, []notices.NoticeBackend{nm.StateBackend()})

	// With filter with type which has backends registered, just get those backends
	filter = &state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}}
	backends = notices.RelevantBackendsForFilter(nm, filter)
	assertBackendsEqual(c, backends, []notices.NoticeBackend{bknd1, bknd2})

	// With filter with mix of registered and unregistered types, get the
	// relevant registered backend as well as state
	filter = &state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice, state.InterfacesRequestsRuleUpdateNotice}}
	backends = notices.RelevantBackendsForFilter(nm, filter)
	assertBackendsEqual(c, backends, []notices.NoticeBackend{nm.StateBackend(), bknd3})
}

func assertBackendsEqual(c *C, received []notices.NoticeBackend, expected []notices.NoticeBackend) {
	c.Assert(received, HasLen, len(expected))
	c.Assert(expected, HasLen, len(received))
	for _, bknd := range received {
		assertSliceContainsBackend(c, expected, bknd)
	}
	for _, bknd := range expected {
		assertSliceContainsBackend(c, received, bknd)
	}
}

func assertSliceContainsBackend(c *C, haystack []notices.NoticeBackend, needle notices.NoticeBackend) {
	for _, backend := range haystack {
		if backend == needle {
			return
		}
	}
	c.Fatalf("backend not found in backend slice")
}

func (s *noticesSuite) TestDoNoticesFilter(c *C) {
	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()
	backends := []*testNoticeBackend{bknd1, bknd2}

	now := time.Now()

	// If passed a nil filter, BeforeOrAt filter should be set according to now
	var given *state.NoticeFilter
	expected := &state.NoticeFilter{BeforeOrAt: now}
	testDoNoticesFilter(c, backends, given, expected, now)

	// If passed a non-nil filter with zero BeforeOrAt, BeforeOrAt filter
	// should be set to now
	given = &state.NoticeFilter{
		Types: []state.NoticeType{state.WarningNotice, state.ChangeUpdateNotice},
	}
	expected = &state.NoticeFilter{
		Types:      []state.NoticeType{state.WarningNotice, state.ChangeUpdateNotice},
		BeforeOrAt: now,
	}
	testDoNoticesFilter(c, backends, given, expected, now)

	// If passed a filter with BeforeOrAt in the past, it should be unchanged.
	given = &state.NoticeFilter{
		Keys:       []string{"foo", "bar"},
		BeforeOrAt: now.Add(-time.Hour),
	}
	expected = &state.NoticeFilter{
		Keys:       []string{"foo", "bar"},
		BeforeOrAt: now.Add(-time.Hour),
	}
	testDoNoticesFilter(c, backends, given, expected, now)

	// If passed a filter with BeforeOrAt in the future, it should be set to now.
	given = &state.NoticeFilter{
		After:      now.Add(-time.Minute),
		BeforeOrAt: now.Add(time.Hour),
	}
	expected = &state.NoticeFilter{
		After:      now.Add(-time.Minute),
		BeforeOrAt: now,
	}
	testDoNoticesFilter(c, backends, given, expected, now)

	// If passed a filter with After in the future, BeforeOrAt will be set as usual.
	// XXX: we allow After to be after BeforeOrAt, and pass the filter on to the
	// backends. This might change in the future if we want to return early.
	given = &state.NoticeFilter{
		After: now.Add(2 * time.Hour),
	}
	expected = &state.NoticeFilter{
		After:      now.Add(2 * time.Hour),
		BeforeOrAt: now,
	}
	testDoNoticesFilter(c, backends, given, expected, now)
}

func testDoNoticesFilter(c *C, backends []*testNoticeBackend, givenFilter, expectedFilter *state.NoticeFilter, now time.Time) {
	// Send nil notices over backends so they do not block
	for _, bknd := range backends {
		bknd.noticesChan <- nil
	}

	// Make a []NoticeBackend so the doNotices type checker is happy
	abstractBackends := make([]notices.NoticeBackend, len(backends))
	for i, bknd := range backends {
		abstractBackends[i] = bknd
	}
	ns := notices.DoNotices(abstractBackends, givenFilter, now)
	c.Assert(ns, HasLen, 0)

	// Check that each backend received the expected filter
	for _, bknd := range backends {
		select {
		case received := <-bknd.noticesFilterChan:
			c.Check(received, DeepEquals, expectedFilter)
		default:
			c.Fatalf("failed to receive filter from backend")
		}
	}
}

func (s *noticesSuite) TestDoNoticesSorting(c *C) {
	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()

	t0 := time.Now()
	t1 := t0.Add(1 * time.Minute)
	t2 := t0.Add(2 * time.Minute)
	t3 := t0.Add(3 * time.Minute)
	t4 := t0.Add(4 * time.Minute)
	t5 := t0.Add(5 * time.Minute)
	t6 := t0.Add(6 * time.Minute)

	// Other notice details don't matter for this test

	n1 := state.NewNotice("1", nil, state.WarningNotice, "bar", t1, nil, 0, 0)
	n2 := state.NewNotice("2", nil, state.WarningNotice, "bar", t2, nil, time.Hour, 0)
	n3 := state.NewNotice("3", nil, state.WarningNotice, "bar", t3, nil, 0, 0)
	n4 := state.NewNotice("4", nil, state.WarningNotice, "bar", t4, nil, 0, 0)

	// Create n5 with initial timestamp before other notices, then repeat so it
	// has the latest lastRepeated timestamp.
	n5 := state.NewNotice("5", nil, state.WarningNotice, "foo", t0, nil, 0, 0)
	n5.Reoccur(t5, nil, 0)

	// For good measure, re-record n2 with a newer timestamp, but less than its
	// RepeatAfter duration. Then we can test that lastRepeated is used.
	n2.Reoccur(t6, nil, time.Hour)

	bknd1.noticesChan <- []*state.Notice{n2, n3, n5}
	bknd2.noticesChan <- []*state.Notice{n1, n4}

	// Check that resulting notices are sorted by lastRepeated time.
	// Since we're directly feeding notices, filter and now do not matter.
	result := notices.DoNotices([]notices.NoticeBackend{bknd1, bknd2}, nil, time.Now())
	c.Check(result, DeepEquals, []*state.Notice{n1, n2, n3, n4, n5})
}

func (s *noticesSuite) TestNotices(c *C) {
	nm := notices.NewNoticeManager(s.st)

	// Add notices to state so we can query for them later
	userID := uint32(1000)
	s.st.Lock()
	_, err := s.st.AddNotice(&userID, state.ChangeUpdateNotice, "foo", nil)
	c.Assert(err, IsNil)
	_, err = s.st.AddNotice(&userID, state.InterfacesRequestsRuleUpdateNotice, "bar", nil)
	c.Assert(err, IsNil)
	s.st.Unlock()

	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()
	bknd3 := newTestNoticeBackend()

	bknd1.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd1, state.WarningNotice, "foo", false)
	c.Assert(err, IsNil)
	<-bknd1.noticesFilterChan
	// Register bknd2 for two notice types
	bknd2.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd2, state.WarningNotice, "bar", false)
	c.Assert(err, IsNil)
	<-bknd2.noticesFilterChan
	bknd2.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd2, state.ChangeUpdateNotice, "baz", false)
	c.Assert(err, IsNil)
	<-bknd2.noticesFilterChan
	bknd3.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd3, state.InterfacesRequestsPromptNotice, "qux", false)
	c.Assert(err, IsNil)
	<-bknd3.noticesFilterChan

	// Build some notices which can later be returned arbitrary by backends.
	// Ignore their ID and type, since we can feed them arbitrarily to the test
	// backends.
	n1 := state.NewNotice("1", nil, "", "abc", time.Now(), nil, 0, 0)
	n2 := state.NewNotice("2", nil, "", "abc", time.Now(), nil, time.Hour, 0)
	n3 := state.NewNotice("3", nil, "", "abc", time.Now(), nil, 0, 0)
	n4 := state.NewNotice("4", nil, "", "abc", time.Now(), nil, 0, 0)

	// Prepare to query all notices from all backends
	bknd1.noticesChan <- []*state.Notice{n2}
	bknd2.noticesChan <- []*state.Notice{n3, n1}
	bknd3.noticesChan <- []*state.Notice{n4}

	// When filter empty, all notices are queried from all backends, including state
	result := nm.Notices(nil)
	c.Check(result, HasLen, 6)
	// We don't have a pointer to the state notices, but we can check that the
	// remaining 4 are as expected and in order.
	c.Check(result[2:], DeepEquals, []*state.Notice{n1, n2, n3, n4})
	// Clear filterChans for each test backend
	<-bknd1.noticesFilterChan
	<-bknd2.noticesFilterChan
	<-bknd3.noticesFilterChan

	// Prepare to query only bknd2 by filtering for ChangeUpdateNotices
	bknd2.noticesChan <- []*state.Notice{n1, n4}

	// When we provide a filter with a type for which there is a registered
	// backend, then state is not queried.
	filter := &state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}}
	result = nm.Notices(filter)
	c.Check(result, DeepEquals, []*state.Notice{n1, n4})
	<-bknd2.noticesFilterChan

	// Prepare to query bknd1 and bknd2
	bknd1.noticesChan <- []*state.Notice{n3, n1}
	bknd2.noticesChan <- []*state.Notice{n2}

	// When we provide a filter with multiple types, some have registered
	// backends and some don't, then all expected backends (including state)
	// are queried.
	filter = &state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice, state.InterfacesRequestsRuleUpdateNotice}}
	result = nm.Notices(filter)
	c.Check(result, HasLen, 4)
	// We don't have a pointer to the state notice, but we can check that the
	// remaining 3 are as expected and in order.
	c.Check(result[1:], DeepEquals, []*state.Notice{n1, n2, n3})
	// Clear filterChans for each test backend
	<-bknd1.noticesFilterChan
	<-bknd2.noticesFilterChan

	// When we provide a filter for a type for which no backend is registered, we only query state
	filter = &state.NoticeFilter{Types: []state.NoticeType{state.InterfacesRequestsRuleUpdateNotice}}
	result = nm.Notices(filter)
	c.Check(result, HasLen, 1)
	c.Check(result[0].Type(), Equals, state.InterfacesRequestsRuleUpdateNotice)
}

func (s *noticesSuite) TestNotice(c *C) {
	nm := notices.NewNoticeManager(s.st)

	// Add a notice to state so we can query for it later
	userID := uint32(1000)
	s.st.Lock()
	stateNoticeID, err := s.st.AddNotice(&userID, state.ChangeUpdateNotice, "foo", nil)
	s.st.Unlock()
	c.Assert(err, IsNil)

	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()
	bknd3 := newTestNoticeBackend()
	bknd4 := newTestNoticeBackend()

	// Send empty notices over noticesChans so registration doesn't block
	// trying to get notices to update last notice timestamp
	bknd1.noticesChan <- nil
	bknd2.noticesChan <- nil
	bknd3.noticesChan <- nil
	bknd4.noticesChan <- nil

	_, _, err = nm.RegisterBackend(bknd1, state.WarningNotice, "foo", false)
	c.Assert(err, IsNil)
	_, _, err = nm.RegisterBackend(bknd2, state.WarningNotice, "bar", false)
	c.Assert(err, IsNil)
	_, _, err = nm.RegisterBackend(bknd3, state.WarningNotice, "baz", false)
	c.Assert(err, IsNil)
	_, _, err = nm.RegisterBackend(bknd4, state.WarningNotice, "qux", false)
	c.Assert(err, IsNil)

	queryID := "baz-123"

	// Define a notice for the correct backend to return. The ID doesn't even
	// technically need to match the queryID, since the manager doesn't check
	// the notice returned by the backend.
	notice := state.NewNotice(queryID, nil, state.WarningNotice, "xyz", time.Now(), nil, 0, 0)

	// Give the correct backend the notice to return via BackendNotice. If any
	// other methods or backends are queried, the test will block, since no
	// other channels are populated with notices.
	bknd3.noticeChan <- notice

	// Hold state lock so we know state isn't checked (else it would block)
	s.st.Lock()
	result := nm.Notice(queryID)
	s.st.Unlock()
	c.Check(result, Equals, notice)

	// Now query for the notice which was previously added to state. If other
	// backends were queried, the test would block.
	result = nm.Notice(stateNoticeID)
	c.Check(result, NotNil)
	c.Check(result.Type(), Equals, state.ChangeUpdateNotice)

	// Now query notice with ID namespace which no backend has registered.
	// Since it is namespaced, state cannot be asked either. Hold state lock
	// to test this. And since no backends have been fed notices, querying them
	// would block too. So test that nothing blocks.
	s.st.Lock()
	result = nm.Notice("unregistered-1234")
	s.st.Unlock()
	c.Check(result, IsNil)
}

func (s *noticesSuite) prepareTestWaitNotices(c *C) (*notices.NoticeManager, []*testNoticeBackend, []*state.Notice) {
	nm := notices.NewNoticeManager(s.st)

	bknd1 := newTestNoticeBackend()
	bknd2 := newTestNoticeBackend()
	bknd3 := newTestNoticeBackend()
	backends := []*testNoticeBackend{bknd1, bknd2, bknd3}

	bknd1.noticesChan <- nil
	_, _, err := nm.RegisterBackend(bknd1, state.WarningNotice, "foo", false)
	c.Assert(err, IsNil)
	<-bknd1.noticesFilterChan
	// Register bknd2 for two notice types
	bknd2.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd2, state.WarningNotice, "bar", false)
	c.Assert(err, IsNil)
	<-bknd2.noticesFilterChan
	bknd2.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd2, state.ChangeUpdateNotice, "baz", false)
	c.Assert(err, IsNil)
	<-bknd2.noticesFilterChan
	bknd3.noticesChan <- nil
	_, _, err = nm.RegisterBackend(bknd3, state.InterfacesRequestsPromptNotice, "qux", false)
	c.Assert(err, IsNil)
	<-bknd3.noticesFilterChan

	// Build some notices which can later be returned arbitrary by backends.
	// Ignore their ID and type, since we can feed them arbitrarily to the test
	// backends.
	n1 := state.NewNotice("1", nil, "", "abc", time.Now(), nil, 0, 0)
	n2 := state.NewNotice("2", nil, "", "abc", time.Now(), nil, time.Hour, 0)
	n3 := state.NewNotice("3", nil, "", "abc", time.Now(), nil, 0, 0)
	n4 := state.NewNotice("4", nil, "", "abc", time.Now(), nil, 0, 0)

	notices := []*state.Notice{n1, n2, n3, n4}

	return nm, backends, notices
}

func (s *noticesSuite) TestWaitNoticesFilterOneBackend(c *C) {
	nm, backends, notices := s.prepareTestWaitNotices(c)

	// If filter only matches one backend, BackendWaitNotices is called on that
	// backend directly, and returned. Other backends are not queried.
	backends[1].waitNoticesChan <- []*state.Notice{notices[1], notices[2]}
	filter := &state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}}
	result, err := nm.WaitNotices(context.Background(), filter)
	c.Assert(err, IsNil)
	c.Check(result, DeepEquals, []*state.Notice{notices[1], notices[2]})
	receivedFilter := <-backends[1].waitNoticesFilterChan
	c.Check(receivedFilter, DeepEquals, &state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
}

func (s *noticesSuite) TestWaitNoticesExistingNotices(c *C) {
	nm, backends, notices := s.prepareTestWaitNotices(c)

	// If filter matches multiple backends, then each should queried for
	// existing notices before or at the current time, and if one or more
	// already has notices, then return them immediately, once all backends
	// return.
	backends[0].noticesChan <- []*state.Notice{notices[2], notices[0]}
	beforeTime := time.Now()
	beforeOrAtFilterTime := beforeTime.Add(time.Hour)
	// Simulate backends[1] being slow to return notices, and ensure its result is included.
	go func() {
		time.Sleep(10 * time.Millisecond)
		backends[1].noticesChan <- []*state.Notice{notices[1]}
	}()
	filter := &state.NoticeFilter{
		Types:      []state.NoticeType{state.WarningNotice},
		BeforeOrAt: beforeOrAtFilterTime,
	}
	result, err := nm.WaitNotices(context.Background(), filter)
	afterTime := time.Now()
	c.Assert(err, IsNil)
	c.Check(result, DeepEquals, []*state.Notice{notices[0], notices[1], notices[2]})
	// Check that the backends received a filter with BeforeOrAt set to the current time when called.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.noticesFilterChan
		c.Check(receivedFilter.Types, DeepEquals, []state.NoticeType{state.WarningNotice})
		c.Check(beforeTime.Before(receivedFilter.BeforeOrAt), Equals, true)
		c.Check(receivedFilter.BeforeOrAt.Before(afterTime), Equals, true)
		c.Check(receivedFilter.BeforeOrAt.Before(beforeOrAtFilterTime), Equals, true)
	}
	// Check that the original filter was not overwritten
	c.Check(filter.BeforeOrAt, Equals, beforeOrAtFilterTime)
}

func (s *noticesSuite) TestWaitNoticesFilterInPast(c *C) {
	nm, backends, _ := s.prepareTestWaitNotices(c)

	// If filter matches multiple backends, then each should queried for
	// existing notices before or at the current time, and if none have
	// notices, and the BeforeOrAt filter is in the past, then return
	// immediately, once all backends return.
	backends[0].noticesChan <- nil
	beforeTime := time.Now()
	// Simulate backends[1] being slow to return, and test the result waited for it.
	go func() {
		time.Sleep(10 * time.Millisecond)
		backends[1].noticesChan <- nil
	}()
	filter := &state.NoticeFilter{
		Types:      []state.NoticeType{state.WarningNotice},
		BeforeOrAt: beforeTime, // this is in the past at call time
	}
	result, err := nm.WaitNotices(context.Background(), filter)
	afterTime := time.Now()
	c.Check(beforeTime.Add(10*time.Millisecond).Before(afterTime), Equals, true)
	c.Assert(err, IsNil)
	c.Check(result, HasLen, 0)
	// Check that the backends received a filter with BeforeOrAt unchanged from the original filter.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.noticesFilterChan
		c.Check(receivedFilter, DeepEquals, filter)
	}
	// Check that the original filter was not overwritten
	c.Check(filter.BeforeOrAt, Equals, beforeTime)
}

func (s *noticesSuite) TestWaitNoticesContextCancelled(c *C) {
	nm, backends, _ := s.prepareTestWaitNotices(c)

	// If all backends return empty lists for BackendNotices, and then the
	// context is cancelled, return immediately.
	backends[0].noticesChan <- nil
	backends[1].noticesChan <- nil
	filter := &state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}}
	beforeTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := nm.WaitNotices(ctx, filter)
	afterTime := time.Now()
	c.Check(beforeTime.Add(10*time.Millisecond).Before(afterTime), Equals, true)
	c.Check(err, ErrorMatches, "context deadline exceeded")
	c.Check(result, HasLen, 0)
	// Check that the backends received a filter to BackendNotices with
	// BeforeOrAt set to the current time when called.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.noticesFilterChan
		c.Check(beforeTime.Before(receivedFilter.BeforeOrAt), Equals, true)
		c.Check(receivedFilter.BeforeOrAt.Before(afterTime), Equals, true)
	}
	// Check that the backends received a filter to BackendWaitNotices with
	// BeforeOrAt unset.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.waitNoticesFilterChan
		c.Check(receivedFilter, DeepEquals, filter)
	}
	// Check that the original filter was not overwritten
	c.Check(filter.BeforeOrAt.IsZero(), Equals, true)
}

func (s *noticesSuite) TestWaitNoticesNoNotices(c *C) {
	nm, backends, _ := s.prepareTestWaitNotices(c)

	// If all backends return empty lists for both BackendNotices and
	// BackendWaitNotices, then WaitNotices returns nil immediately.
	backends[0].noticesChan <- nil
	backends[1].noticesChan <- nil
	backends[0].waitNoticesChan <- nil
	backends[1].waitNoticesChan <- nil
	filter := &state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}}
	result, err := nm.WaitNotices(context.Background(), filter)
	c.Check(err, IsNil)
	c.Check(result, HasLen, 0)
	// Check that the backends received a filter to BackendNotices with
	// BeforeOrAt set to the current time when called.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.noticesFilterChan
		c.Check(receivedFilter.BeforeOrAt.IsZero(), Equals, false)
	}
	// Check that the backends received a filter to BackendWaitNotices with
	// BeforeOrAt unset.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.waitNoticesFilterChan
		c.Check(receivedFilter, DeepEquals, filter)
	}
	// Check that the original filter was not overwritten
	c.Check(filter.BeforeOrAt.IsZero(), Equals, true)
}

func (s *noticesSuite) TestWaitNoticesFutureNotice(c *C) {
	nm, backends, notices := s.prepareTestWaitNotices(c)

	// If all backends return empty lists for BackendNotices, then a backend
	// returns some notices for BackendWaitNotices, then all backends are
	// again queried for BackendNotices, but with a BeforeOrAt filter set to
	// the last return notice's timestamp.
	backends[0].noticesChan <- nil
	backends[1].noticesChan <- nil
	backends[0].waitNoticesChan <- []*state.Notice{notices[2], notices[3]}
	// backends[1] doesn't return notices from WaitNotices, instead it should
	// be cancelled once backends[0] returns the notices.
	beforeTime := time.Now()
	go func() {
		// Once able, send notices for final BackendNotices.
		// These notices might include the notice(s) originally returned by
		// BackendWaitNotices, since they might have been re-recorded since.
		// For consistency with other backends, they should be excluded from
		// the final result. Test this by returning unrelated notices.
		backends[0].noticesChan <- []*state.Notice{notices[1]}
		time.Sleep(10 * time.Millisecond)
		backends[1].noticesChan <- []*state.Notice{notices[0]}
	}()
	filter := &state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}}
	result, err := nm.WaitNotices(context.Background(), filter)
	afterTime := time.Now()
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, []*state.Notice{notices[0], notices[1]})
	c.Check(beforeTime.Add(10*time.Millisecond).Before(afterTime), Equals, true)
	// Check that the backends first received a filter to BackendNotices
	// with BeforeOrAt set to the current time when called.
	for _, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.noticesFilterChan
		c.Check(beforeTime.Before(receivedFilter.BeforeOrAt), Equals, true)
		c.Check(receivedFilter.BeforeOrAt.Before(afterTime), Equals, true)
	}
	// Check that the backends then received a filter to BackendWaitNotices
	// with BeforeOrAt unset.
	for i, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.waitNoticesFilterChan
		c.Check(receivedFilter, DeepEquals, filter, Commentf("Backend %d", i+1))
	}
	// Check that the backends lastly receive a filter to BackendNotices
	// with BeforeOrAt set to the lastRepeated timestamp of the final notice
	// returned by the calls to BackendWaitNotices.
	for i, bknd := range []*testNoticeBackend{backends[0], backends[1]} {
		receivedFilter := <-bknd.noticesFilterChan
		c.Check(receivedFilter.BeforeOrAt.Equal(notices[3].LastRepeated()), Equals, true, Commentf("Backend %d receivedFilter.BeforeOrAt: %v", i+1, receivedFilter.BeforeOrAt))
	}
	// Check that the original filter was not overwritten
	c.Check(filter.BeforeOrAt.IsZero(), Equals, true)
}
