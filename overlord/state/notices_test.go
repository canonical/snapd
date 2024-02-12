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
	"errors"
	"fmt"
	"strings"
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
	uid := uint32(1000)
	addNotice(c, st, &uid, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	addNotice(c, st, &uid, state.ChangeUpdateNotice, "123", &state.AddNoticeOptions{
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
		"user-id":      1000.0,
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
		"user-id": 1000,
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
		"user-id":        1000.0,
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

func (s *noticesSuite) TestString(c *C) {
	noticeJSON := []byte(`{
		"id": "1",
		"user-id": 1000,
		"type": "change-update",
		"key": "123",
		"first-occurred": "2023-09-01T05:23:01Z",
		"last-occurred": "2023-09-01T07:23:02Z",
		"last-repeated": "2023-09-01T06:23:03.123456789Z",
		"occurrences": 2
	}`)
	var notice *state.Notice
	err := json.Unmarshal(noticeJSON, &notice)
	c.Assert(err, IsNil)

	c.Assert(notice.String(), Equals, "Notice 1 (1000:change-update:123)")

	noticeJSON = []byte(`{
		"id": "2",
		"user-id": null,
		"type": "warning",
		"key": "scary",
		"first-occurred": "2023-09-01T05:23:01Z",
		"last-occurred": "2023-09-01T07:23:02Z",
		"last-repeated": "2023-09-01T06:23:03.123456789Z",
		"occurrences": 2
	}`)
	err = json.Unmarshal(noticeJSON, &notice)
	c.Assert(err, IsNil)

	c.Assert(notice.String(), Equals, "Notice 2 (public:warning:scary)")
}

func (s *noticesSuite) TestType(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, nil, state.ChangeUpdateNotice, "123", nil)
	addNotice(c, st, nil, state.RefreshInhibitNotice, "-", nil)
	addNotice(c, st, nil, state.WarningNotice, "danger!", nil)

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
	c.Assert(notices, HasLen, 1)
	c.Check(notices[0].Type(), Equals, state.ChangeUpdateNotice)

	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.RefreshInhibitNotice}})
	c.Assert(notices, HasLen, 1)
	c.Check(notices[0].Type(), Equals, state.RefreshInhibitNotice)

	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, HasLen, 1)
	c.Check(notices[0].Type(), Equals, state.WarningNotice)
}

func (s *noticesSuite) TestOccurrences(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.ChangeUpdateNotice, "123", nil)

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

	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", &state.AddNoticeOptions{
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
	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", &state.AddNoticeOptions{
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

func (s *noticesSuite) TestNoticesFilterUserID(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	uid1 := uint32(1000)
	uid2 := uint32(0)
	addNotice(c, st, &uid1, state.ChangeUpdateNotice, "443", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &uid2, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &uid2, state.WarningNotice, "Warning 1!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "Warning 2!", nil)

	// No filter
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 4)

	// User ID unset
	notices = st.Notices(&state.NoticeFilter{})
	c.Assert(notices, HasLen, 4)

	// User ID set
	notices = st.Notices(&state.NoticeFilter{UserID: &uid2})
	c.Assert(notices, HasLen, 3)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, float64(uid2))
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "123")
	n = noticeToMap(c, notices[1])
	c.Check(n["user-id"], Equals, float64(uid2))
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 1!")
	n = noticeToMap(c, notices[2])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 2!")
}

func (s *noticesSuite) TestNoticesFilterType(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, nil, state.RefreshInhibitNotice, "-", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.RequestsPromptNotice, "443", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "Warning 1!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "Warning 2!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.SnapRunInhibitNotice, "snap-name", nil)

	// No filter
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 6)

	// No types
	notices = st.Notices(&state.NoticeFilter{})
	c.Assert(notices, HasLen, 6)

	// One type
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 1!")
	n = noticeToMap(c, notices[1])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 2!")

	// Another type
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.RefreshInhibitNotice}})
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "refresh-inhibit")
	c.Check(n["key"], Equals, "-")

	// Another type
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.SnapRunInhibitNotice}})
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "snap-run-inhibit")
	c.Check(n["key"], Equals, "snap-name")

	// Multiple types
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{
		state.ChangeUpdateNotice,
		state.RequestsPromptNotice,
	}})
	c.Assert(notices, HasLen, 2)
	n = noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "interfaces-requests-prompt")
	c.Check(n["key"], Equals, "443")
	n = noticeToMap(c, notices[1])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "123")
}

func (s *noticesSuite) TestNoticesFilterKey(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "example.com/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/baz", nil)

	// No filter
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 3)

	// No keys
	notices = st.Notices(&state.NoticeFilter{})
	c.Assert(notices, HasLen, 3)

	// One key
	notices = st.Notices(&state.NoticeFilter{Keys: []string{"example.com/x"}})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "example.com/x")

	// Multiple keys
	notices = st.Notices(&state.NoticeFilter{Keys: []string{
		"foo.com/bar",
		"foo.com/baz",
	}})
	c.Assert(notices, HasLen, 2)
	n = noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/bar")
	n = noticeToMap(c, notices[1])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/baz")
}

func (s *noticesSuite) TestNoticesFilterAfter(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, nil, state.WarningNotice, "foo.com/x", nil)
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/y", nil)

	// After unset
	notices = st.Notices(nil)
	c.Assert(notices, HasLen, 2)

	// After set
	notices = st.Notices(&state.NoticeFilter{After: lastRepeated})
	c.Assert(notices, HasLen, 1)
	n = noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/y")
}

func (s *noticesSuite) TestNotice(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	uid1 := uint32(0)
	uid2 := uint32(123)
	uid3 := uint32(1000)
	addNotice(c, st, &uid1, state.WarningNotice, "foo.com/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &uid2, state.WarningNotice, "foo.com/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, &uid3, state.WarningNotice, "foo.com/z", nil)

	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 3)
	n := noticeToMap(c, notices[1])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)

	notice := st.Notice(noticeId)
	c.Assert(notice, NotNil)
	n = noticeToMap(c, notice)
	c.Check(n["user-id"], Equals, 123.0)
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
	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
	st.Unlock()
	c.Assert(backend.checkpoints, HasLen, 1)

	st2, err := state.ReadState(nil, bytes.NewReader(backend.checkpoints[0]))
	c.Assert(err, IsNil)
	st2.Lock()
	defer st2.Unlock()

	notices := st2.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/bar")
}

func (s *noticesSuite) TestDeleteExpired(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	old := time.Now().Add(-8 * 24 * time.Hour)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/w", &state.AddNoticeOptions{
		Time: old,
	})
	addNotice(c, st, nil, state.WarningNotice, "foo.com/x", &state.AddNoticeOptions{
		Time: old,
	})
	addNotice(c, st, nil, state.WarningNotice, "foo.com/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/z", nil)

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

	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
	addNotice(c, st, nil, state.WarningNotice, "example.com/x", nil)
	addNotice(c, st, nil, state.WarningNotice, "foo.com/baz", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notices, err := st.WaitNotices(ctx, &state.NoticeFilter{Keys: []string{"example.com/x"}})
	c.Assert(err, IsNil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "example.com/x")
}

func (s *noticesSuite) TestWaitNoticesNew(c *C) {
	st := state.New(nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		defer st.Unlock()
		addNotice(c, st, nil, state.WarningNotice, "example.com/x", nil)
		addNotice(c, st, nil, state.WarningNotice, "example.com/y", nil)
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

func (s *noticesSuite) TestReadStateWaitNotices(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	marshalled, err := st.MarshalJSON()
	c.Assert(err, IsNil)

	st2, err := state.ReadState(nil, bytes.NewBuffer(marshalled))
	c.Assert(err, IsNil)
	st2.Lock()
	defer st2.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	notices, err := st2.WaitNotices(ctx, nil)
	c.Assert(errors.Is(err, context.DeadlineExceeded), Equals, true)
	c.Assert(notices, HasLen, 0)
}

func (s *noticesSuite) TestWaitNoticesLongPoll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	go func() {
		for i := 0; i < 10; i++ {
			st.Lock()
			addNotice(c, st, nil, state.WarningNotice, fmt.Sprintf("a.b/%d", i), nil)
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
		addNotice(c, st, nil, state.WarningNotice, fmt.Sprintf("a.b/%d", i), nil)
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

func (s *noticesSuite) TestValidateNotice(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// Invalid type
	id, err := st.AddNotice(nil, "bad-type", "123", nil)
	c.Check(err, ErrorMatches, `internal error: cannot add notice with invalid type "bad-type"`)
	c.Check(id, Equals, "")

	// Empty key
	id, err = st.AddNotice(nil, state.ChangeUpdateNotice, "", nil)
	c.Check(err, ErrorMatches, `internal error: cannot add change-update notice with invalid key ""`)
	c.Check(id, Equals, "")

	// Large key
	id, err = st.AddNotice(nil, state.ChangeUpdateNotice, strings.Repeat("x", 257), nil)
	c.Check(err, ErrorMatches, `internal error: cannot add change-update notice with invalid key: key must be 256 bytes or less`)
	c.Check(id, Equals, "")

	// Unxpected key for refresh-inhibit notice
	id, err = st.AddNotice(nil, state.RefreshInhibitNotice, "123", nil)
	c.Check(err, ErrorMatches, `internal error: cannot add refresh-inhibit notice with invalid key "123": only "-" key is supported`)
	c.Check(id, Equals, "")
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

func addNotice(c *C, st *state.State, userID *uint32, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) {
	_, err := st.AddNotice(userID, noticeType, key, options)
	c.Assert(err, IsNil)
}
