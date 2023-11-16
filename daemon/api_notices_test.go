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

package daemon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
)

var _ = Suite(&noticesSuite{})

type noticesSuite struct {
	apiBaseSuite
}

func (s *noticesSuite) TestNoticesFilterType(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"types": {"change-update"}}
	})
}

func (s *noticesSuite) TestNoticesFilterKey(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"keys": {"123"}}
	})
}

func (s *noticesSuite) TestNoticesFilterAfter(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"after": {after.UTC().Format(time.RFC3339Nano)}}
	})
}

func (s *noticesSuite) TestNoticesFilterAll(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{
			"types": {"change-update"},
			"keys":  {"123"},
			"after": {after.UTC().Format(time.RFC3339Nano)},
		}
	})
}

func (s *noticesSuite) testNoticesFilter(c *C, makeQuery func(after time.Time) url.Values) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, state.WarningNotice, "warning", nil)
	after := time.Now()
	time.Sleep(time.Microsecond)
	noticeId, err := st.AddNotice(state.ChangeUpdateNotice, "123", &state.AddNoticeOptions{
		Data: map[string]string{"k": "v"},
	})
	c.Assert(err, IsNil)
	st.Unlock()

	query := makeQuery(after)
	req, err := http.NewRequest("GET", "/v2/notices?"+query.Encode(), nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(firstOccurred.After(after), Equals, true)
	lastOccurred, err := time.Parse(time.RFC3339, n["last-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastOccurred.Equal(firstOccurred), Equals, true)
	lastRepeated, err := time.Parse(time.RFC3339, n["last-repeated"].(string))
	c.Assert(err, IsNil)
	c.Assert(lastRepeated.Equal(firstOccurred), Equals, true)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           noticeId,
		"type":         "change-update",
		"key":          "123",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
	})
}

func (s *noticesSuite) TestNoticesFilterMultipleTypes(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/notices?types=change-update&types=warning", nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], Equals, "change-update")
	n = noticeToMap(c, notices[1])
	c.Assert(n["type"], Equals, "warning")
}

func (s *noticesSuite) TestNoticesFilterMultipleKeys(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.ChangeUpdateNotice, "456", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/notices?keys=456&keys=danger", nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], Equals, "456")
	n = noticeToMap(c, notices[1])
	c.Assert(n["key"], Equals, "danger")
}

func (s *noticesSuite) TestNoticesFilterInvalidTypes(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, state.ChangeUpdateNotice, "123", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "danger", nil)
	st.Unlock()

	// Check that invalid types are discarded, and notices with remaining
	// types are requested as expected, without error.
	req, err := http.NewRequest("GET", "/v2/notices?types=foo&types=warning&types=bar,baz", nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Assert(n["type"], Equals, "warning")

	// Check that if all types are invalid, no notices are returned, and there
	// is no error.
	req, err = http.NewRequest("GET", "/v2/notices?types=foo&types=bar,baz", nil)
	c.Assert(err, IsNil)
	rsp = s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok = rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 0)
}

func (s *noticesSuite) TestNoticesWait(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		addNotice(c, st, state.WarningNotice, "foo", nil)
		st.Unlock()
	}()

	req, err := http.NewRequest("GET", "/v2/notices?timeout=1s", nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "foo")
}

func (s *noticesSuite) TestNoticesTimeout(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/notices?timeout=1ms", nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 0)
}

func (s *noticesSuite) TestNoticesRequestCancelled(c *C) {
	s.daemon(c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", "/v2/notices?timeout=1s", nil)
	c.Assert(err, IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, Equals, 400)
	c.Check(rsp.Message, Matches, "request canceled")

	elapsed := time.Since(start)
	c.Check(elapsed > 10*time.Millisecond, Equals, true)
	c.Check(elapsed < time.Second, Equals, true)
}

func (s *noticesSuite) TestNoticesInvalidAfter(c *C) {
	s.testNoticesBadRequest(c, "after=foo", `invalid "after" timestamp.*`)
}

func (s *noticesSuite) TestNoticesInvalidTimeout(c *C) {
	s.testNoticesBadRequest(c, "timeout=foo", "invalid timeout.*")
}

func (s *noticesSuite) testNoticesBadRequest(c *C, query, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/notices?"+query, nil)
	c.Assert(err, IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Matches, errorMatch)
}

func (s *noticesSuite) TestNotice(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, state.WarningNotice, "foo", nil)
	noticeId, err := st.AddNotice(state.WarningNotice, "bar", nil)
	c.Assert(err, IsNil)
	addNotice(c, st, state.WarningNotice, "baz", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/notices/"+noticeId, nil)
	c.Assert(err, IsNil)
	s.vars = map[string]string{"id": noticeId}
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, Equals, true)
	n := noticeToMap(c, notice)
	c.Check(n["type"], Equals, "warning")
	c.Check(n["key"], Equals, "bar")
}

func (s *noticesSuite) TestNoticeNotFound(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/notices/1234", nil)
	c.Assert(err, IsNil)
	s.vars = map[string]string{"id": "1234"}
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, Equals, 404)
	c.Check(rsp.Message, Matches, `cannot find notice with id "1234"`)
}

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
