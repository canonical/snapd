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

package apparmorprompting_test

import (
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type noticebackendSuite struct {
	testutil.BaseTest

	st        *state.State
	noticeMgr *notices.NoticeManager
}

var _ = Suite(&noticebackendSuite{})

func (s *noticebackendSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.st = state.New(nil)
	s.noticeMgr = notices.NewNoticeManager(s.st)
}

func (s *noticebackendSuite) TestNewNoticeBackends(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	c.Check(noticeBackend, NotNil)
}

func (s *noticebackendSuite) TestRegisterWithManager(c *C) {
	uid1 := uint32(1000)
	uid2 := uint32(1001)
	data1 := map[string]string{"foo": "bar"}
	data2 := map[string]string{"baz": "qux", "fizz": "buzz"}
	// Add some notices to state so that they are drained and added to the
	// prompting backends during registration. Notice keys are expected to be
	// the result of IDType.String(), so make sure these are that.
	s.st.Lock()
	id1, err := s.st.AddNotice(&uid1, state.InterfacesRequestsPromptNotice, prompting.IDType(0x123).String(), &state.AddNoticeOptions{Data: data1})
	c.Assert(err, IsNil)
	id2, err := s.st.AddNotice(&uid1, state.InterfacesRequestsRuleUpdateNotice, prompting.IDType(0x456).String(), &state.AddNoticeOptions{Data: data2})
	c.Assert(err, IsNil)
	id3, err := s.st.AddNotice(&uid2, state.InterfacesRequestsRuleUpdateNotice, prompting.IDType(0x789).String(), &state.AddNoticeOptions{Data: data1})
	c.Assert(err, IsNil)
	id4, err := s.st.AddNotice(&uid2, state.WarningNotice, "foo", &state.AddNoticeOptions{Data: data2})
	c.Assert(err, IsNil)
	// Add one prompting notice with a key which is not an IDType.String(), which will be dropped
	id5, err := s.st.AddNotice(&uid2, state.InterfacesRequestsRuleUpdateNotice, "bar", &state.AddNoticeOptions{Data: data2})
	c.Assert(err, IsNil)
	s.st.Unlock()

	// Check that all notices are retrievable from the manager
	existingNotices := s.noticeMgr.Notices(nil)
	c.Assert(existingNotices, HasLen, 5)
	c.Check(existingNotices[0].ID(), Equals, id1)
	c.Check(existingNotices[1].ID(), Equals, id2)
	c.Check(existingNotices[2].ID(), Equals, id3)
	c.Check(existingNotices[3].ID(), Equals, id4)
	c.Check(existingNotices[4].ID(), Equals, id5)

	// Create new prompting notice backends and check that they initially have no notices
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Check(err, IsNil)
	c.Check(noticeBackend.PromptBackend().BackendNotices(nil), HasLen, 0)
	c.Check(noticeBackend.RuleBackend().BackendNotices(nil), HasLen, 0)

	// Creating backend has no effect on the notice manager yet
	existingNotices = s.noticeMgr.Notices(nil)
	c.Assert(existingNotices, HasLen, 5)

	// Register the prompting backends with the notice manager
	err = apparmorprompting.RegisterWithManager(noticeBackend, s.noticeMgr)
	c.Check(err, IsNil)

	// Check that the prompting backends have the expected notices now
	promptNotices := noticeBackend.PromptBackend().BackendNotices(nil)
	c.Assert(promptNotices, HasLen, 1)
	ruleNotices := noticeBackend.RuleBackend().BackendNotices(nil)
	c.Assert(ruleNotices, HasLen, 2)
	// Check that the new notice IDs are namespaced as expected and key and data were preserved
	c.Check(promptNotices[0].ID(), Equals, "prompt-0000000000000123")
	c.Check(promptNotices[0].Key(), Equals, "0000000000000123")
	c.Check(promptNotices[0].LastData(), DeepEquals, data1)
	c.Check(ruleNotices[0].ID(), Equals, "rule-0000000000000456")
	c.Check(ruleNotices[0].Key(), Equals, "0000000000000456")
	c.Check(ruleNotices[0].LastData(), DeepEquals, data2)
	c.Check(ruleNotices[1].ID(), Equals, "rule-0000000000000789")
	c.Check(ruleNotices[1].Key(), Equals, "0000000000000789")
	c.Check(ruleNotices[1].LastData(), DeepEquals, data1)

	// Check that the state no longer has notices with prompting types
	s.st.Lock()
	stateNotices := s.st.Notices(nil)
	s.st.Unlock()
	c.Check(stateNotices, HasLen, 1)
	c.Check(stateNotices[0].ID(), Equals, id4)
	c.Check(stateNotices[0].Key(), Equals, "foo")

	// Check that the notice manager can retrieve all expected notices
	notices := s.noticeMgr.Notices(nil)
	c.Check(notices, HasLen, 4)
	c.Check(notices[0].ID(), Equals, id4)
	c.Check(notices[0].Key(), Equals, "foo")
	c.Check(notices[1].ID(), Equals, "prompt-0000000000000123")
	c.Check(notices[1].Key(), Equals, "0000000000000123")
	c.Check(notices[2].ID(), Equals, "rule-0000000000000456")
	c.Check(notices[2].Key(), Equals, "0000000000000456")
	c.Check(notices[3].ID(), Equals, "rule-0000000000000789")
	c.Check(notices[3].Key(), Equals, "0000000000000789")
}

func (s *noticebackendSuite) TestAddNotice(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	promptBackend := noticeBackend.PromptBackend()

	notices := promptBackend.BackendNotices(nil)
	c.Assert(notices, HasLen, 0)

	userID := uint32(1000)

	before1 := time.Now()

	// We can add a new notice
	c.Check(promptBackend.AddNotice(userID, 0x123, nil), IsNil)
	notices = promptBackend.BackendNotices(nil)
	c.Assert(notices, HasLen, 1)
	c.Check(notices[0].Key(), Equals, prompting.IDType(0x123).String())
	c.Check(notices[0].ID(), Equals, "prompt-"+prompting.IDType(0x123).String())
	uid, ok := notices[0].UserID()
	c.Check(ok, Equals, true)
	c.Check(uid, Equals, userID)
	c.Check(notices[0].LastRepeated().After(before1), Equals, true)

	before2 := time.Now()
	c.Check(notices[0].LastRepeated().Before(before2), Equals, true)

	// We can add another notice for the same user
	c.Check(promptBackend.AddNotice(userID, 0x456, nil), IsNil)
	notices = promptBackend.BackendNotices(nil)
	c.Assert(notices, HasLen, 2)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Assert(notices, HasLen, 2)
	c.Check(notices[0].Key(), Equals, prompting.IDType(0x123).String())
	c.Check(notices[1].Key(), Equals, prompting.IDType(0x456).String())
	c.Check(notices[1].ID(), Equals, "prompt-"+prompting.IDType(0x456).String())
	uid, ok = notices[1].UserID()
	c.Check(ok, Equals, true)
	c.Check(uid, Equals, userID)
	c.Check(notices[0].LastRepeated().Before(before2), Equals, true)
	c.Check(notices[1].LastRepeated().After(before2), Equals, true)

	// We can add a notice for a different user
	c.Check(promptBackend.AddNotice(1234, 0x789, nil), IsNil)
	notices = promptBackend.BackendNotices(nil)
	c.Assert(notices, HasLen, 3)
	c.Check(notices[0].Key(), Equals, prompting.IDType(0x123).String())
	c.Check(notices[1].Key(), Equals, prompting.IDType(0x456).String())
	c.Check(notices[2].Key(), Equals, prompting.IDType(0x789).String())
	c.Check(notices[2].ID(), Equals, "prompt-"+prompting.IDType(0x789).String())
	uid, ok = notices[2].UserID()
	c.Check(ok, Equals, true)
	c.Check(uid, Equals, uint32(1234))

	beforeReAdd := time.Now()

	// If we re-add an existing notice, it ends up at the end of the list since
	// it has the newest timestamp. This works even across multiple users.
	c.Check(promptBackend.AddNotice(userID, 0x123, nil), IsNil)
	notices = promptBackend.BackendNotices(nil)
	c.Assert(notices, HasLen, 3)
	c.Check(notices[0].Key(), Equals, prompting.IDType(0x456).String())
	c.Check(notices[0].LastRepeated().Before(beforeReAdd), Equals, true)
	c.Check(notices[1].Key(), Equals, prompting.IDType(0x789).String())
	c.Check(notices[1].LastRepeated().Before(beforeReAdd), Equals, true)
	c.Check(notices[2].Key(), Equals, prompting.IDType(0x123).String())
	c.Check(notices[2].LastRepeated().After(beforeReAdd), Equals, true)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Assert(notices, HasLen, 2)
	c.Check(notices[0].Key(), Equals, prompting.IDType(0x456).String())
	c.Check(notices[1].Key(), Equals, prompting.IDType(0x123).String())
}

func (s *noticebackendSuite) TestAddNoticeData(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	promptBackend := noticeBackend.PromptBackend()

	userID := uint32(1000)

	data1 := map[string]string{"foo": "bar", "baz": "qux"}
	data2 := map[string]string{"fizz": "buzz"}

	// Add notice with no data
	c.Check(promptBackend.AddNotice(userID, 0x123, nil), IsNil)
	notices := promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), IsNil)

	// Re-add notice with different data
	c.Check(promptBackend.AddNotice(userID, 0x123, data1), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), DeepEquals, data1)

	// Re-add notice with same data
	c.Check(promptBackend.AddNotice(userID, 0x123, data1), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), DeepEquals, data1)

	// Re-add notice with other different data
	c.Check(promptBackend.AddNotice(userID, 0x123, data2), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), DeepEquals, data2)

	// Re-add notice with nil data
	c.Check(promptBackend.AddNotice(userID, 0x123, nil), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), IsNil)

	// Re-add notice with some data again
	c.Check(promptBackend.AddNotice(userID, 0x123, data1), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), DeepEquals, data1)

	// Add different notice with different data
	c.Check(promptBackend.AddNotice(userID, 0x456, data2), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), DeepEquals, data1)
	c.Check(notices[1].LastData(), DeepEquals, data2)

	// Re-add first notice with nil data again
	c.Check(promptBackend.AddNotice(userID, 0x123, nil), IsNil)
	notices = promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(notices[0].LastData(), DeepEquals, data2)
	c.Check(notices[1].LastData(), IsNil)
}

func (s *noticebackendSuite) TestAddNoticeSameKeyDifferentUser(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	promptBackend := noticeBackend.PromptBackend()

	// Add notice with one user
	c.Check(promptBackend.AddNotice(1000, 0x123, nil), IsNil)

	// Add notice with same ID but different user
	result := promptBackend.AddNotice(1234, 0x123, nil)
	c.Check(result, ErrorMatches, "cannot add prompt notice with ID prompt-0000000000000123 for user 1234: notice with the same ID already exists for user 1000")
}

func (s *noticebackendSuite) TestAddNoticeSomeExpired(c *C) {
	userID := uint32(1000)
	for i, testCase := range []struct {
		record   prompting.IDType
		expected []prompting.IDType
	}{
		{
			record:   5, // a new notice
			expected: []prompting.IDType{3, 4, 5},
		},
		{
			record:   1, // the first existing expired notice
			expected: []prompting.IDType{3, 4, 1},
		},
		{
			record:   2, // the last existing expired notice
			expected: []prompting.IDType{3, 4, 2},
		},
		{
			record:   3, // the first non-expired notice
			expected: []prompting.IDType{4, 3},
		},
		{
			record:   4, // the last non-expired notice
			expected: []prompting.IDType{3, 4},
		},
	} {
		// Need a new root dir for each test case
		dirs.SetRootDir(c.MkDir())
		st := state.New(nil)
		noticeMgr := notices.NewNoticeManager(st)

		noticeBackend, err := apparmorprompting.NewNoticeBackends(noticeMgr)
		c.Assert(err, IsNil)
		ruleBackend := noticeBackend.RuleBackend()

		c.Check(ruleBackend.AddNotice(userID, 1, nil), IsNil)
		c.Check(ruleBackend.AddNotice(userID, 2, nil), IsNil)
		c.Check(ruleBackend.AddNotice(userID, 3, nil), IsNil)
		c.Check(ruleBackend.AddNotice(userID, 4, nil), IsNil)
		origNotices := ruleBackend.BackendNotices(nil)
		c.Check(origNotices, HasLen, 4, Commentf("testCase %d: %+v\norigNotices: %+v", i, testCase, origNotices))
		// Expire the first two notices by re-recording them in the past
		for _, notice := range origNotices[:2] {
			notice.Reoccur(notice.LastRepeated().Add(-1000*time.Hour), nil, 0)
		}

		// Record the specified notice
		c.Check(ruleBackend.AddNotice(userID, testCase.record, nil), IsNil)
		notices := ruleBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
		// Check that the expected notices are found
		c.Check(notices, HasLen, len(testCase.expected), Commentf("testCase %d: %+v\nnotices: %+v", i, testCase, notices))
		for i, promptID := range testCase.expected {
			c.Check(notices[i].Key(), Equals, promptID.String())
		}
	}
}

func (s *noticebackendSuite) TestAddNoticeAllExpired(c *C) {
	for _, id := range []prompting.IDType{1, 2, 3, 4} {
		// Need a new root dir for each test case
		dirs.SetRootDir(c.MkDir())
		st := state.New(nil)
		noticeMgr := notices.NewNoticeManager(st)

		noticeBackend, err := apparmorprompting.NewNoticeBackends(noticeMgr)
		c.Assert(err, IsNil)
		promptBackend := noticeBackend.PromptBackend()

		userID := uint32(1000)

		c.Check(promptBackend.AddNotice(userID, 1, nil), IsNil)
		c.Check(promptBackend.AddNotice(userID, 2, nil), IsNil)
		c.Check(promptBackend.AddNotice(userID, 3, nil), IsNil)
		origNotices := promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
		c.Check(origNotices, HasLen, 3, Commentf("trying to add notice %s", id))
		// Expire all existing notices by re-recording them in the past
		for _, notice := range origNotices {
			notice.Reoccur(notice.LastRepeated().Add(-1000*time.Hour), nil, 0)
		}

		// Record the specified notice
		c.Check(promptBackend.AddNotice(userID, id, nil), IsNil)
		notices := promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
		// Check that the nesly (re-)recorded notice is the only one
		c.Check(notices, HasLen, 1, Commentf("trying to add notice %s", id))
		c.Check(notices[0].Key(), Equals, id.String())
	}
}

func (s *noticebackendSuite) TestAddNoticeSaveFailureRollback(c *C) {
	for _, id := range []prompting.IDType{1, 2, 3, 4, 5} {
		// Need a new root dir for each test case
		dirs.SetRootDir(c.MkDir())
		st := state.New(nil)
		noticeMgr := notices.NewNoticeManager(st)

		noticeBackend, err := apparmorprompting.NewNoticeBackends(noticeMgr)
		c.Assert(err, IsNil)
		promptBackend := noticeBackend.PromptBackend()

		userID := uint32(1000)

		c.Check(promptBackend.AddNotice(userID, 1, nil), IsNil)
		c.Check(promptBackend.AddNotice(userID, 2, nil), IsNil)
		c.Check(promptBackend.AddNotice(userID, 3, nil), IsNil)
		c.Check(promptBackend.AddNotice(userID, 4, nil), IsNil)
		origNotices := promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
		c.Assert(origNotices, HasLen, 4)
		// Expire the first two notices by re-recording them in the past
		for _, notice := range origNotices[:2] {
			notice.Reoccur(notice.LastRepeated().Add(-1000*time.Hour), nil, 0)
		}
		beforeNotices := promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
		c.Assert(beforeNotices, HasLen, 2)
		c.Assert(beforeNotices[0].Key(), Equals, prompting.IDType(3).String())
		c.Assert(beforeNotices[1].Key(), Equals, prompting.IDType(4).String())

		// Check that the expired notices are still in the ID map.
		// Technically, this check relies on internal implementation details
		// which are not required to remain true.
		c.Assert(promptBackend.BackendNotice("prompt-0000000000000001"), NotNil)
		c.Assert(promptBackend.BackendNotice("prompt-0000000000000002"), NotNil)

		// Cause a save error by writing a directory in place of the notices state file
		path := filepath.Join(dirs.SnapInterfacesRequestsRunDir, "prompt-notices.json")
		c.Assert(os.Remove(path), IsNil)
		c.Assert(os.Mkdir(path, 0o700), IsNil)

		// Add a notice with the ID from the test case
		result := promptBackend.AddNotice(userID, id, nil)
		c.Check(result, ErrorMatches, "cannot add notice to prompting interfaces-requests-prompt backend.*")

		// Check that the new notice was not added
		afterNotices := promptBackend.BackendNotices(&state.NoticeFilter{UserID: &userID})
		c.Check(afterNotices, HasLen, 2, Commentf("after adding notice with ID %s, afterNotices: %+v", id, afterNotices))
		if len(afterNotices) != 2 {
			// continue so we can get information to debug from other cases
			// without panicking.
			continue
		}
		c.Check(afterNotices[0].Key(), Equals, prompting.IDType(3).String(), Commentf("after adding notice with ID %s", id))
		c.Check(afterNotices[1].Key(), Equals, prompting.IDType(4).String(), Commentf("after adding notice with ID %s", id))

		// Check that the expired notices are still in the ID map.
		c.Check(promptBackend.BackendNotice("prompt-0000000000000001"), NotNil, Commentf("could not find expired notice with ID 1 after adding notice with ID %s", id))
		c.Check(promptBackend.BackendNotice("prompt-0000000000000002"), NotNil, Commentf("could not find expired notice with ID 2 after adding notice with ID %s", id))
	}
}

func (s *noticebackendSuite) TestLoad(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)

	data1 := map[string]string{"foo": "bar"}
	data2 := map[string]string{"baz": "qux", "fizz": "buzz"}

	c.Check(noticeBackend.PromptBackend().AddNotice(123, 0x456, nil), IsNil)
	c.Check(noticeBackend.PromptBackend().AddNotice(789, 0xabc, data1), IsNil)
	// Rule with the same user ID and prompt/rule ID will not be repeated, since
	// these are of different types and end up in different backends.
	c.Check(noticeBackend.RuleBackend().AddNotice(123, 0x456, data1), IsNil)
	c.Check(noticeBackend.RuleBackend().AddNotice(0xf00, 0xba4, nil), IsNil)
	// Add another notice to the prompts backend
	c.Check(noticeBackend.PromptBackend().AddNotice(123, 0xdef, data2), IsNil)

	promptNotices := noticeBackend.PromptBackend().BackendNotices(nil)
	c.Assert(promptNotices, HasLen, 3)
	c.Check(promptNotices[0].Key(), Equals, "0000000000000456")
	c.Check(promptNotices[1].Key(), Equals, "0000000000000ABC")
	c.Check(promptNotices[2].Key(), Equals, "0000000000000DEF")

	ruleNotices := noticeBackend.RuleBackend().BackendNotices(nil)
	c.Assert(ruleNotices, HasLen, 2)
	c.Check(ruleNotices[0].Key(), Equals, "0000000000000456")
	c.Check(ruleNotices[1].Key(), Equals, "0000000000000BA4")

	// Initialize a new backend and check that it loads the existing notices
	newBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Check(err, IsNil)

	promptNotices = newBackend.PromptBackend().BackendNotices(nil)
	c.Check(promptNotices, HasLen, 3)
	c.Check(promptNotices[0].Key(), Equals, "0000000000000456")
	c.Check(promptNotices[0].LastData(), IsNil)
	c.Check(promptNotices[1].Key(), Equals, "0000000000000ABC")
	c.Check(promptNotices[1].LastData(), DeepEquals, data1)
	c.Check(promptNotices[2].Key(), Equals, "0000000000000DEF")
	c.Check(promptNotices[2].LastData(), DeepEquals, data2)
	ruleNotices = newBackend.RuleBackend().BackendNotices(nil)
	c.Check(ruleNotices, HasLen, 2)
	c.Check(ruleNotices[0].Key(), Equals, "0000000000000456")
	c.Check(ruleNotices[0].LastData(), DeepEquals, data1)
	c.Check(ruleNotices[1].Key(), Equals, "0000000000000BA4")
	c.Check(ruleNotices[1].LastData(), IsNil)
}

func (s *noticebackendSuite) TestLoadSomeExpired(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	promptBackend := noticeBackend.PromptBackend()

	userID := uint32(1000)

	c.Check(promptBackend.AddNotice(userID, 1, nil), IsNil)
	c.Check(promptBackend.AddNotice(userID, 2, nil), IsNil)
	c.Check(promptBackend.AddNotice(userID, 3, nil), IsNil)
	c.Check(promptBackend.AddNotice(userID, 4, nil), IsNil)
	origNotices := promptBackend.BackendNotices(nil)
	c.Assert(origNotices, HasLen, 4)
	// Expire the first two notices by re-recording them in the past
	for _, notice := range origNotices[:2] {
		notice.Reoccur(notice.LastRepeated().Add(-1000*time.Hour), nil, 0)
	}

	// Manually save to disk to ensure the expired notices are written to disk.
	// Notices are saved when adding a new notice, but that also drops expired
	// notices, so we need to save manually here to ensure expired notices are
	// not dropped.
	c.Assert(promptBackend.Save(), IsNil)

	// Initialize a new backend and check that it loads existing notices
	newBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Check(err, IsNil)

	afterNotices := newBackend.PromptBackend().BackendNotices(nil)
	c.Check(afterNotices, HasLen, 2)
	c.Check(afterNotices[0].Key(), Equals, "0000000000000003")
	c.Check(afterNotices[1].Key(), Equals, "0000000000000004")
	// Check that expired notices were not added
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000001"), IsNil)
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000002"), IsNil)
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000003"), NotNil)
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000004"), NotNil)

	// Add a new notice and make sure everything is fine
	c.Check(newBackend.PromptBackend().AddNotice(1000, 5, nil), IsNil)
	finalNotices := newBackend.PromptBackend().BackendNotices(&state.NoticeFilter{UserID: &userID})
	c.Check(finalNotices, HasLen, 3)
	c.Check(finalNotices[0].Key(), Equals, "0000000000000003")
	c.Check(finalNotices[1].Key(), Equals, "0000000000000004")
	c.Check(finalNotices[2].Key(), Equals, "0000000000000005")
}

func (s *noticebackendSuite) TestLoadAllExpired(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	promptBackend := noticeBackend.PromptBackend()

	c.Check(promptBackend.AddNotice(1000, 1, nil), IsNil)
	c.Check(promptBackend.AddNotice(1000, 2, nil), IsNil)
	c.Check(promptBackend.AddNotice(1000, 3, nil), IsNil)
	c.Check(promptBackend.AddNotice(1000, 4, nil), IsNil)
	origNotices := promptBackend.BackendNotices(nil)
	c.Assert(origNotices, HasLen, 4)
	// Expire all the notices by re-recording them in the past
	for _, notice := range origNotices {
		notice.Reoccur(notice.LastRepeated().Add(-1000*time.Hour), nil, 0)
	}

	// Manually save to disk to ensure the expired notices are written to disk.
	// Notices are saved when adding a new notice, but that also drops expired
	// notices, so we need to save manually here to ensure expired notices are
	// not dropped.
	c.Assert(promptBackend.Save(), IsNil)

	// Initialize a new backend and check that it loads existing notices
	newBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Check(err, IsNil)

	afterNotices := newBackend.PromptBackend().BackendNotices(nil)
	c.Check(afterNotices, HasLen, 0)
	// Check that expired notices were not added
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000001"), IsNil)
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000002"), IsNil)
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000003"), IsNil)
	c.Check(newBackend.PromptBackend().BackendNotice("prompt-0000000000000004"), IsNil)

	// Add a new notice and make sure everything is fine
	c.Check(newBackend.PromptBackend().AddNotice(1000, 5, nil), IsNil)
	finalNotices := newBackend.PromptBackend().BackendNotices(nil)
	c.Check(finalNotices, HasLen, 1)
	c.Check(finalNotices[0].Key(), Equals, "0000000000000005")
}

func (s *noticebackendSuite) TestBackendNotice(c *C) {
	noticeBackend, err := apparmorprompting.NewNoticeBackends(s.noticeMgr)
	c.Assert(err, IsNil)
	ruleBackend := noticeBackend.RuleBackend()

	c.Check(ruleBackend.AddNotice(1234, 1, nil), IsNil)
	c.Check(ruleBackend.AddNotice(1234, 2, nil), IsNil)
	c.Check(ruleBackend.AddNotice(1234, 3, nil), IsNil)
	c.Check(ruleBackend.AddNotice(1234, 4, nil), IsNil)
	origNotices := ruleBackend.BackendNotices(nil)
	c.Assert(origNotices, HasLen, 4)
	// Expire the first two notices by re-recording them in the past
	for _, notice := range origNotices[:2] {
		notice.Reoccur(notice.LastRepeated().Add(-1000*time.Hour), nil, 0)
	}

	afterNotices := ruleBackend.BackendNotices(nil)
	c.Assert(afterNotices, HasLen, 2)

	// Check that non-expired notices can be found by ID
	notice := ruleBackend.BackendNotice("rule-0000000000000003")
	c.Assert(notice, NotNil)
	c.Check(notice.Key(), Equals, "0000000000000003")
	notice = ruleBackend.BackendNotice("rule-0000000000000004")
	c.Assert(notice, NotNil)
	c.Check(notice.Key(), Equals, "0000000000000004")

	// Check that expired notices can still be found by ID
	// XXX: we don't necessarily want to guarantee this, but test it for now
	notice = ruleBackend.BackendNotice("rule-0000000000000001")
	c.Assert(notice, NotNil)
	c.Check(notice.Key(), Equals, "0000000000000001")
	notice = ruleBackend.BackendNotice("rule-0000000000000002")
	c.Assert(notice, NotNil)
	c.Check(notice.Key(), Equals, "0000000000000002")

	// Check that a nonexistent notice returns nil
	notice = ruleBackend.BackendNotice("rule-0000000000000005")
	c.Assert(notice, IsNil)
	notice = ruleBackend.BackendNotice("foo")
	c.Assert(notice, IsNil)
}
