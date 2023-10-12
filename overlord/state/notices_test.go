// Copyright (c) 2023 Canonical Ltd
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

package state_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
)

type noticesSuite struct{}

var _ = Suite(&noticesSuite{})

func (s *noticesSuite) TestMarshal(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	start := time.Now()
	addNotice(c, st, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	addNotice(c, st, state.ChangeUpdateNotice, "123", &state.AddNoticeOptions{
		Data: map[string]string{"k": "v"},
	})

	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)

	// Convert it to a map so we're not testing the JSON string directly
	// (order of fields doesn't matter).
	n := noticeToMap(c, notices[0])

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(!firstOccurred.Before(start), Equals, true) // firstOccurred >= start
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastOccurred.After(firstOccurred), Equals, true) // lastOccurred > firstOccurred
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastRepeated.After(firstOccurred), Equals, true) // lastRepeated > firstOccurred

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           "1",
		"type":         "change-update",
		"key":          "123",
		"occurrences":  2.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
	})
}

func (s *noticesSuite) TestUnmarshal(c *C) {
	noticeJSON := []byte(`{
		"id": "1",
		"type": "change-update",
		"key": "123",
		"first-occurred": "2023-09-01T05:23:01Z",
		"last-occurred": "2023-09-01T07:23:02Z",
		"last-repeated": "2023-09-01T06:23:03.123456789Z",
		"occurrences": 2,
		"last-data": {"k": "v"},
		"repeat-after": "60m",
		"expire-after": "168h0m0s"
	}`)
	var notice *state.Notice
	err := json.Unmarshal(noticeJSON, &notice)
	c.Assert(err, IsNil)

	// The Notice fields aren't exported, so we need to marshal it into JSON
	// and then unmarshal it into a map to test.
	n := noticeToMap(c, notice)
	c.Assert(n, DeepEquals, map[string]any{
		"id":             "1",
		"type":           "change-update",
		"key":            "123",
		"first-occurred": "2023-09-01T05:23:01Z",
		"last-occurred":  "2023-09-01T07:23:02Z",
		"last-repeated":  "2023-09-01T06:23:03.123456789Z",
		"occurrences":    2.0,
		"last-data":      map[string]any{"k": "v"},
		"repeat-after":   "1h0m0s",
		"expire-after":   "168h0m0s",
	})
}

func (s *noticesSuite) TestOccurrences(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.WarningNotice, "foo.com/bar", nil)
	addNotice(c, st, state.WarningNotice, "foo.com/bar", nil)
	addNotice(c, st, state.WarningNotice, "foo.com/bar", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.ChangeUpdateNotice, "123", nil)

	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Check(n["id"], Equals, "1")
	c.Check(n["occurrences"], Equals, 3.0)
	n = noticeToMap(c, notices[1])
	c.Check(n["id"], Equals, "2")
	c.Check(n["occurrences"], Equals, 1.0)
}

func (s *noticesSuite) TestRepeatAfterFirst(c *C) {
	s.testRepeatAfter(c, 10*time.Second, 0, 10*time.Second)
}

func (s *noticesSuite) TestRepeatAfterSecond(c *C) {
	s.testRepeatAfter(c, 0, 10*time.Second, 10*time.Second)
}

func (s *noticesSuite) TestRepeatAfterBoth(c *C) {
	s.testRepeatAfter(c, 10*time.Second, 10*time.Second, 10*time.Second)
}

func (s *noticesSuite) testRepeatAfter(c *C, first, second, delay time.Duration) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.WarningNotice, "foo.com/bar", &state.AddNoticeOptions{
		RepeatAfter: first,
	})
	time.Sleep(time.Microsecond)

	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	// LastRepeated won't yet be updated as we only waited 1us (repeat-after is long)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	// Add a notice (with faked time) after a long time and ensure it has repeated
	future := time.Now().Add(delay)
	addNotice(c, st, state.WarningNotice, "foo.com/bar", &state.AddNoticeOptions{
		RepeatAfter: second,
		Time:        future,
	})
	notices = st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	newLastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(newLastRepeated.After(lastRepeated), Equals, true)
}

func (s *noticesSuite) TestNoticesFilterType(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.ChangeUpdateNotice, "443", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "Warning 1!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "Warning 2!", nil)

	// No types
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 4)

	// One type
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 1!")
	n = noticeToMap(c, notices[1])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 2!")

	// Another type
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
	c.Assert(notices, HasLen, 2)
	n = noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "443")
	n = noticeToMap(c, notices[1])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "123")

	// Multiple types
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{
		state.ChangeUpdateNotice,
		state.WarningNotice,
	}})
	c.Assert(notices, HasLen, 4)
	n = noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "443")
	n = noticeToMap(c, notices[1])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "123")
	n = noticeToMap(c, notices[2])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 1!")
	n = noticeToMap(c, notices[3])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 2!")
}

func (s *noticesSuite) TestNoticesFilterKey(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.WarningNotice, "foo.com/bar", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "example.com/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "foo.com/baz", nil)

	// No keys
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 3)

	// One key
	notices = st.Notices(&state.NoticeFilter{Keys: []string{"example.com/x"}})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "example.com/x")

	// Multiple keys
	notices = st.Notices(&state.NoticeFilter{Keys: []string{
		"foo.com/bar",
		"foo.com/baz",
	}})
	c.Assert(notices, HasLen, 2)
	n = noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/bar")
	n = noticeToMap(c, notices[1])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/baz")
}

func (s *noticesSuite) TestNoticesFilterAfter(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.WarningNotice, "foo.com/x", nil)
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "foo.com/y", nil)

	notices = st.Notices(&state.NoticeFilter{After: lastRepeated})
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/y")
}

func (s *noticesSuite) TestNotice(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.WarningNotice, "foo.com/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "foo.com/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "foo.com/z", nil)

	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 3)
	n := noticeToMap(c, notices[1])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)

	notice := st.Notice(noticeId)
	c.Assert(notice, NotNil)
	n = noticeToMap(c, notice)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/y")
}

func (s *noticesSuite) TestEmptyState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	notices := st.Notices(nil)
	c.Check(notices, HasLen, 0)
}

func (s *noticesSuite) TestCheckpoint(c *C) {
	backend := &fakeStateBackend{}
	st := state.New(backend)
	st.Lock()
	addNotice(c, st, state.WarningNotice, "foo.com/bar", nil)
	st.Unlock()
	c.Assert(backend.checkpoints, HasLen, 1)

	st2, err := state.ReadState(nil, bytes.NewReader(backend.checkpoints[0]))
	c.Assert(err, IsNil)
	st2.Lock()
	defer st2.Unlock()

	notices := st2.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/bar")
}

func (s *noticesSuite) TestDeleteExpired(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	old := time.Now().Add(-8 * 24 * time.Hour)
	addNotice(c, st, state.WarningNotice, "foo.com/w", &state.AddNoticeOptions{
		Time: old,
	})
	addNotice(c, st, state.WarningNotice, "foo.com/x", &state.AddNoticeOptions{
		Time: old,
	})
	addNotice(c, st, state.WarningNotice, "foo.com/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "foo.com/z", nil)

	c.Assert(st.NumNotices(), Equals, 4)
	st.Prune(time.Now(), 0, 0, 0)
	c.Assert(st.NumNotices(), Equals, 2)

	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], Equals, "foo.com/y")
	n = noticeToMap(c, notices[1])
	c.Assert(n["key"], Equals, "foo.com/z")
}

func (s *noticesSuite) TestWaitNoticesExisting(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, state.WarningNotice, "foo.com/bar", nil)
	addNotice(c, st, state.WarningNotice, "example.com/x", nil)
	addNotice(c, st, state.WarningNotice, "foo.com/baz", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notices, err := st.WaitNotices(ctx, &state.NoticeFilter{Keys: []string{"example.com/x"}})
	c.Assert(err, IsNil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "example.com/x")
}

func (s *noticesSuite) TestWaitNoticesNew(c *C) {
	st := state.New(nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		defer st.Unlock()
		addNotice(c, st, state.WarningNotice, "example.com/x", nil)
		addNotice(c, st, state.WarningNotice, "example.com/y", nil)
	}()

	st.Lock()
	defer st.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notices, err := st.WaitNotices(ctx, &state.NoticeFilter{Keys: []string{"example.com/y"}})
	c.Assert(err, IsNil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], Equals, "example.com/y")
}

func (s *noticesSuite) TestWaitNoticesTimeout(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	notices, err := st.WaitNotices(ctx, nil)
	c.Assert(err, ErrorMatches, "context deadline exceeded")
	c.Assert(notices, HasLen, 0)
}

func (s *noticesSuite) TestWaitNoticesLongPoll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	go func() {
		for i := 0; i < 10; i++ {
			st.Lock()
			addNotice(c, st, state.WarningNotice, fmt.Sprintf("a.b/%d", i), nil)
			st.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var after time.Time
	for total := 0; total < 10; {
		notices, err := st.WaitNotices(ctx, &state.NoticeFilter{After: after})
		c.Assert(err, IsNil)
		c.Assert(len(notices) > 0, Equals, true)
		total += len(notices)
		n := noticeToMap(c, notices[len(notices)-1])
		lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
		c.Assert(err, IsNil)
		after = lastRepeated
	}
}

func (s *noticesSuite) TestWaitNoticesConcurrent(c *C) {
	const numWaiters = 100

	st := state.New(nil)

	var wg sync.WaitGroup
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st.Lock()
			defer st.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			key := fmt.Sprintf("a.b/%d", i)
			notices, err := st.WaitNotices(ctx, &state.NoticeFilter{Keys: []string{key}})
			c.Assert(err, IsNil)
			c.Assert(notices, HasLen, 1)
			n := noticeToMap(c, notices[0])
			c.Assert(n["key"], Equals, key)
		}(i)
	}

	for i := 0; i < numWaiters; i++ {
		st.Lock()
		addNotice(c, st, state.WarningNotice, fmt.Sprintf("a.b/%d", i), nil)
		st.Unlock()
		time.Sleep(time.Microsecond)
	}

	// Wait for WaitNotice goroutines to finish
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for WaitNotice goroutines to finish")
	case <-done:
	}
}

// noticeToMap converts a Notice to a map using a JSON marshal-unmarshal round trip.
func noticeToMap(c *C, notice *state.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}

func addNotice(c *C, st *state.State, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) {
	_, err := st.AddNotice(noticeType, key, options)
	c.Assert(err, IsNil)
}
