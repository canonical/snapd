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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/overlord/state"
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

	s.st = state.New(nil)
	s.defaultUser = 1000
}

func requestWithReplyChan(req *prompting.Request) (*prompting.Request, chan []string) {
	replyChan := make(chan []string, 1)
	injectReplyChan(req, replyChan)
	return req, replyChan
}

func injectReplyChan(req *prompting.Request, replyChan chan []string) *prompting.Request {
	req.Reply = func(allowedPerms []string) error {
		replyChan <- allowedPerms
		return nil
	}
	return req
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
	restore := apparmorprompting.MockListenerRegister(func() (apparmorprompting.ListenerBackend, error) {
		return nil, registerFailure
	})
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot register prompting listener: %v", registerFailure))
	c.Assert(mgr, IsNil)
}

func (s *apparmorpromptingSuite) TestNewErrorPromptDB(c *C) {
	_, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	// Prevent prompt backend from opening successfully
	maxIDFilepath := filepath.Join(dirs.SnapInterfacesRequestsRunDir, "request-prompt-max-id")
	c.Assert(os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755), IsNil)
	f, err := os.Create(maxIDFilepath)
	c.Assert(err, IsNil)
	c.Assert(f.Chmod(0o400), IsNil)
	defer f.Chmod(0o600)

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, ErrorMatches, "cannot open request prompts backend:.*")
	c.Assert(mgr, IsNil)

	checkListenerClosed(c, reqChan)
}

func checkListenerClosed(c *C, reqChan <-chan *prompting.Request) {
	select {
	case _, ok := <-reqChan:
		// reqChan was already closed
		c.Check(ok, Equals, false)
	case <-time.NewTimer(100 * time.Millisecond).C:
		c.Errorf("listener was not closed")
	}
}

func (s *apparmorpromptingSuite) TestNewErrorRuleDB(c *C) {
	_, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	// Prevent rule backend from opening successfully
	maxIDFilepath := filepath.Join(dirs.SnapInterfacesRequestsStateDir, "request-rule-max-id")
	c.Assert(os.MkdirAll(dirs.SnapInterfacesRequestsStateDir, 0o755), IsNil)
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
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	promptDB := mgr.PromptDB()
	c.Assert(promptDB, NotNil)
	ruleDB := mgr.RuleDB()
	c.Assert(ruleDB, NotNil)

	// Add rule so it can be found when trying to patch
	close(readyChan)
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/foo"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule, err := mgr.AddRule(s.defaultUser, "foo", "home", constraints)
	c.Assert(err, IsNil)

	err = mgr.Stop()
	c.Check(err, IsNil)

	// Check that the listener and prompt and rule backends were closed
	checkListenerClosed(c, reqChan)
	c.Check(promptDB.Close(), Equals, prompting_errors.ErrPromptsClosed)
	c.Check(ruleDB.Close(), Equals, prompting_errors.ErrRulesClosed)

	// Check that calls to API methods don't panic after backends have been closed
	_, err = mgr.Prompts(1000, false)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	_, err = mgr.PromptWithID(1000, rule.ID, false)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	_, err = mgr.HandleReply(1000, rule.ID, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "", true)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	_, err = mgr.Rules(1000, "foo", "bar")
	c.Check(err, IsNil) // rule backend supports getting rules even after closed
	_, err = mgr.AddRule(1000, "foo", "home", constraints)
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	_, err = mgr.RemoveRules(1000, "foo", "bar")
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	_, err = mgr.RuleWithID(1000, rule.ID)
	c.Check(err, IsNil) // rule backend supports getting rules even after closed
	_, err = mgr.PatchRule(1000, rule.ID, nil)
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
	_, err = mgr.RemoveRule(1000, rule.ID)
	c.Check(err, Equals, prompting_errors.ErrRulesClosed)
}

func (s *apparmorpromptingSuite) TestHandleListenerRequestDenyRoot(c *C) {
	_, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Send request for root
	req, replyChan := requestWithReplyChan(&prompting.Request{
		// Most fields don't matter here
		UID: 0,
	})
	reqChan <- req
	// Should get immediate denial
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(allowedPermissions, HasLen, 0)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestHandleListenerRequestErrors(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Close readyChan so we can check mgr.Prompts
	close(readyChan)

	clientActivity := true
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Fill the requestprompts backend until we hit its outstanding prompt
	// count limit
	maxOutstandingPromptsPerUser := 1000 // from requestprompts package
	for i := 0; i < maxOutstandingPromptsPerUser; i++ {
		req := &prompting.Request{
			PID:           1234,
			Cgroup:        "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
			AppArmorLabel: "snap.firefox.firefox",
			UID:           s.defaultUser,
			Path:          fmt.Sprintf("/home/test/%d", i),
			Interface:     "home",
			Permissions:   []string{"write"},
		}
		reqChan <- req
	}
	time.Sleep(10 * time.Millisecond)

	prompts, err = mgr.Prompts(s.defaultUser, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(len(prompts), Equals, maxOutstandingPromptsPerUser)

	// Now try to add one more request, it should fail
	logger.WithLoggerLock(func() {
		logbuf.Reset()
	})

	req, replyChan := requestWithReplyChan(&prompting.Request{
		PID:           1234,
		Cgroup:        "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		AppArmorLabel: "snap.firefox.firefox",
		UID:           s.defaultUser,
		Path:          fmt.Sprintf("/home/test/%d", maxOutstandingPromptsPerUser),
		Interface:     "home",
		Permissions:   []string{"write"},
	})
	reqChan <- req
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(allowedPermissions, DeepEquals, []string{})
	logger.WithLoggerLock(func() {
		c.Check(logbuf.String(), testutil.Contains,
			" WARNING: too many outstanding prompts for user 1000; auto-denying new one\n")
	})

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestHandleReplySimple(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	req, replyChan := requestWithReplyChan(&prompting.Request{})
	_, prompt := s.simulateRequest(c, reqChan, mgr, req, false)

	// Reply to the request
	constraintsJSON := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`["read"]`),
	}
	clientActivity := true
	satisfied, err := mgr.HandleReply(s.defaultUser, prompt.ID, constraintsJSON, prompting.OutcomeAllow, prompting.LifespanSingle, "", clientActivity)
	c.Check(err, IsNil)
	c.Check(satisfied, HasLen, 0)

	// Simulate the listener receiving the response
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	expectedPerms := []string{"read"}
	c.Check(allowedPermissions, DeepEquals, expectedPerms)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) simulateRequest(c *C, reqChan chan *prompting.Request, mgr *apparmorprompting.InterfacesRequestsManager, req *prompting.Request, shouldMerge bool) (*prompting.Request, *requestprompts.Prompt) {
	clientActivity := false
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	origPromptIDs := make(map[prompting.IDType]bool)
	for _, p := range prompts {
		origPromptIDs[p.ID] = true
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	// Simulate request from the kernel
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	// push a request
	reqChan <- req

	// Check that no error occurred
	time.Sleep(10 * time.Millisecond)
	logger.WithLoggerLock(func() { c.Assert(logbuf.String(), Equals, "") })

	// which should generate a notice
	s.st.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	n, err := s.st.WaitNotices(ctx, &state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: whenSent,
	})
	s.st.Unlock()
	c.Check(err, IsNil)
	c.Check(n, HasLen, 1)

	// Check prompts now
	prompts, err = mgr.Prompts(s.defaultUser, clientActivity)
	c.Assert(err, IsNil)

	if shouldMerge {
		c.Assert(prompts, HasLen, len(origPromptIDs))
		return req, nil
	}

	c.Assert(prompts, HasLen, len(origPromptIDs)+1)
	var prompt *requestprompts.Prompt
	for _, p := range prompts {
		if origPromptIDs[p.ID] {
			continue
		}
		prompt = p
		break
	}
	c.Assert(prompt, NotNil)
	expectedSnap := req.AppArmorLabel
	labelComponents := strings.Split(req.AppArmorLabel, ".")
	if len(labelComponents) == 3 {
		expectedSnap = labelComponents[1]
	}

	c.Check(prompt.Snap, Equals, expectedSnap)
	c.Check(prompt.PID, Equals, req.PID)
	c.Check(prompt.Cgroup, Equals, req.Cgroup)
	c.Check(prompt.Interface, Equals, "home")
	c.Check(prompt.Constraints.Path(), Equals, req.Path)

	// Check that we can query that prompt by ID
	promptByID, err := mgr.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(promptByID, Equals, prompt)

	// Return request and prompt
	return req, prompt
}

// fillInPartialRequest fills in any blank fields from the given request
// with default non-empty values.
func (s *apparmorpromptingSuite) fillInPartialRequest(req *prompting.Request) {
	if req.PID == 0 {
		req.PID = 1234
	}
	if req.Cgroup == "" {
		req.Cgroup = "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope"
	}
	if req.AppArmorLabel == "" {
		req.AppArmorLabel = "snap.firefox.firefox"
	}
	if req.UID == uint32(0) {
		req.UID = s.defaultUser
	}
	if req.Path == "" {
		req.Path = "/home/test/foo"
	}
	if req.Interface == "" {
		req.Interface = "home"
	}
	if req.Permissions == nil {
		req.Permissions = []string{"read"}
	}
}

var errNoReply = errors.New("no reply received")

func waitForReply(replyChan chan []string) ([]string, error) {
	select {
	case allowedPerms := <-replyChan:
		return allowedPerms, nil
	case <-time.NewTimer(100 * time.Millisecond).C:
		return nil, errNoReply
	}
}

func (s *apparmorpromptingSuite) TestHandleReplyErrors(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	_, prompt := s.simulateRequest(c, reqChan, mgr, &prompting.Request{}, false)

	// Wrong user ID
	clientActivity := true
	result, err := mgr.HandleReply(s.defaultUser+1, prompt.ID, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "", clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	c.Check(result, IsNil)

	// Wrong prompt ID
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID+1, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "", clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	c.Check(result, IsNil)

	// Invalid constraints
	invalidConstraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`["foo"]`),
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, invalidConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "", clientActivity)
	c.Check(err, ErrorMatches, "cannot decode request body into prompt reply: invalid permissions for home interface:.*")
	c.Check(result, IsNil)

	// Path not matched
	badPatternConstraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/other"`),
		"permissions":  json.RawMessage(`["read"]`),
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, badPatternConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "", clientActivity)
	c.Check(err, ErrorMatches, "path pattern in reply constraints does not match originally requested path.*")
	c.Check(result, IsNil)

	// Permissions not matched
	badPermissionConstraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/foo"`),
		"permissions":  json.RawMessage(`["write"]`),
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, badPermissionConstraints, prompting.OutcomeAllow, prompting.LifespanSingle, "", clientActivity)
	c.Check(err, ErrorMatches, "permissions in reply constraints do not include all requested permissions.*")
	c.Check(result, IsNil)

	// Conflicting rule
	// For this, need to add another rule to the DB first, then try to reply
	// with a rule which conflicts with it. Reuse badPatternConstraints.
	anotherConstraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/other"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"timespan","duration":"10s"}}`),
	}
	newRule, err := mgr.AddRule(s.defaultUser, "firefox", "home", anotherConstraints)
	c.Assert(err, IsNil)
	c.Assert(newRule, NotNil)
	conflictingOutcome := prompting.OutcomeDeny
	conflictingConstraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/{foo,other}"`),
		"permissions":  json.RawMessage(`["read"]`),
	}
	result, err = mgr.HandleReply(s.defaultUser, prompt.ID, conflictingConstraints, conflictingOutcome, prompting.LifespanForever, "", clientActivity)
	c.Check(err, ErrorMatches, "cannot add rule.*")
	c.Check(result, IsNil)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRuleAllowsNewPrompt(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// pretend that there are no pending requests to be re-sent
	close(readyChan)

	// Add allow rule to match read permission
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Add allow rule to match write permission
	constraints = prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"write":{"outcome":"allow","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Create request for read and write
	req, replyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read", "write"},
	})
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	reqChan <- req
	time.Sleep(10 * time.Millisecond)

	// Check that no prompts were created
	clientActivity := false
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Check that no notices were recorded
	s.checkRecordedPromptNotices(c, whenSent, 0)

	// Check that kernel received a reply
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	expectedPermissions := []string{"read", "write"}
	c.Check(allowedPermissions, DeepEquals, expectedPermissions)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) checkRecordedPromptNotices(c *C, since time.Time, count int) {
	s.st.Lock()
	n := s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsPromptNotice},
		After: since,
	})
	s.st.Unlock()
	c.Check(n, HasLen, count, Commentf("%+v", n))
}

func (s *apparmorpromptingSuite) checkRecordedRuleUpdateNotices(c *C, since time.Time, count int) {
	s.st.Lock()
	n := s.st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.InterfacesRequestsRuleUpdateNotice},
		After: since,
	})
	s.st.Unlock()
	c.Check(n, HasLen, count, Commentf("%+v", n))
}

func (s *apparmorpromptingSuite) TestExistingRulePartiallyAllowsNewPrompt(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// pretend that there are no pending requests to be re-sent
	close(readyChan)

	// Add rule to match read permission
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Do NOT add rule to match write permission

	// Create request for read and write
	partialReq := &prompting.Request{
		Permissions: []string{"read", "write"},
	}
	_, prompt := s.simulateRequest(c, reqChan, mgr, partialReq, false)

	// Check that prompt was created for outstanding "write" permission
	c.Check(prompt.Constraints.OutstandingPermissions(), DeepEquals, []string{"write"})

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRulePartiallyDeniesNewPrompt(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// pretend that there are no pending requests to be re-sent
	close(readyChan)

	// Add deny rule to match read permission
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"deny","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Add no rule for write permissions

	// Create request for read and write
	req, replyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read", "write"},
	})
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	reqChan <- req
	time.Sleep(10 * time.Millisecond)

	// Check that no prompts were created
	clientActivity := false
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Check that no notices were recorded
	s.checkRecordedPromptNotices(c, whenSent, 0)

	// Check that kernel received a reply
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Check(allowedPermissions, DeepEquals, []string{})

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestExistingRulesMixedMatchNewPromptDenies(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// pretend that there are no pending requests to be re-sent
	close(readyChan)

	// Add deny rule to match read permission
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"deny","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Add allow rule for write permissions
	constraints = prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"write":{"outcome":"allow","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Create request for read and write
	req, replyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read", "write"},
	})
	s.fillInPartialRequest(req)
	whenSent := time.Now()
	reqChan <- req
	time.Sleep(10 * time.Millisecond)

	// Check that no prompts were created
	clientActivity := false
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, HasLen, 0)

	// Check that no notices were recorded
	s.checkRecordedPromptNotices(c, whenSent, 0)

	// If there is an allow rule for some permissions and a deny rule for other
	// permissions, an allow response should be sent immediately for only the
	// previously-allowed permissions, and all denied permissions should be
	// automatically denied by the kernel.

	// Check that kernel received a reply
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	expectedPermissions := []string{"write"}
	c.Check(allowedPermissions, DeepEquals, expectedPermissions)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestNewRuleAllowExistingPrompt(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	// Add read request
	readReq, readReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read"},
	})
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Add request for write
	writeReq := &prompting.Request{
		Permissions: []string{"write"},
	}
	_, writePrompt := s.simulateRequest(c, reqChan, mgr, writeReq, false)

	// Add request for read and write
	rwReq := &prompting.Request{
		Permissions: []string{"read", "write"},
	}
	_, rwPrompt := s.simulateRequest(c, reqChan, mgr, rwReq, false)

	// Add rule to allow read request
	whenSent := time.Now()
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule, err := mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Check that kernel received a reply
	allowedPermissions, err := waitForReply(readReplyChan)
	c.Assert(err, IsNil)
	expectedPermissions := []string{"read"}
	c.Check(allowedPermissions, DeepEquals, expectedPermissions)

	// Check that read request prompt was satisfied
	clientActivity := false
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID, clientActivity)
	c.Check(err, NotNil)

	// Check that rwPrompt only has write permission left
	c.Check(rwPrompt.Constraints.OutstandingPermissions(), DeepEquals, []string{"write"})

	// Check that two prompts still exist
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(prompts, HasLen, 2)
	if !(writePrompt == prompts[0] || writePrompt == prompts[1]) {
		c.Errorf("write prompt not found")
	}
	if !(rwPrompt == prompts[0] || rwPrompt == prompts[1]) {
		c.Errorf("rw prompt not found")
	}

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, DeepEquals, []*requestrules.Rule{rule})

	// Check that notices were recorded for read prompt and rw prompt,
	// and for the rule
	s.checkRecordedPromptNotices(c, whenSent, 2)
	s.checkRecordedRuleUpdateNotices(c, whenSent, 1)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestNewRuleDenyExistingPrompt(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	// Add read request
	readReq, readReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read"},
	})
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Add request for write
	writeReq := &prompting.Request{
		Permissions: []string{"write"},
	}
	_, writePrompt := s.simulateRequest(c, reqChan, mgr, writeReq, false)

	// Add request for read and write
	rwReq, rwReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read", "write"},
	})
	_, rwPrompt := s.simulateRequest(c, reqChan, mgr, rwReq, false)

	// Add rule to deny read request
	whenSent := time.Now()
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"deny","lifespan":"forever"}}`),
	}
	rule, err := mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)

	// Check that kernel received replies for read and rw
	for _, replyChan := range []chan []string{readReplyChan, rwReplyChan} {
		allowedPermissions, err := waitForReply(replyChan)
		c.Assert(err, IsNil)
		c.Check(allowedPermissions, DeepEquals, []string{})
	}

	// Check that read and rw prompts were satisfied
	clientActivity := false
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID, clientActivity)
	c.Check(err, NotNil)
	_, err = mgr.PromptWithID(s.defaultUser, rwPrompt.ID, clientActivity)
	c.Check(err, NotNil)

	// Check that one prompt still exists
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Assert(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{writePrompt})

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, DeepEquals, []*requestrules.Rule{rule})

	// Check that notices were recorded for read prompt and rw prompt,
	// and for the rule
	s.checkRecordedPromptNotices(c, whenSent, 2)
	s.checkRecordedRuleUpdateNotices(c, whenSent, 1)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestReplyNewRuleHandlesExistingPrompt(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Already tested HandleReply errors, and that applyRuleToOutstandingPrompts
	// works correctly, so now just need to test that if reply creates a rule,
	// that rule applies to existing prompts.

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	// Add read request
	readReq, readReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read"},
	})
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Add request for write
	writeReq := &prompting.Request{
		Permissions: []string{"write"},
	}
	_, writePrompt := s.simulateRequest(c, reqChan, mgr, writeReq, false)

	// Add request for read and write
	rwReq, rwReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read", "write"},
	})
	_, rwPrompt := s.simulateRequest(c, reqChan, mgr, rwReq, false)

	// Reply to read prompt with denial
	whenSent := time.Now()
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`["read"]`),
	}
	clientActivity := true
	satisfiedPromptIDs, err := mgr.HandleReply(s.defaultUser, readPrompt.ID, constraints, prompting.OutcomeDeny, prompting.LifespanTimespan, "10s", clientActivity)
	c.Check(err, IsNil)

	// Check that rw prompt was also satisfied
	c.Check(satisfiedPromptIDs, DeepEquals, []prompting.IDType{rwPrompt.ID})

	// Check that kernel received replies for read and rw
	for _, replyChan := range []chan []string{readReplyChan, rwReplyChan} {
		allowedPermissions, err := waitForReply(replyChan)
		c.Assert(err, IsNil)
		c.Check(allowedPermissions, DeepEquals, []string{})
	}

	// Check that read and rw prompts no longer exist
	clientActivity = false
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID, clientActivity)
	c.Check(err, NotNil)
	_, err = mgr.PromptWithID(s.defaultUser, rwPrompt.ID, clientActivity)
	c.Check(err, NotNil)

	// Check that one prompt still exists
	prompts, err := mgr.Prompts(s.defaultUser, clientActivity)
	c.Assert(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{writePrompt})

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, HasLen, 1)

	// Check that notices were recorded for read prompt and rw prompt,
	// and for the rule
	s.checkRecordedPromptNotices(c, whenSent, 2)
	s.checkRecordedRuleUpdateNotices(c, whenSent, 1)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestReplyNewRuleAllowsFuturePromptsForever(c *C) {
	s.testReplyRuleHandlesFuturePrompts(c, prompting.OutcomeAllow, prompting.LifespanForever)
}

func (s *apparmorpromptingSuite) TestReplyNewRuleAllowsFuturePromptsTimespan(c *C) {
	s.testReplyRuleHandlesFuturePrompts(c, prompting.OutcomeAllow, prompting.LifespanTimespan)
}

func (s *apparmorpromptingSuite) TestReplyNewRuleDeniesFuturePromptsForever(c *C) {
	s.testReplyRuleHandlesFuturePrompts(c, prompting.OutcomeDeny, prompting.LifespanForever)
}

func (s *apparmorpromptingSuite) TestReplyNewRuleDeniesFuturePromptsTimespan(c *C) {
	s.testReplyRuleHandlesFuturePrompts(c, prompting.OutcomeDeny, prompting.LifespanTimespan)
}

func (s *apparmorpromptingSuite) testReplyRuleHandlesFuturePrompts(c *C, outcome prompting.OutcomeType, lifespan prompting.LifespanType) {
	duration := ""
	if lifespan == prompting.LifespanTimespan {
		duration = "10m"
	}

	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Already tested HandleReply errors, and that applyRuleToOutstandingPrompts
	// works correctly, so now just need to test that if reply creates a rule,
	// that rule applies to existing prompts.

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	// Add read request
	readReq, readReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read"},
	})
	_, readPrompt := s.simulateRequest(c, reqChan, mgr, readReq, false)

	// Reply to read prompt with denial for read and write
	whenSent := time.Now()
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`["read","write"]`),
	}
	clientActivity := false
	satisfiedPromptIDs, err := mgr.HandleReply(s.defaultUser, readPrompt.ID, constraints, outcome, lifespan, duration, clientActivity)
	c.Check(err, IsNil)

	// Check that kernel received reply
	allowedPermissions, err := waitForReply(readReplyChan)
	c.Assert(err, IsNil)
	switch outcome {
	case prompting.OutcomeAllow:
		c.Check(allowedPermissions, DeepEquals, []string{"read"})
	case prompting.OutcomeDeny:
		c.Check(allowedPermissions, HasLen, 0)
	}

	// Check that no other prompts were satisfied
	c.Check(satisfiedPromptIDs, HasLen, 0)

	// Check that new rule exists
	rules, err := mgr.Rules(s.defaultUser, "", "")
	c.Assert(err, IsNil)
	c.Check(rules, HasLen, 1)

	// Check that read prompt no longer exists
	clientActivity = false
	_, err = mgr.PromptWithID(s.defaultUser, readPrompt.ID, clientActivity)
	c.Check(err, NotNil)

	// Check that notices were recorded for read prompt and new rule.
	s.checkRecordedPromptNotices(c, whenSent, 1)
	s.checkRecordedRuleUpdateNotices(c, whenSent, 1)

	whenSent = time.Now()

	// Add request for write
	writeReq, writeReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"write"},
	})
	s.fillInPartialRequest(writeReq)
	reqChan <- writeReq

	// Add request for read and write
	rwReq, rwReplyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read", "write"},
	})
	s.fillInPartialRequest(rwReq)
	reqChan <- rwReq

	// Check that kernel received replies
	for _, pair := range []struct {
		req       *prompting.Request
		replyChan chan []string
	}{
		{writeReq, writeReplyChan},
		{rwReq, rwReplyChan},
	} {
		allowedPermissions, err := waitForReply(pair.replyChan)
		c.Assert(err, IsNil)
		switch outcome {
		case prompting.OutcomeAllow:
			c.Check(allowedPermissions, DeepEquals, pair.req.Permissions)
		case prompting.OutcomeDeny:
			c.Check(allowedPermissions, HasLen, 0)
		}
	}

	// Check that no notices were recorded
	s.checkRecordedPromptNotices(c, whenSent, 0)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestRequestMerged(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Requests with identical *original* abstract permissions are merged into
	// the existing prompt

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	// Create request for read and write
	partialReq := &prompting.Request{
		Permissions: []string{"read", "write"},
	}
	_, prompt := s.simulateRequest(c, reqChan, mgr, partialReq, false)

	// Create identical request, it should merge
	identicalReq := &prompting.Request{
		Permissions: []string{"read", "write"},
	}
	s.simulateRequest(c, reqChan, mgr, identicalReq, true)

	// Add rule to satisfy the read permission
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	_, err = mgr.AddRule(s.defaultUser, prompt.Snap, prompt.Interface, constraints)
	c.Assert(err, IsNil)

	// Create identical request again, it should merge even though some
	// permissions have been satisfied
	identicalReqAgain := &prompting.Request{
		Permissions: []string{"read", "write"},
	}
	s.simulateRequest(c, reqChan, mgr, identicalReqAgain, true)

	// Now new requests for just write access will have identical outstanding
	// permissions, but not identical original permissions, so should not merge
	readReq := &prompting.Request{
		Permissions: []string{"write"},
	}
	s.simulateRequest(c, reqChan, mgr, readReq, false)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestRules(c *C) {
	readyChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Close readyChan so we can add rules
	close(readyChan)

	mgr, rules := s.prepManagerWithRules(c)

	// Assume returned rules are in the order in which they were added.
	// This is true now but may not remain true in the future

	userRules, err := mgr.Rules(s.defaultUser, "", "")
	c.Check(err, IsNil)
	c.Check(userRules, DeepEquals, rules[:3])

	ifaceRules, err := mgr.Rules(s.defaultUser, "", "home")
	c.Check(err, IsNil)
	c.Check(ifaceRules, DeepEquals, rules[:2])

	snapRules, err := mgr.Rules(s.defaultUser, "firefox", "")
	c.Check(err, IsNil)
	c.Check(snapRules, DeepEquals, []*requestrules.Rule{rules[0], rules[2]})

	snapIfaceRules, err := mgr.Rules(s.defaultUser, "firefox", "home")
	c.Check(err, IsNil)
	c.Check(snapIfaceRules, DeepEquals, []*requestrules.Rule{rules[0]})

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) prepManagerWithRules(c *C) (mgr *apparmorprompting.InterfacesRequestsManager, rules []*requestrules.Rule) {
	var err error
	mgr, err = apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	whenAdded := time.Now()

	// Add rule for firefox and home
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/1"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule1, err := mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)
	rules = append(rules, rule1)

	// Add rule for thunderbird and home
	constraints = prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/2"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule2, err := mgr.AddRule(s.defaultUser, "thunderbird", "home", constraints)
	c.Assert(err, IsNil)
	rules = append(rules, rule2)

	// Add rule for firefox and camera
	constraints = prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/dev/video3"`),
		"permissions":  json.RawMessage(`{"access":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule3, err := mgr.AddRule(s.defaultUser, "firefox", "camera", constraints)
	c.Assert(err, IsNil)
	rules = append(rules, rule3)

	// Add rule for firefox and home, but for a different user
	constraints = prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/4"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule4, err := mgr.AddRule(s.defaultUser+1, "firefox", "home", constraints)
	c.Assert(err, IsNil)
	rules = append(rules, rule4)

	// Check that four notices were recorded
	s.checkRecordedRuleUpdateNotices(c, whenAdded, 4)

	return mgr, rules
}

func (s *apparmorpromptingSuite) TestRemoveRulesInterface(c *C) {
	readyChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Close readyChan so we can add rules
	close(readyChan)

	mgr, rules := s.prepManagerWithRules(c)

	// Assume returned rules are in the order in which they were added.
	// This is true now but may not remain true in the future

	whenRemoved := time.Now()

	ifaceRules, err := mgr.RemoveRules(s.defaultUser, "", "home")
	c.Check(err, IsNil)
	c.Check(ifaceRules, DeepEquals, rules[:2])

	userRules, err := mgr.Rules(s.defaultUser, "", "")
	c.Check(err, IsNil)
	c.Check(userRules, DeepEquals, rules[2:3])

	s.checkRecordedRuleUpdateNotices(c, whenRemoved, 2)
	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestRemoveRulesSnap(c *C) {
	readyChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Close readyChan so we can add rules
	close(readyChan)

	mgr, rules := s.prepManagerWithRules(c)

	// Assume returned rules are in the order in which they were added.
	// This is true now but may not remain true in the future

	whenRemoved := time.Now()

	snapRules, err := mgr.RemoveRules(s.defaultUser, "firefox", "")
	c.Check(err, IsNil)
	c.Check(snapRules, DeepEquals, []*requestrules.Rule{rules[0], rules[2]})

	userRules, err := mgr.Rules(s.defaultUser, "", "")
	c.Check(err, IsNil)
	c.Check(userRules, DeepEquals, rules[1:2])

	s.checkRecordedRuleUpdateNotices(c, whenRemoved, 2)
	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestRemoveRulesSnapInterface(c *C) {
	readyChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	// Close readyChan so we can add rules
	close(readyChan)

	mgr, rules := s.prepManagerWithRules(c)

	// Assume returned rules are in the order in which they were added.
	// This is true now but may not remain true in the future

	whenRemoved := time.Now()

	snapRules, err := mgr.RemoveRules(s.defaultUser, "firefox", "home")
	c.Check(err, IsNil)
	c.Check(snapRules, DeepEquals, []*requestrules.Rule{rules[0]})

	userRules, err := mgr.Rules(s.defaultUser, "", "")
	c.Check(err, IsNil)
	c.Check(userRules, DeepEquals, rules[1:3])

	s.checkRecordedRuleUpdateNotices(c, whenRemoved, 1)
	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestAddRuleWithIDPatchRemove(c *C) {
	readyChan, reqChan, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// simulateRequest checks mgr.Prompts, so make sure we close readyChan first
	close(readyChan)

	// Add read request
	req, replyChan := requestWithReplyChan(&prompting.Request{
		Permissions: []string{"read"},
	})
	_, prompt := s.simulateRequest(c, reqChan, mgr, req, false)

	// Add write rule
	whenAdded := time.Now()
	constraints := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/**"`),
		"permissions":  json.RawMessage(`{"write":{"outcome":"allow","lifespan":"forever"}}`),
	}
	rule, err := mgr.AddRule(s.defaultUser, "firefox", "home", constraints)
	c.Assert(err, IsNil)
	s.checkRecordedRuleUpdateNotices(c, whenAdded, 1)
	s.checkRecordedPromptNotices(c, whenAdded, 0)

	// Test RuleWithID
	whenAccessed := time.Now()
	retrieved, err := mgr.RuleWithID(rule.User, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(retrieved, Equals, rule)
	s.checkRecordedRuleUpdateNotices(c, whenAccessed, 0)

	// Check prompt still exists and no prompt notices recorded since before
	// the rule was added
	clientActivity := false
	retrievedPrompt, err := mgr.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(retrievedPrompt, Equals, prompt)
	s.checkRecordedPromptNotices(c, whenAccessed, 0)

	// Patch rule to now cover the outstanding prompt
	whenPatched := time.Now()
	constraintsPatch := prompting.ConstraintsJSON{
		"path-pattern": json.RawMessage(`"/home/test/{foo,bar,baz}"`),
		"permissions":  json.RawMessage(`{"read":{"outcome":"allow","lifespan":"forever"},"write":{"outcome":"allow","lifespan":"forever"}}`),
	}
	patched, err := mgr.PatchRule(s.defaultUser, rule.ID, constraintsPatch)
	c.Assert(err, IsNil)
	s.checkRecordedRuleUpdateNotices(c, whenPatched, 1)

	// Check that RuleWithID with original ID returns patched rule
	retrieved, err = mgr.RuleWithID(rule.User, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(retrieved, Equals, patched)

	// Check that prompt has been satisfied
	_, err = mgr.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Assert(err, Equals, prompting_errors.ErrPromptNotFound)
	s.checkRecordedPromptNotices(c, whenPatched, 1)

	// Check that a reply has been received
	allowedPermissions, err := waitForReply(replyChan)
	c.Assert(err, IsNil)
	c.Assert(allowedPermissions, DeepEquals, []string{"read"})

	// Remove the rule
	whenRemoved := time.Now()
	removed, err := mgr.RemoveRule(rule.User, rule.ID)
	c.Assert(err, IsNil)
	c.Assert(removed, Equals, patched)
	s.checkRecordedRuleUpdateNotices(c, whenRemoved, 1)

	// Check that it can no longer be found
	_, err = mgr.RuleWithID(rule.User, rule.ID)
	c.Assert(err, Equals, prompting_errors.ErrRuleNotFound)
	rules, err := mgr.Rules(rule.User, "", "")
	c.Assert(err, IsNil)
	c.Assert(rules, HasLen, 0)

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestListenerReadyCausesPromptsHandleReadying(c *C) {
	readyChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	handleStarted := make(chan struct{})
	finishHandle := make(chan struct{})
	restore = apparmorprompting.MockPromptsHandleReadying(func(pdb *requestprompts.PromptDB) error {
		close(handleStarted)
		<-finishHandle
		return nil
	})
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	// Check that the callback has not started yet
	select {
	case <-handleStarted:
		c.Errorf("HandleReadying started before ready was signalled")
	case <-time.NewTimer(10 * time.Millisecond).C:
		// all good
	}

	// Signal ready
	close(readyChan)

	// Check that the callback has now started
	select {
	case <-handleStarted:
		// all good
	case <-time.NewTimer(time.Second).C:
		c.Errorf("HandleReadying failed to start after ready was signalled")
	}

	// Check that the manager is not yet ready
	select {
	case <-mgr.Ready():
		c.Errorf("manager is ready before HandleReadying returned")
	case <-time.NewTimer(10 * time.Millisecond).C:
		// all good
	}

	// Tell the HandleReadying to return
	close(finishHandle)

	// Check that the manager is now ready
	select {
	case <-mgr.Ready():
		// all good
	case <-time.NewTimer(time.Second).C:
		c.Errorf("manager failed to become ready after HandleReadying returned")
	}

	c.Assert(mgr.Stop(), IsNil)
}

func (s *apparmorpromptingSuite) TestListenerReadyBlocksRepliesNewRules(c *C) {
	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		prompts, err := mgr.Prompts(1000, false)
		c.Check(err, IsNil)
		c.Check(prompts, HasLen, 0)
	})

	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		_, err := mgr.PromptWithID(1000, 0, false)
		c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	})

	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		_, err := mgr.HandleReply(1000, 0, nil, prompting.OutcomeAllow, prompting.LifespanSingle, "", false)
		c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	})

	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		_, err := mgr.AddRule(1000, "foo", "bar", nil)
		c.Check(err, NotNil)
	})

	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		rules, err := mgr.RemoveRules(1000, "foo", "bar")
		c.Check(err, IsNil)
		c.Check(rules, HasLen, 0)
	})

	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		_, err := mgr.PatchRule(1000, 0, nil)
		c.Check(err, Equals, prompting_errors.ErrRuleNotFound)
	})

	s.testReadyBlocks(c, func(mgr *apparmorprompting.InterfacesRequestsManager) {
		_, err := mgr.RemoveRule(1000, 0)
		c.Check(err, NotNil)
	})
}

func (s *apparmorpromptingSuite) testReadyBlocks(c *C, f func(mgr *apparmorprompting.InterfacesRequestsManager)) {
	readyChan, _, restore := apparmorprompting.MockListener()
	defer restore()

	mgr, err := apparmorprompting.New(s.st)
	c.Assert(err, IsNil)

	startChan := make(chan time.Time)
	doneChan := make(chan time.Time)
	go func() {
		startChan <- time.Now()
		f(mgr)
		doneChan <- time.Now()
	}()
	// Wait for function to start
	<-startChan
	// Wait another few milliseconds
	<-time.NewTimer(10 * time.Millisecond).C
	// Record the current time before readying
	now := time.Now()
	close(readyChan)
	finished := <-doneChan
	// Check that the finished time was after the ready time
	c.Check(finished.After(now), Equals, true, Commentf("finish time failed to be after ready time"))

	// restore races with listenerRun and listenerReqs, so wait for everything
	// to stop before restoring.
	err = mgr.Stop()
	c.Check(err, IsNil)
}
