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

package apparmorprompting

import (
	"errors"
	"fmt"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

var (
	// Allow mocking the listener for tests
	listenerRegister = listener.Register
	listenerClose    = (*listener.Listener).Close
	listenerRun      = (*listener.Listener).Run
	listenerReady    = (*listener.Listener).Ready
	listenerReqs     = (*listener.Listener).Reqs

	requestReply = func(req *listener.Request, allowedPermission notify.AppArmorPermission) error {
		return req.Reply(allowedPermission)
	}

	promptsHandleReadying = (*requestprompts.PromptDB).HandleReadying

	promptingInterfaceFromTagsets = prompting.InterfaceFromTagsets
)

// A Manager holds outstanding prompts and mediates their replies, further it
// stores and applies persistent rules.
type Manager interface {
	Prompts(userID uint32, clientActivity bool) ([]*requestprompts.Prompt, error)
	PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error)
	HandleReply(userID uint32, promptID prompting.IDType, replyConstraints *prompting.ReplyConstraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) ([]prompting.IDType, error)
	Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error)
	AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints) (*requestrules.Rule, error)
	RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error)
	RuleWithID(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error)
	PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatch *prompting.RuleConstraintsPatch) (*requestrules.Rule, error)
	RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error)
}

// verify that InterfacesRequestsManager implements Manager
var _ Manager = (*InterfacesRequestsManager)(nil)

type InterfacesRequestsManager struct {
	tomb tomb.Tomb
	// The lock should be held for writing when acting on the manager in a way
	// which requires synchronization between the prompts and rules databases,
	// or when removing those databases. The lock can be held for reading when
	// acting on just one or the other, as each has an internal mutex as well.
	lock     sync.RWMutex
	listener *listener.Listener
	prompts  *requestprompts.PromptDB
	rules    *requestrules.RuleDB

	// ready should block method calls which depend on the manager having re-
	// received all pending requests which were previously sent before snapd
	// restarted (or timed out attempting to do so). It is closed to broadcast
	// that method calls may proceed.
	//
	// We can't block on listenerReady directly in the method calls, as that
	// would mean that after the manager receives the final request, there
	// would be a race between the method calls unblocking and the manager
	// actually getting the chance to handle the request.
	ready chan struct{}

	notices *noticeBackend
}

func New(noticeMgr *notices.NoticeManager) (m *InterfacesRequestsManager, retErr error) {
	noticesBackend, err := initializeNoticeBackend(noticeMgr)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize prompting notices backend: %w", err)
	}

	listenerBackend, err := listenerRegister()
	if err != nil {
		return nil, fmt.Errorf("cannot register prompting listener: %w", err)
	}
	defer func() {
		if retErr != nil {
			listenerClose(listenerBackend)
		}
	}()

	promptsBackend, err := requestprompts.New(noticesBackend.promptBackend.addNotice)
	if err != nil {
		return nil, fmt.Errorf("cannot open request prompts backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			promptsBackend.Close()
		}
	}()

	rulesBackend, err := requestrules.New(noticesBackend.ruleBackend.addNotice)
	if err != nil {
		return nil, fmt.Errorf("cannot open request rules backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			rulesBackend.Close()
		}
	}()

	if err = noticesBackend.registerWithManager(noticeMgr); err != nil {
		// This should never occur
		return nil, fmt.Errorf("cannot register notice backends for prompting manager: %w", err)
	}

	m = &InterfacesRequestsManager{
		listener: listenerBackend,
		prompts:  promptsBackend,
		rules:    rulesBackend,
		ready:    make(chan struct{}),
		notices:  noticesBackend,
	}

	m.tomb.Go(m.run)

	return m, nil
}

// Run is the main run loop for the manager, and must be called using tomb.Go.
func (m *InterfacesRequestsManager) run() error {
	m.tomb.Go(func() error {
		logger.Debugf("starting prompting listener")
		// listener.Run will return an error if and only if there's a real
		// error, not if Close() is called. But Close() is called only by
		// disconnect(), which is itself only called when this run() function
		// returns, which only occurs when the manager tomb is dying. So we
		// don't need to worry about the listener returning nil when we don't
		// already expect to be exiting.
		return listenerRun(m.listener)
	})

	defer func() {
		// Ensure that m.ready ends up closed, since we'll never have the
		// opportunity to close it again after this function returns, and we
		// don't want to leave method calls blocked forever.
		select {
		case <-m.ready:
			// is already closed
		default:
			close(m.ready)
		}
	}()

run_loop:
	for {
		logger.Debugf("waiting prompt loop")
		select {
		case <-m.listenerReadyForTheFirstTime():
			// The previous request we processed was the final pending one
			// waiting to be resent, or the listener timed out waiting for one.
			// In either case, let method calls proceed.
			logger.Debugf("received ready signal from the listener")
			// Tell the requestprompts backend that the listener is ready, so
			// it can discard ID mappings for requests which have not been
			// re-received. For completeness, acquire the lock, though this
			// shouldn't really be necessary since the API endpoints are still
			// waiting on the manager signalling readiness. The lock only needs
			// to be held for reading, since no synchronization is required
			// between the rules and prompts backends, and the prompts backend
			// has an internal mutex.
			m.lock.RLock()
			promptsHandleReadying(m.prompts)
			m.lock.RUnlock()
			// Close the ready channel to unblock method calls.
			close(m.ready)
		case req, ok := <-listenerReqs(m.listener):
			if !ok {
				// Reqs() closed, so an error occurred in the listener. In
				// production, the listener does not close itself on error, so
				// this should never actually occur.
				//
				// If Stop() were called, it would set the tomb to dying and
				// the other select case would have occurred, since the
				// disconnect() method is the only place in the manager where
				// the listener Close() method is called, and disconnect() is
				// only called when this function returns.
				//
				// The listener Close() method has already been called by the
				// listener itself, and the tomb error will be set to the error
				// value of the Run() call from the previous tracked goroutine.
				logger.Debugf("prompting listener closed requests channel")
				break run_loop
			}

			logger.Debugf("received from kernel requests channel: %+v", req)
			if err := m.handleListenerReq(req); err != nil {
				logger.Noticef("error while handling request: %+v", err)
			}
		case <-m.tomb.Dying():
			logger.Debugf("InterfacesRequestsManager tomb is dying with error %v, disconnecting", m.tomb.Err())
			break run_loop
		}
	}
	return m.disconnect()
}

func (m *InterfacesRequestsManager) listenerReadyForTheFirstTime() <-chan struct{} {
	select {
	case <-m.ready:
		// We already closed m.ready, so the listener previously readied and we
		// handled it. So return something which will never signal.
		return nil
	default:
		// We haven't handled a ready signal yet, so return the real thing.
		return listenerReady(m.listener)
	}
}

func (m *InterfacesRequestsManager) handleListenerReq(req *listener.Request) error {
	userID := uint32(req.SubjectUID)
	if userID == 0 {
		// Deny any request for the root user
		return requestReply(req, nil)
	}
	snap := req.Label // Default to apparmor label, in case process is not a snap
	if tag, err := naming.ParseSecurityTag(req.Label); err == nil {
		// the triggering process is a snap, so use instance name as snap field
		snap = tag.InstanceName()
	}

	iface, err := promptingInterfaceFromTagsets(req.Tagsets)
	if err != nil {
		if errors.Is(err, prompting_errors.ErrNoInterfaceTags) {
			// There were no tags registered with a snapd interface, so we
			// default to the "home" interface.
			iface = "home"
		} else {
			// There was either more than one interface associated with tags, or
			// none which applied to all requested permissions. Since we can't
			// decide which interface to use, automatically deny this request.
			logger.Noticef("error while selecting interface from metadata tags: %v", err)
			return requestReply(req, nil)
		}
	}

	path := req.Path

	permissions, err := prompting.AbstractPermissionsFromAppArmorPermissions(iface, req.Permission)
	if err != nil {
		logger.Noticef("error while parsing AppArmor permissions: %v", err)
		return requestReply(req, nil)
	}

	// we're done with early checks, serious business starts now, and we can
	// take the lock
	m.lock.Lock()
	defer m.lock.Unlock()

	allowedPerms, matchedDenyRule, outstandingPerms, err := m.rules.IsRequestAllowed(userID, snap, iface, path, permissions)
	if err != nil || matchedDenyRule || len(outstandingPerms) == 0 {
		switch {
		case err != nil:
			logger.Noticef("error while checking request against existing rules: %v", err)
		case matchedDenyRule:
			logger.Debugf("request denied by existing rule: %+v", req)
		case len(outstandingPerms) == 0:
			logger.Debugf("request allowed by existing rule: %+v", req)
		}
		// Allow any requested permissions which were explicitly allowed by
		// existing rules (there may be no such permissions) and let the
		// listener deny all permissions which were not explicitly included in
		// the allowed permissions.
		allowedPermission, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, allowedPerms)
		// Error should not occur, but if it does, allowedPermission is set to
		// empty, leaving it to the listener to default deny all permissions.
		return requestReply(req, allowedPermission)
	}

	// Request not satisfied by any of existing rules, record a prompt for the user

	metadata := &prompting.Metadata{
		User:      userID,
		Snap:      snap,
		PID:       req.PID,
		Interface: iface,
	}

	newPrompt, merged, err := m.prompts.AddOrMerge(metadata, path, permissions, outstandingPerms, req)
	if err != nil {
		logger.Noticef("error while checking request against prompt DB: %v", err)

		// We weren't able to create a new prompt, so respond with the best
		// information we have, which is to allow any permissions which were
		// allowed by existing rules, and let the listener deny the rest.
		allowedPermission, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, allowedPerms)
		// Error should not occur, but if it does, allowedPermission is set to
		// empty, leaving it to the listener to default deny all permissions.
		return requestReply(req, allowedPermission)
	}

	if merged {
		logger.Debugf("new prompt merged with identical existing prompt: %+v", newPrompt)
	} else {
		logger.Debugf("adding prompt to internal storage: %+v", newPrompt)
	}

	return nil
}

func (m *InterfacesRequestsManager) disconnect() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	var errs []error
	if m.listener != nil {
		errs = append(errs, listenerClose(m.listener))
	}
	if m.prompts != nil {
		errs = append(errs, m.prompts.Close())
	}
	if m.rules != nil {
		errs = append(errs, m.rules.Close())
	}

	return strutil.JoinErrors(errs...)
}

// Stop closes the listener, prompt DB, and rule DB. Stop is idempotent, and
// the receiver cannot be started or used after it has been stopped.
func (m *InterfacesRequestsManager) Stop() error {
	m.tomb.Kill(nil)
	// Kill causes the run loop to exit and call disconnect()
	return m.tomb.Wait()
}

// Prompts returns all prompts for the user with the given user ID.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) Prompts(userID uint32, clientActivity bool) ([]*requestprompts.Prompt, error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.prompts.Prompts(userID, clientActivity)
}

// PromptWithID returns the prompt with the given ID for the given user.
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) PromptWithID(userID uint32, promptID prompting.IDType, clientActivity bool) (*requestprompts.Prompt, error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.prompts.PromptWithID(userID, promptID, clientActivity)
}

// HandleReply checks that the given reply contents are valid, satisfies the
// original request, and does not conflict with any existing rules (if lifespan
// is not "single"). If all of these are true, sends a reply for the prompt with
// the given ID, and both creates a new rule and checks any outstanding prompts
// against it, if the lifespan is not "single".
//
// If clientActivity is true, reset the expiration timeout for prompts for
// the given user.
func (m *InterfacesRequestsManager) HandleReply(userID uint32, promptID prompting.IDType, replyConstraints *prompting.ReplyConstraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string, clientActivity bool) (satisfiedPromptIDs []prompting.IDType, retErr error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	m.lock.Lock()
	defer m.lock.Unlock()

	prompt, err := m.prompts.PromptWithID(userID, promptID, clientActivity)
	if err != nil {
		return nil, err
	}

	// Validate reply constraints and convert them to Constraints, which have
	// dedicated PermissionEntry values for each permission in the reply.
	// Outcome and lifespan are validated while unmarshalling, and duration is
	// validated against the given lifespan when constructing the Constraints.
	constraints, err := replyConstraints.ToConstraints(prompt.Interface, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}

	// Check that constraints matches original requested path.
	// AppArmor is responsible for pre-vetting that all paths which appear
	// in requests from the kernel are allowed by the appropriate
	// interfaces, so we do not assert anything else particular about the
	// constraints, such as check that the path pattern does not match
	// any paths not granted by the interface.
	// TODO: Should this be reconsidered?
	matches, err := constraints.Match(prompt.Constraints.Path())
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, &prompting_errors.RequestedPathNotMatchedError{
			Requested: prompt.Constraints.Path(),
			Replied:   constraints.PathPattern.String(),
		}
	}

	// XXX: do we want to allow only replying to a select subset of permissions, and
	// auto-deny the rest?
	contained := constraints.ContainPermissions(prompt.Constraints.OutstandingPermissions())
	if !contained {
		return nil, &prompting_errors.RequestedPermissionsNotMatchedError{
			Requested: prompt.Constraints.OutstandingPermissions(),
			Replied:   replyConstraints.Permissions, // equivalent to keys of constraints.Permissions
		}
	}

	// It is important that a lock is held while checking for conflicts with
	// other rules so that if the rule is eventually removed due to an error,
	// no prompts can have been matched against it in the meantime.
	var newRule *requestrules.Rule
	if lifespan != prompting.LifespanSingle {
		// Check that adding the rule doesn't conflict with other rules
		newRule, err = m.rules.AddRule(userID, prompt.Snap, prompt.Interface, constraints)
		if err != nil {
			// Rule conflicts with existing rule (at least one identical pattern
			// variant and permission). This should be considered a bad reply,
			// since the user should only be prompted for permissions and paths
			// which are not already covered.
			return nil, err
		}

		defer func() {
			if retErr != nil || lifespan == prompting.LifespanSingle {
				m.rules.RemoveRule(userID, newRule.ID)
			}
		}()
	}

	prompt, retErr = m.prompts.Reply(userID, promptID, outcome, clientActivity)
	if retErr != nil {
		// Error should not occur unless the listener has closed
		return nil, retErr
	}

	if lifespan == prompting.LifespanSingle {
		return []prompting.IDType{}, nil
	}

	// Apply new rule to outstanding prompts.
	satisfiedPromptIDs = m.applyRuleToOutstandingPrompts(newRule)

	return satisfiedPromptIDs, nil
}

func (m *InterfacesRequestsManager) applyRuleToOutstandingPrompts(rule *requestrules.Rule) []prompting.IDType {
	metadata := &prompting.Metadata{
		User:      rule.User,
		Snap:      rule.Snap,
		Interface: rule.Interface,
	}
	satisfiedPromptIDs, err := m.prompts.HandleNewRule(metadata, rule.Constraints)
	if err != nil {
		// The rule's constraints and outcome were already validated, so an
		// error should not occur here unless the prompt DB was already closed.
		logger.Noticef("error when handling new rule: %v", err)
	}
	return satisfiedPromptIDs
}

// Rules returns all rules for the user with the given user ID and,
// optionally, only those for the given snap and/or interface.
func (m *InterfacesRequestsManager) Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if snap != "" {
		if iface != "" {
			rules := m.rules.RulesForSnapInterface(userID, snap, iface)
			return rules, nil
		}
		rules := m.rules.RulesForSnap(userID, snap)
		return rules, nil
	}
	if iface != "" {
		rules := m.rules.RulesForInterface(userID, iface)
		return rules, nil
	}
	rules := m.rules.Rules(userID)
	return rules, nil
}

// AddRule creates a new rule with the given contents and then checks it against
// outstanding prompts, resolving any prompts which it satisfies.
func (m *InterfacesRequestsManager) AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints) (*requestrules.Rule, error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	m.lock.Lock()
	defer m.lock.Unlock()

	newRule, err := m.rules.AddRule(userID, snap, iface, constraints)
	if err != nil {
		return nil, err
	}
	// Apply new rule to outstanding prompts.
	m.applyRuleToOutstandingPrompts(newRule)
	return newRule, nil
}

// RemoveRules removes all rules for the user with the given user ID and the
// given snap and/or interface. Snap and iface can't both be unspecified.
func (m *InterfacesRequestsManager) RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	// The lock need only be held for reading, since no synchronization is
	// required between the rules and prompts backends, and the rules backend
	// has an internal mutex.
	m.lock.RLock()
	defer m.lock.RUnlock()

	if snap == "" && iface == "" {
		// The caller should ensure that this is not the case.
		return nil, fmt.Errorf("cannot remove rules for unspecified snap and interface")
	}
	if snap != "" {
		if iface != "" {
			return m.rules.RemoveRulesForSnapInterface(userID, snap, iface)
		} else {
			return m.rules.RemoveRulesForSnap(userID, snap)
		}
	}
	return m.rules.RemoveRulesForInterface(userID, iface)
}

// RuleWithID returns the rule with the given ID for the given user.
func (m *InterfacesRequestsManager) RuleWithID(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	rule, err := m.rules.RuleWithID(userID, ruleID)
	return rule, err
}

// PatchRule updates the rule with the given ID using the provided contents.
// Any of the given fields which are empty/nil are not updated in the rule.
func (m *InterfacesRequestsManager) PatchRule(userID uint32, ruleID prompting.IDType, constraintsPatch *prompting.RuleConstraintsPatch) (*requestrules.Rule, error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	m.lock.Lock()
	defer m.lock.Unlock()

	patchedRule, err := m.rules.PatchRule(userID, ruleID, constraintsPatch)
	if err != nil {
		return nil, err
	}
	// Apply patched rule to outstanding prompts.
	m.applyRuleToOutstandingPrompts(patchedRule)
	return patchedRule, nil
}

func (m *InterfacesRequestsManager) RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	// Wait until the listener has re-sent pending requests and prompts have
	// been re-created.
	<-m.ready

	// The lock need only be held for reading, since no synchronization is
	// required between the rules and prompts backends, and the rules backend
	// has an internal mutex.
	m.lock.RLock()
	defer m.lock.RUnlock()

	rule, err := m.rules.RemoveRule(userID, ruleID)
	return rule, err
}
