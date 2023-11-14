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
		return url.Values{"types": {"custom"}}
	})
}

func (s *noticesSuite) TestNoticesFilterKey(c *C) {
	s.testNoticesFilter(c, func(after time.Time) url.Values {
		return url.Values{"keys": {"a.b/2"}}
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
			"types": {"custom"},
			"keys":  {"a.b/2"},
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
	noticeId, err := st.AddNotice(state.CustomNotice, "a.b/2", &state.AddNoticeOptions{
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
		"type":         "custom",
		"key":          "a.b/2",
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
	addNotice(c, st, state.CustomNotice, "a.b/x", nil)
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
	addNotice(c, st, state.CustomNotice, "a.b/x", nil)
	time.Sleep(time.Microsecond)
	addNotice(c, st, state.WarningNotice, "danger", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/notices?keys=a.b/x&keys=danger", nil)
	c.Assert(err, IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notices, ok := rsp.Result.([]*state.Notice)
	c.Assert(ok, Equals, true)
	c.Assert(notices, HasLen, 2)
	n := noticeToMap(c, notices[0])
	c.Assert(n["key"], Equals, "a.b/x")
	n = noticeToMap(c, notices[1])
	c.Assert(n["key"], Equals, "danger")
}

func (s *noticesSuite) TestNoticesWait(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	go func() {
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		addNotice(c, st, state.CustomNotice, "a.b/1", nil)
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
	c.Check(n["type"], Equals, "custom")
	c.Check(n["key"], Equals, "a.b/1")
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

func (s *noticesSuite) TestAddNotice(c *C) {
	s.daemon(c)

	start := time.Now()
	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1",
		"repeat-after": "1h",
		"data": {"k": "v"}
	}`)
	req, err := http.NewRequest("POST", "/v2/notices", bytes.NewReader(body))
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v2/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	notices := st.Notices(nil)
	st.Unlock()
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)
	c.Assert(string(resultBytes), Equals, `{"id":"`+noticeId+`"}`)

	firstOccurred, err := time.Parse(time.RFC3339, n["first-occurred"].(string))
	c.Assert(err, IsNil)
	c.Assert(firstOccurred.After(start), Equals, true)
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
		"type":         "custom",
		"key":          "a.b/1",
		"occurrences":  1.0,
		"last-data":    map[string]any{"k": "v"},
		"expire-after": "168h0m0s",
		"repeat-after": "1h0m0s",
	})
}

func (s *noticesSuite) TestAddNoticeMinimal(c *C) {
	s.daemon(c)

	body := []byte(`{
		"action": "add",
		"type": "custom",
		"key": "a.b/1"
	}`)
	req, err := http.NewRequest("POST", "/v2/notices", bytes.NewReader(body))
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v2/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Status, Equals, http.StatusOK)
	resultBytes, err := json.Marshal(rsp.Result)
	c.Assert(err, IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	notices := st.Notices(nil)
	st.Unlock()
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	noticeId, ok := n["id"].(string)
	c.Assert(ok, Equals, true)
	c.Assert(string(resultBytes), Equals, `{"id":"`+noticeId+`"}`)

	delete(n, "first-occurred")
	delete(n, "last-occurred")
	delete(n, "last-repeated")
	c.Assert(n, DeepEquals, map[string]any{
		"id":           noticeId,
		"type":         "custom",
		"key":          "a.b/1",
		"occurrences":  1.0,
		"expire-after": "168h0m0s",
	})
}

func (s *noticesSuite) TestAddNoticeInvalidAction(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "bad"}`, "invalid action.*")
}

func (s *noticesSuite) TestAddNoticeInvalidType(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "foo"}`, "invalid type.*")
}

func (s *noticesSuite) TestAddNoticeInvalidKey(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "custom", "key": "bad"}`,
		"invalid key.*")
}

func (s *noticesSuite) TestAddNoticeKeyTooLong(c *C) {
	request, err := json.Marshal(map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/" + strings.Repeat("x", 257-4),
	})
	c.Assert(err, IsNil)
	s.testAddNoticeBadRequest(c, string(request), "key must be 256 bytes or less")
}

func (s *noticesSuite) TestAddNoticeDataTooLarge(c *C) {
	request, err := json.Marshal(map[string]any{
		"action": "add",
		"type":   "custom",
		"key":    "a.b/c",
		"data": map[string]string{
			"a": strings.Repeat("x", 2047),
			"b": strings.Repeat("y", 2048),
		},
	})
	c.Assert(err, IsNil)
	s.testAddNoticeBadRequest(c, string(request), "total size of data must be 4096 bytes or less")
}

func (s *noticesSuite) TestInvalidRepeatAfter(c *C) {
	s.testAddNoticeBadRequest(c, `{"action": "add", "type": "custom", "key": "a.b/1", "repeat-after": "bad"}`,
		"invalid repeat-after.*")
}

func (s *noticesSuite) testAddNoticeBadRequest(c *C, body, errorMatch string) {
	s.daemon(c)

	req, err := http.NewRequest("POST", "/v2/notices", strings.NewReader(body))
	c.Assert(err, IsNil)
	noticesCmd := apiCmd("/v2/notices")
	rsp, ok := noticesCmd.POST(noticesCmd, req, nil).(*resp)
	c.Assert(ok, Equals, true)

	c.Check(rsp.Type, Equals, ResponseTypeError)
	c.Check(rsp.Status, Equals, http.StatusBadRequest)

	result, ok := rsp.Result.(*errorResult)
	c.Assert(ok, Equals, true)
	c.Assert(result.Message, Matches, errorMatch)
}

func (s *noticesSuite) TestNotice(c *C) {
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	addNotice(c, st, state.CustomNotice, "a.b/1", nil)
	noticeId, err := st.AddNotice(state.CustomNotice, "a.b/2", nil)
	c.Assert(err, IsNil)
	addNotice(c, st, state.CustomNotice, "a.b/3", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/notices/"+noticeId, nil)
	c.Assert(err, IsNil)
	s.vars = map[string]string{"id": noticeId}
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)

	notice, ok := rsp.Result.(*state.Notice)
	c.Assert(ok, Equals, true)
	n := noticeToMap(c, notice)
	c.Check(n["type"], Equals, "custom")
	c.Check(n["key"], Equals, "a.b/2")
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
