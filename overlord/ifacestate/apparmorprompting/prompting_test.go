// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type apparmorpromptingSuite struct {
	testutil.BaseTest

	st *state.State

	defaultUser uint32
}

var _ = Suite(&apparmorpromptingSuite{})

func (s *apparmorpromptingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	os.MkdirAll(dirs.SnapRunDir, 0o755)

	s.st = state.New(nil)
	s.defaultUser = 1000
}

func (s *apparmorpromptingSuite) TestNew(c *C) {
	_, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	err = mgr.Stop()
	c.Assert(err, IsNil)
}

func (s *apparmorpromptingSuite) TestNewErrorListener(c *C) {
	registerFailure := fmt.Errorf("failed to register listener")
	restore := apparmorprompting.MockListenerRegister(func() (*listener.Listener, error) {
		return nil, registerFailure
	})
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot register request listener: %v", registerFailure))
	c.Assert(mgr, IsNil)
}

func (s *apparmorpromptingSuite) TestNewErrorPromptDB(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Prevent prompt backend from opening successfully
	maxIDFilepath := filepath.Join(dirs.SnapRunDir, "request-prompt-max-id")
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0o700), IsNil)
	f, err := os.Create(maxIDFilepath)
	c.Assert(err, IsNil)
	c.Assert(f.Chmod(0o400), IsNil)
	defer f.Chmod(0o600)

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, "cannot open request prompts backend:.*")
	c.Assert(mgr, IsNil)

	checkListenerClosed(c, reqChan)
}

func checkListenerClosed(c *C, reqChan <-chan *listener.Request) {
	select {
	case _, ok := <-reqChan:
		// reqChan was already closed
		c.Check(ok, Equals, false)
	case <-time.NewTimer(100 * time.Millisecond).C:
		c.Errorf("listener was not closed")
	}
}

func (s *apparmorpromptingSuite) TestNewErrorRuleDB(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Prevent rule backend from opening successfully
	maxIDFilepath := filepath.Join(prompting.StateDir(), "request-rule-max-id")
	c.Assert(os.MkdirAll(prompting.StateDir(), 0o700), IsNil)
	f, err := os.Create(maxIDFilepath)
	c.Assert(err, IsNil)
	c.Assert(f.Chmod(0o400), IsNil)
	defer f.Chmod(0o600)

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, "cannot open request rules backend:.*")
	c.Assert(mgr, IsNil)

	// Check that listener was closed
	checkListenerClosed(c, reqChan)
	// Ideally, we'd check that the prompt DB is also closed, but since the
	// InterfaceManager was never returned, it and the prompt DB pointed to
	// by it should be garbage collected, at least. The code calls Close(),
	// so we're pretty confident all is well.
}

func (s *apparmorpromptingSuite) TestStop(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	promptDB := mgr.PromptDB()
	c.Assert(promptDB, NotNil)
	ruleDB := mgr.RuleDB()
	c.Assert(ruleDB, NotNil)

	err = mgr.Stop()
	c.Check(err, IsNil)

	// Check that the listener and prompt and rule backends were closed
	checkListenerClosed(c, reqChan)
	c.Check(promptDB.Close(), Equals, requestprompts.ErrClosed)
	c.Check(ruleDB.Close(), Equals, requestrules.ErrClosed)

	// Check that current backends are nil
	c.Check(mgr.PromptDB(), IsNil)
	c.Check(mgr.RuleDB(), IsNil)
}

func (s *apparmorpromptingSuite) TestHandleListenerErrors(c *C) {
	reqChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Send request with invalid permissions
	logbuf, restore := logger.MockLogger()
	defer restore()
	req := &listener.Request{
		// Most fields don't matter here
		Permission: notify.FilePermission(0),
	}
	reqChan <- req
	time.Sleep(10 * time.Millisecond)
	c.Check(fmt.Errorf("%#v", logbuf.String()), ErrorMatches, ".*error while parsing AppArmor permissions: cannot get abstract permissions from empty AppArmor permissions.*")

	// Fill the requestprompts backend until we hit its outstanding prompt
	// count limit
	maxOutstandingPromptsPerUser := 1000 // from requestprompts package
	for i := 0; i < maxOutstandingPromptsPerUser; i++ {
		req := &listener.Request{
			Label:      "snap.firefox.firefox",
			SubjectUID: s.defaultUser,
			Path:       fmt.Sprintf("/home/test/%d", i),
			Class:      notify.AA_CLASS_FILE,
			Permission: notify.AA_MAY_APPEND,
		}
		reqChan <- req
	}
	time.Sleep(10 * time.Millisecond)
	prompts, err = mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)
	c.Assert(len(prompts), Equals, maxOutstandingPromptsPerUser)
	// Now try to add one more request, it should fail
	logbuf, restore = logger.MockLogger()
	defer restore()
	req = &listener.Request{
		Label:      "snap.firefox.firefox",
		SubjectUID: s.defaultUser,
		Path:       fmt.Sprintf("/home/test/%d", maxOutstandingPromptsPerUser),
		Class:      notify.AA_CLASS_FILE,
		Permission: notify.AA_MAY_APPEND,
	}
	reqChan <- req
	time.Sleep(10 * time.Millisecond)
	c.Check(fmt.Errorf("%#v", logbuf.String()), ErrorMatches, ".*Error while handling request: cannot get abstract permissions from empty AppArmor permissions.*")
}

func (s *apparmorpromptingSuite) TestHandleReplySimple(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, req, prompt := s.prepManagerWithSinglePrompt(c, reqChan)

	// Reply to the request
	constraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"read"},
	}
	satisfied, err := mgr.HandleReply(s.defaultUser, prompt.ID, &constraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	// Simulate the listener receiving the response
	resp, err := waitForReply(replyChan)
	c.Assert(err, IsNil)

	c.Check(resp.Request, Equals, req)
	c.Check(resp.Response.Allow, Equals, true)
	aaPerms, err := prompting.AbstractPermissionsToAppArmorPermissions("home", constraints.Permissions)
	c.Check(err, IsNil)
	c.Check(resp.Response.Permission, Equals, aaPerms)
}

func (s *apparmorpromptingSuite) prepManagerWithSinglePrompt(c *C, reqChan chan *listener.Request) (*apparmorprompting.InterfacesRequestsManager, *listener.Request, *requestprompts.Prompt) {
	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	prompts, err := mgr.Prompts(s.defaultUser)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	path := "/home/test/foo"

	// Simulate request from the kernel
	req := &listener.Request{
		Label:      "snap.firefox.firefox",
		SubjectUID: s.defaultUser,
		Path:       path,
		Class:      notify.AA_CLASS_FILE,
		Permission: notify.AA_MAY_READ,
	}
	reqChan <- req

	// Check that prompt exists
	prompts, err = mgr.Prompts(s.defaultUser)
	c.Assert(err, IsNil)
	c.Assert(prompts, HasLen, 1)
	prompt := prompts[0]
	c.Check(prompt.Snap, Equals, "firefox")
	c.Check(prompt.Interface, Equals, "home")
	c.Check(prompt.Constraints.Path(), Equals, path)
	c.Check(prompt.Constraints.RemainingPermissions(), DeepEquals, []string{"read"})

	// Check that we can query that prompt by ID
	promptByID, err := mgr.PromptWithID(s.defaultUser, prompt.ID)
	c.Check(err, IsNil)
	c.Check(promptByID, Equals, prompt)

	// Return manager, request, and prompt
	return mgr, req, prompt
}

func mustParsePathPattern(c *C, pattern string) *patterns.PathPattern {
	parsed, err := patterns.ParsePathPattern(pattern)
	c.Assert(err, IsNil)
	return parsed
}

var errNoReply = errors.New("no reply received")

func waitForReply(replyChan chan apparmorprompting.RequestResponse) (*apparmorprompting.RequestResponse, error) {
	select {
	case resp := <-replyChan:
		return &resp, nil
	case <-time.NewTimer(100 * time.Millisecond).C:
		return nil, errNoReply
	}
}

func (s *apparmorpromptingSuite) TestHandleReplyErrors(c *C) {
	reqChan, replyChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, _, prompt := s.prepManagerWithSinglePrompt(c, reqChan)

	// Wrong user ID
	result, err := mgr.HandleReply(s.defaultUser+1, prompt.ID, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, Equals, requestprompts.ErrNotFound)
	c.Check(result, IsNil)

	// Wrong prompt ID
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID+1, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, Equals, requestprompts.ErrNotFound)
	c.Check(result, IsNil)

	// Invalid constraints
	invalidConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/**"),
		Permissions: []string{"foo"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &invalidConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, ErrorMatches, "invalid constraints.*")
	c.Check(result, IsNil)

	// Path not matched
	badPatternConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/other"),
		Permissions: []string{"read"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &badPatternConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, ErrorMatches, "constraints in reply do not match original request.*")
	c.Check(result, IsNil)

	// Permissions not matched
	badPermissionConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/foo"),
		Permissions: []string{"write"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &badPermissionConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "")
	c.Check(err, ErrorMatches, "replied permissions do not include all requested permissions.*")
	c.Check(result, IsNil)

	// Conflicting rule
	// For this, need to add another rule to the DB first, then try to reply
	// with a rule which conflicts with it. Reuse badPatternConstraints.
	newRule, err := mgr.AddRule(s.defaultUser, "firefox", "home", &badPatternConstraints, prompting.OutcomeAllow, prompting.LifespanTimespan, "10s")
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)
	conflictingConstraints := prompting.Constraints{
		PathPattern: mustParsePathPattern(c, "/home/test/{foo,other}"),
		Permissions: []string{"read"},
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, &conflictingConstraints, prompting.OutcomeAllow, prompting.LifespanForever, "")
	c.Check(err, ErrorMatches, "cannot add rule.*")
	c.Check(result, IsNil)

	// Should not have received a reply
	_, err = waitForReply(replyChan)
	c.Assert(err, NotNil)
}

func (s *apparmorpromptingSuite) TestRequestMerged(c *C) {
	// Requests with identical *original* abstract permissions are merged into
	// the existing prompt
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestExistingRuleAllowsNewPrompt(c *C) {
	// If rules allow all of a request's permissions, that request is auto-allowed
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestExistingRulePartiallyAllowsNewPrompt(c *C) {
	// If rules allow some of a request's permissions, a prompt should be
	// created for the remaining permissions, but the full list of original
	// permissions (including those satisfied by existing rules) should be
	// preserved in the prompt, and replying to the prompt should result in a
	// response to the kernel with all (recognized) permissions from the
	// original request.
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestExistingRulePartiallyDeniesNewPrompt(c *C) {
	// Partial denial should result in immediate request denial
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestNewRuleHandlesExistingPrompt(c *C) {
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestReplyNewRuleHandlesExistingPrompt(c *C) {
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestRules(c *C) {
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestRemoveRules(c *C) {
	c.Fatalf("TODO")
}

func (s *apparmorpromptingSuite) TestAddRuleWithIDPatchRemove(c *C) {
	c.Fatalf("TODO")
}
