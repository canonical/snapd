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

package notices_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/overlord/state"
)

type noticesSuite struct{}

var _ = Suite(&noticesSuite{})

func newNoticeManager(c *C) *notices.NoticeManager {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	m, err := notices.Manager(st)
	c.Assert(err, IsNil)
	return m
}

func (s *noticesSuite) TestMarshal(c *C) {
	m := newNoticeManager(c)

	start := time.Now()
	uid := uint32(1000)
	addNotice(c, m, &uid, notices.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond) // ensure there's time between the occurrences
	addNotice(c, m, &uid, notices.ChangeUpdateNotice, "123", &notices.AddNoticeOptions{
		Data: map[string]string{"k": "v"},
	})

	notices := m.Notices(nil)
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
	var notice *notices.Notice
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
	var notice *notices.Notice
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
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.ChangeUpdateNotice, "123", nil)
	addNotice(c, m, nil, notices.RefreshInhibitNotice, "-", nil)
	addNotice(c, m, nil, notices.WarningNotice, "danger!", nil)

	result := m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{notices.ChangeUpdateNotice}})
	c.Assert(result, HasLen, 1)
	c.Check(result[0].Type(), Equals, notices.ChangeUpdateNotice)

	result = m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{notices.RefreshInhibitNotice}})
	c.Assert(result, HasLen, 1)
	c.Check(result[0].Type(), Equals, notices.RefreshInhibitNotice)

	result = m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{notices.WarningNotice}})
	c.Assert(result, HasLen, 1)
	c.Check(result[0].Type(), Equals, notices.WarningNotice)
}

func (s *noticesSuite) TestOccurrences(c *C) {
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", nil)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", nil)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.ChangeUpdateNotice, "123", nil)

	result := m.Notices(nil)
	c.Assert(result, HasLen, 2)
	n := noticeToMap(c, result[0])
	c.Check(n["id"], Equals, "1")
	c.Check(n["occurrences"], Equals, 3.0)
	n = noticeToMap(c, result[1])
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
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", &notices.AddNoticeOptions{
		RepeatAfter: first,
	})
	time.Sleep(time.Microsecond)

	result := m.Notices(nil)
	c.Assert(result, HasLen, 1)
	n := noticeToMap(c, result[0])
	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	// LastRepeated won't yet be updated as we only waited 1us (repeat-after is long)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	// Add a notice (with faked time) after a long time and ensure it has repeated
	future := time.Now().Add(delay)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", &notices.AddNoticeOptions{
		RepeatAfter: second,
		Time:        future,
	})
	result = m.Notices(nil)
	c.Assert(result, HasLen, 1)
	n = noticeToMap(c, result[0])
	newLastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(newLastRepeated.After(lastRepeated), Equals, true)
}

func (s *noticesSuite) TestNoticesFilterUserID(c *C) {
	m := newNoticeManager(c)

	uid1 := uint32(1000)
	uid2 := uint32(0)
	addNotice(c, m, &uid1, notices.ChangeUpdateNotice, "443", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, &uid2, notices.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, &uid2, notices.WarningNotice, "Warning 1!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "Warning 2!", nil)

	// No filter
	result := m.Notices(nil)
	c.Assert(result, HasLen, 4)

	// User ID unset
	result = m.Notices(&notices.NoticeFilter{})
	c.Assert(result, HasLen, 4)

	// User ID set
	result = m.Notices(&notices.NoticeFilter{UserID: &uid2})
	c.Assert(result, HasLen, 3)
	n := noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, float64(uid2))
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "123")
	n = noticeToMap(c, result[1])
	c.Check(n["user-id"], Equals, float64(uid2))
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 1!")
	n = noticeToMap(c, result[2])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 2!")
}

func (s *noticesSuite) TestNoticesFilterType(c *C) {
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.RefreshInhibitNotice, "-", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.InterfacesRequestsPromptNotice, "443", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "Warning 1!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "Warning 2!", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.SnapRunInhibitNotice, "snap-name", nil)

	// No filter
	result := m.Notices(nil)
	c.Assert(result, HasLen, 6)

	// No types
	result = m.Notices(&notices.NoticeFilter{})
	c.Assert(result, HasLen, 6)

	// One type
	result = m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{notices.WarningNotice}})
	c.Assert(result, HasLen, 2)
	n := noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 1!")
	n = noticeToMap(c, result[1])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "Warning 2!")

	// Another type
	result = m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{notices.RefreshInhibitNotice}})
	c.Assert(result, HasLen, 1)
	n = noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "refresh-inhibit")
	c.Check(n["key"], Equals, "-")

	// Another type
	result = m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{notices.SnapRunInhibitNotice}})
	c.Assert(result, HasLen, 1)
	n = noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "snap-run-inhibit")
	c.Check(n["key"], Equals, "snap-name")

	// Multiple types
	result = m.Notices(&notices.NoticeFilter{Types: []notices.NoticeType{
		notices.ChangeUpdateNotice,
		notices.InterfacesRequestsPromptNotice,
	}})
	c.Assert(result, HasLen, 2)
	n = noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "interfaces-requests-prompt")
	c.Check(n["key"], Equals, "443")
	n = noticeToMap(c, result[1])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, "123")
}

func (s *noticesSuite) TestNoticesFilterKey(c *C) {
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "example.com/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/baz", nil)

	// No filter
	result := m.Notices(nil)
	c.Assert(result, HasLen, 3)

	// No keys
	result = m.Notices(&notices.NoticeFilter{})
	c.Assert(result, HasLen, 3)

	// One key
	result = m.Notices(&notices.NoticeFilter{Keys: []string{"example.com/x"}})
	c.Assert(result, HasLen, 1)
	n := noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "example.com/x")

	// Multiple keys
	result = m.Notices(&notices.NoticeFilter{Keys: []string{
		"foo.com/bar",
		"foo.com/baz",
	}})
	c.Assert(result, HasLen, 2)
	n = noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/bar")
	n = noticeToMap(c, result[1])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/baz")
}

func (s *noticesSuite) TestNoticesFilterAfter(c *C) {
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.WarningNotice, "foo.com/x", nil)
	result := m.Notices(nil)
	c.Assert(result, HasLen, 1)
	n := noticeToMap(c, result[0])
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)

	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/y", nil)

	// After unset
	result = m.Notices(nil)
	c.Assert(result, HasLen, 2)

	// After set
	result = m.Notices(&notices.NoticeFilter{After: lastRepeated})
	c.Assert(result, HasLen, 1)
	n = noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/y")
}

func (s *noticesSuite) TestNotice(c *C) {
	m := newNoticeManager(c)

	uid1 := uint32(0)
	uid2 := uint32(123)
	uid3 := uint32(1000)
	addNotice(c, m, &uid1, notices.WarningNotice, "foo.com/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, &uid2, notices.WarningNotice, "foo.com/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, &uid3, notices.WarningNotice, "foo.com/z", nil)

	result := m.Notices(nil)
	c.Assert(result, HasLen, 3)
	n := noticeToMap(c, result[1])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)

	notice := m.Notice(noticeId)
	c.Assert(notice, NotNil)
	n = noticeToMap(c, notice)
	c.Check(n["user-id"], Equals, 123.0)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo.com/y")
}

func (s *noticesSuite) TestEmptyState(c *C) {
	m := newNoticeManager(c)

	result := m.Notices(nil)
	c.Check(result, HasLen, 0)
}

// TODO: test save/restore instead
//
//func (s *noticesSuite) TestCheckpoint(c *C) {
//	backend := &fakeStateBackend{}
//	st := state.New(backend)
//	st.Lock()
//	addNotice(c, st, nil, state.WarningNotice, "foo.com/bar", nil)
//	st.Unlock()
//	c.Assert(backend.checkpoints, HasLen, 1)
//
//	st2, err := state.ReadState(nil, bytes.NewReader(backend.checkpoints[0]))
//	c.Assert(err, IsNil)
//	st2.Lock()
//	defer st2.Unlock()
//
//	notices := st2.Notices(nil)
//	c.Assert(notices, HasLen, 1)
//	n := noticeToMap(c, notices[0])
//	c.Check(n["user-id"], Equals, nil)
//	c.Check(n["type"], Equals, "warning")
//	c.Check(n["key"], Equals, "foo.com/bar")
//}

func (s *noticesSuite) TestDeleteExpired(c *C) {
	m := newNoticeManager(c)

	old := time.Now().Add(-8 * 24 * time.Hour)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/w", &notices.AddNoticeOptions{
		Time: old,
	})
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/x", &notices.AddNoticeOptions{
		Time: old,
	})
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/y", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/z", nil)

	c.Assert(m.NumNotices(), Equals, 4)
	m.Ensure()
	c.Assert(m.NumNotices(), Equals, 2)

	result := m.Notices(nil)
	c.Assert(result, HasLen, 2)
	n := noticeToMap(c, result[0])
	c.Assert(n["key"], Equals, "foo.com/y")
	n = noticeToMap(c, result[1])
	c.Assert(n["key"], Equals, "foo.com/z")
}

func (s *noticesSuite) TestWaitNoticesExisting(c *C) {
	m := newNoticeManager(c)

	addNotice(c, m, nil, notices.WarningNotice, "foo.com/bar", nil)
	addNotice(c, m, nil, notices.WarningNotice, "example.com/x", nil)
	addNotice(c, m, nil, notices.WarningNotice, "foo.com/baz", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := m.WaitNotices(ctx, &notices.NoticeFilter{Keys: []string{"example.com/x"}})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 1)
	n := noticeToMap(c, result[0])
	c.Check(n["user-id"], Equals, nil)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "example.com/x")
}

func (s *noticesSuite) TestWaitNoticesNew(c *C) {
	m := newNoticeManager(c)

	go func() {
		time.Sleep(10 * time.Millisecond)
		addNotice(c, m, nil, notices.WarningNotice, "example.com/x", nil)
		addNotice(c, m, nil, notices.WarningNotice, "example.com/y", nil)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := m.WaitNotices(ctx, &notices.NoticeFilter{Keys: []string{"example.com/y"}})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 1)
	n := noticeToMap(c, result[0])
	c.Assert(n["key"], Equals, "example.com/y")
}

func (s *noticesSuite) TestWaitNoticesTimeout(c *C) {
	m := newNoticeManager(c)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	result, err := m.WaitNotices(ctx, nil)
	c.Assert(err, ErrorMatches, "context deadline exceeded")
	c.Assert(result, HasLen, 0)
}

// TODO: adapt this test
//
//func (s *noticesSuite) TestReadStateWaitNotices(c *C) {
//	st := state.New(nil)
//	st.Lock()
//	defer st.Unlock()
//
//	marshalled, err := st.MarshalJSON()
//	c.Assert(err, IsNil)
//
//	st2, err := state.ReadState(nil, bytes.NewBuffer(marshalled))
//	c.Assert(err, IsNil)
//	st2.Lock()
//	defer st2.Unlock()
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
//	defer cancel()
//	notices, err := st2.WaitNotices(ctx, nil)
//	c.Assert(errors.Is(err, context.DeadlineExceeded), Equals, true)
//	c.Assert(notices, HasLen, 0)
//}

func (s *noticesSuite) TestWaitNoticesLongPoll(c *C) {
	m := newNoticeManager(c)

	go func() {
		for i := 0; i < 10; i++ {
			addNotice(c, m, nil, notices.WarningNotice, fmt.Sprintf("a.b/%d", i), nil)
			time.Sleep(time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var after time.Time
	for total := 0; total < 10; {
		result, err := m.WaitNotices(ctx, &notices.NoticeFilter{After: after})
		c.Assert(err, IsNil)
		c.Assert(result, Not(HasLen), 0)
		total += len(result)
		n := noticeToMap(c, result[len(result)-1])
		lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
		c.Assert(err, IsNil)
		after = lastRepeated
	}
}

func (s *noticesSuite) TestWaitNoticesConcurrent(c *C) {
	const numWaiters = 100

	m := newNoticeManager(c)

	var wg sync.WaitGroup
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			key := fmt.Sprintf("a.b/%d", i)
			result, err := m.WaitNotices(ctx, &notices.NoticeFilter{Keys: []string{key}})
			c.Assert(err, IsNil)
			c.Assert(result, HasLen, 1)
			n := noticeToMap(c, result[0])
			c.Assert(n["key"], Equals, key)
		}(i)
	}

	for i := 0; i < numWaiters; i++ {
		addNotice(c, m, nil, notices.WarningNotice, fmt.Sprintf("a.b/%d", i), nil)
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
	m := newNoticeManager(c)

	// Invalid type
	id, err := m.AddNotice(nil, "bad-type", "123", nil)
	c.Check(err, ErrorMatches, `internal error: cannot add notice with invalid type "bad-type"`)
	c.Check(id, Equals, "")

	// Empty key
	id, err = m.AddNotice(nil, notices.ChangeUpdateNotice, "", nil)
	c.Check(err, ErrorMatches, `internal error: cannot add change-update notice with invalid key ""`)
	c.Check(id, Equals, "")

	// Large key
	id, err = m.AddNotice(nil, notices.ChangeUpdateNotice, strings.Repeat("x", 257), nil)
	c.Check(err, ErrorMatches, `internal error: cannot add change-update notice with invalid key: key must be 256 bytes or less`)
	c.Check(id, Equals, "")

	// Unxpected key for refresh-inhibit notice
	id, err = m.AddNotice(nil, notices.RefreshInhibitNotice, "123", nil)
	c.Check(err, ErrorMatches, `internal error: cannot add refresh-inhibit notice with invalid key "123": only "-" key is supported`)
	c.Check(id, Equals, "")
}

func (s *noticesSuite) TestAvoidTwoNoticesWithSameDateTime(c *C) {
	m := newNoticeManager(c)

	testDate := time.Date(2024, time.April, 11, 11, 24, 5, 21, time.UTC)
	restore := notices.MockTime(testDate)
	defer restore()

	id1, err := m.AddNotice(nil, notices.ChangeUpdateNotice, "123", nil)
	c.Assert(err, IsNil)
	notice1 := noticeToMap(c, m.Notice(id1))
	c.Assert(notice1, NotNil)

	id2, err := m.AddNotice(nil, notices.ChangeUpdateNotice, "456", nil)
	c.Assert(err, IsNil)
	notice2 := noticeToMap(c, m.Notice(id2))
	c.Assert(notice2, NotNil)

	id3, err := m.AddNotice(nil, notices.ChangeUpdateNotice, "789", nil)
	c.Assert(err, IsNil)
	notice3 := noticeToMap(c, m.Notice(id3))
	c.Assert(notice3, NotNil)

	testDate2 := time.Date(2024, time.April, 11, 11, 24, 5, 40, time.UTC)
	restore2 := notices.MockTime(testDate2)
	defer restore2()

	id4, err := m.AddNotice(nil, notices.ChangeUpdateNotice, "ABC", nil)
	c.Assert(err, IsNil)
	notice4 := noticeToMap(c, m.Notice(id4))
	c.Assert(notice4, NotNil)

	// ensure that the notices are ordered in time
	lastOccurred1, err := time.Parse(time.RFC3339, notice1["last-occurred"].(string))
	c.Assert(err, IsNil)
	lastOccurred2, err := time.Parse(time.RFC3339, notice2["last-occurred"].(string))
	c.Assert(err, IsNil)
	lastOccurred3, err := time.Parse(time.RFC3339, notice3["last-occurred"].(string))
	c.Assert(err, IsNil)
	lastOccurred4, err := time.Parse(time.RFC3339, notice4["last-occurred"].(string))
	c.Assert(err, IsNil)

	c.Assert(lastOccurred1.Equal(testDate), Equals, true)
	c.Assert(lastOccurred2.Equal(testDate), Equals, false)
	c.Assert(lastOccurred3.Equal(testDate), Equals, false)
	c.Assert(lastOccurred1.Before(lastOccurred2), Equals, true)
	c.Assert(lastOccurred1.Before(lastOccurred3), Equals, true)
	c.Assert(lastOccurred2.Before(lastOccurred3), Equals, true)
	c.Assert(lastOccurred4.Equal(testDate2), Equals, true)
	c.Assert(lastOccurred4.After(lastOccurred3), Equals, true)
}

// noticeToMap converts a Notice to a map using a JSON marshal-unmarshal round trip.
func noticeToMap(c *C, notice *notices.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}

func addNotice(c *C, m *notices.NoticeManager, userID *uint32, noticeType notices.NoticeType, key string, options *notices.AddNoticeOptions) {
	_, err := m.AddNotice(userID, noticeType, key, options)
	c.Assert(err, IsNil)
}
