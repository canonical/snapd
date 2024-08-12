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

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	ErrPromptingNotEnabled = errors.New("AppArmor Prompting is not enabled")

	// Allow mocking the listener for tests
	listenerRegister = listener.Register
	listenerClose    = func(l *listener.Listener) error { return l.Close() }
	listenerRun      = func(l *listener.Listener) error { return l.Run() }
	listenerReqs     = func(l *listener.Listener) <-chan *listener.Request { return l.Reqs() }
)

type InterfacesRequestsManager struct {
	tomb     tomb.Tomb
	listener *listener.Listener
	prompts  *requestprompts.PromptDB
	rules    *requestrules.RuleDB

	notifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error
	notifyRule   func(userID uint32, ruleID prompting.IDType, data map[string]string) error
}

func New(s *state.State) (m *InterfacesRequestsManager, retErr error) {
	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		// TODO: add some sort of queue so that notifyPrompt function can return
		// quickly without waiting for state lock and AddNotice() to return.
		s.Lock()
		defer s.Unlock()
		options := state.AddNoticeOptions{
			Data: data,
		}
		_, err := s.AddNotice(&userID, state.InterfacesRequestsPromptNotice, promptID.String(), &options)
		return err
	}
	notifyRule := func(userID uint32, ruleID prompting.IDType, data map[string]string) error {
		// TODO: add some sort of queue so that notifyPrompt function can return
		// quickly without waiting for state lock and AddNotice() to return.
		s.Lock()
		defer s.Unlock()
		options := state.AddNoticeOptions{
			Data: data,
		}
		_, err := s.AddNotice(&userID, state.InterfacesRequestsRuleUpdateNotice, ruleID.String(), &options)
		return err
	}

	listenerBackend, err := listenerRegister()
	if err != nil {
		return nil, fmt.Errorf("cannot register request listener: %w", err)
	}
	defer func() {
		if retErr != nil {
			listenerBackend.Close()
		}
	}()

	promptsBackend, err := requestprompts.New(m.notifyPrompt)
	if err != nil {
		return nil, fmt.Errorf("cannot open request prompts backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			promptsBackend.Close()
		}
	}()

	rulesBackend, err := requestrules.New(m.notifyRule)
	if err != nil {
		return nil, fmt.Errorf("cannot open request rules backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			rulesBackend.Close()
		}
	}()

	m = &InterfacesRequestsManager{
		listener:     listenerBackend,
		prompts:      promptsBackend,
		rules:        rulesBackend,
		notifyPrompt: notifyPrompt,
		notifyRule:   notifyRule,
	}

	m.tomb.Go(m.run)

	return m, nil
}

func (m *InterfacesRequestsManager) run() error {
	m.tomb.Go(func() error {
		logger.Noticef("starting listener")
		if err := listenerRun(m.listener); err != listener.ErrClosed {
			return err
		}
		return nil
	})

	logger.Noticef("ready for prompts")
run_loop:
	for {
		logger.Debugf("waiting prompt loop")
		select {
		case req, ok := <-listenerReqs(m.listener):
			if !ok {
				// Reqs() closed, so either errored or Stop() was called.
				// In either case, the listener Close() method has already
				// been called, and the tomb error will be set to the return
				// value of the Run() call from the previous tracked goroutine.
				logger.Noticef("listener closed requests channel")
				return m.disconnect()
			}
			// XXX: this debug log leaks information about internal activity
			logger.Debugf("Got from kernel req chan: %v", req)
			if err := m.handleListenerReq(req); err != nil { // no use multithreading, since IsPathAllowed locks
				// XXX: this log leaks information about internal activity
				logger.Noticef("Error while handling request: %+v", err)
			}
		case <-m.tomb.Dying():
			logger.Noticef("InterfacesRequestsManager tomb is dying, disconnecting")
			break run_loop
		}
	}
	return m.disconnect()
}

func (m *InterfacesRequestsManager) handleListenerReq(req *listener.Request) error {
	userID := uint32(req.SubjectUID())
	// TODO: immediately deny if req.SubjectUID() == 0 (root)
	snap := req.Label() // Default to apparmor label, in case process is not a snap
	tag, err := naming.ParseSecurityTag(req.Label())
	if err == nil {
		// the triggering process is not a snap, so treat apparmor label as snap field
		snap = tag.InstanceName()
	}

	// TODO: when we support interfaces beyond "home", do a proper selection here
	iface := "home"

	path := req.Path()

	permissions, err := prompting.AbstractPermissionsFromAppArmorPermissions(iface, req.Permission())
	if err != nil {
		// XXX: this log leaks information about internal activity
		logger.Noticef("error while parsing AppArmor permissions: %v", err)
		response := listener.Response{
			Allow:      false,
			Permission: req.Permission(),
		}
		return req.Reply(&response)
	}

	remainingPerms := make([]string, 0, len(permissions))
	satisfiedPerms := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		if yesNo, err := m.rules.IsPathAllowed(userID, snap, iface, path, perm); err == nil {
			if !yesNo {
				// XXX: this debug log leaks information about internal activity
				logger.Debugf("request denied by existing rule: %+v", req)
				response := listener.Response{
					Allow:      false,
					Permission: req.Permission(),
				}
				return req.Reply(&response)
			} else {
				satisfiedPerms = append(satisfiedPerms, perm)
			}
		} else {
			// No matching rule found
			remainingPerms = append(remainingPerms, perm)
		}
	}
	if len(remainingPerms) == 0 {
		// XXX: this debug log leaks information about internal activity
		logger.Debugf("request allowed by existing rule: %+v", req)

		// We don't want to just send back req.Permission() here, since that
		// could include unrecognized permissions which were discarded, and
		// were not matched by an existing rule. So only respond with the
		// permissions which were matched and allowed, the kernel will
		// auto-deny any which are not included.
		responsePermissions, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, satisfiedPerms)
		// Error should not occur, but if it does, responsePermissions are set
		// to none, leaving it to AppArmor to default deny.

		response := listener.Response{
			Allow:      true,
			Permission: responsePermissions,
		}
		return req.Reply(&response)
	}

	metadata := &prompting.Metadata{
		User:      userID,
		Snap:      snap,
		Interface: iface,
	}

	newPrompt, merged, err := m.prompts.AddOrMerge(metadata, path, permissions, remainingPerms, req)
	if err != nil {
		// XXX: this debug log leaks information about internal activity
		logger.Debugf("error while adding prompt to prompt DB: %+v: %v", req, err)

		// We weren't able to create a new prompt, so respond with the best
		// information we have, which is to allow any permissions which were
		// automatically allowed by existing rules, and auto-deny the rest.
		responsePermissions, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, satisfiedPerms)
		// Error should not occur, but if it does, responsePermissions are set
		// to none, leaving it to AppArmor to default deny.
		response := listener.Response{
			Allow:      true,
			Permission: responsePermissions,
		}
		return req.Reply(&response)
	}
	if merged {
		// XXX: this debug log leaks information about internal activity
		logger.Debugf("new prompt merged with identical existing prompt: %+v", newPrompt)
		return nil
	}

	// XXX: this debug log leaks information about internal activity
	logger.Debugf("adding prompt to internal storage: %+v", newPrompt)

	return nil
}

func (m *InterfacesRequestsManager) disconnect() error {
	var errs []error
	if m.listener != nil {
		errs = append(errs, listenerClose(m.listener))
		m.listener = nil
	}
	if m.prompts != nil {
		errs = append(errs, m.prompts.Close())
		m.prompts = nil
	}
	if m.rules != nil {
		errs = append(errs, m.rules.Close())
		m.rules = nil
	}

	return errorsJoin(errs...)
}

// errorsJoin returns an error that wraps the given errors.
// Any nil error values are discarded.
// errorsJoin returns nil if every value in errs is nil.
//
// TODO: replace with errors.Join() once we're on golang v1.20+
func errorsJoin(errs ...error) error {
	var nonNilErrs []error
	for _, e := range errs {
		if e != nil {
			nonNilErrs = append(nonNilErrs, e)
		}
	}
	if len(nonNilErrs) == 0 {
		return nil
	}
	err := nonNilErrs[0]
	for _, e := range nonNilErrs[1:] {
		err = fmt.Errorf("%w\n%v", err, e)
	}
	return err
}

func (m *InterfacesRequestsManager) Stop() error {
	m.tomb.Kill(nil)
	m.prompts.Close()
	return m.tomb.Wait()
}

// Prompts returns all prompts for the user with the given user ID.
func (m *InterfacesRequestsManager) Prompts(userID uint32) ([]*requestprompts.Prompt, error) {
	return m.prompts.Prompts(userID)
}

// PromptWithID returns the prompt with the given ID for the given user.
func (m *InterfacesRequestsManager) PromptWithID(userID uint32, promptID prompting.IDType) (*requestprompts.Prompt, error) {
	return m.prompts.PromptWithID(userID, promptID)
}

// HandleReply checks that the given reply contents are valid, satisfies the
// original request, and does not conflict with any existing rules (if lifespan
// is not "single"). If all of these are true, sends a reply for the prompt with
// the given ID, and both creates a new rule and checks any outstanding prompts
// against it, if the lifespan is not "single".
func (m *InterfacesRequestsManager) HandleReply(userID uint32, promptID prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (satisfiedPromptIDs []prompting.IDType, retErr error) {
	prompt, err := m.prompts.PromptWithID(userID, promptID)
	if err != nil {
		return nil, err
	}

	// Outcome and lifesnap are validated while unmarshalling, and duration is
	// validated when the rule is being added. So only need to validate
	// constraints.
	if err := constraints.ValidateForInterface(prompt.Interface); err != nil {
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
		return nil, fmt.Errorf("constraints in reply do not match original request: '%v' does not match '%v'; please try again", constraints, prompt.Constraints)
	}

	// XXX: do we want to allow only replying to a select subset of permissions, and
	// auto-deny the rest?
	contained := constraints.ContainPermissions(prompt.Constraints.RemainingPermissions())
	if !contained {
		return nil, fmt.Errorf("replied permissions do not include all requested permissions: requested %v, replied %v; please try again", prompt.Constraints.RemainingPermissions(), constraints.Permissions)
	}

	// TODO: a lock should be held while checking for conflicts with other rules
	// so that if the rule is eventually removed due to an error, no prompts can
	// have been matched against it in the meantime.
	// A RWMutex over prompts and rules should work well, and could potentially
	// replace the internal mutexes in those packages.
	var newRule *requestrules.Rule
	if lifespan != prompting.LifespanSingle {
		// Check that adding the rule doesn't conflict with other rules
		newRule, err = m.rules.AddRule(userID, prompt.Snap, prompt.Interface, constraints, outcome, lifespan, duration)
		if err != nil {
			// Rule conflicts with existing rule (at least one identical pattern
			// variant and permission). This should be considered a bad reply,
			// since the user should only be prompted for permissions and paths
			// which are not already covered.

			// TODO: there are scenarios where this could reasonably happen, so
			// better to retry adding the new rule after removing any conflicts
			// with existing rules. Likely, the new rule should replace the old.
			// A new requestrules.ForceAddRule() might be the best way.

			return nil, err
		}

		defer func() {
			if retErr != nil || lifespan == prompting.LifespanSingle {
				m.rules.RemoveRule(userID, newRule.ID)
			}
		}()
	}

	prompt, retErr = m.prompts.Reply(userID, promptID, outcome)
	if retErr != nil {
		return nil, retErr
	}

	if lifespan == prompting.LifespanSingle {
		return []prompting.IDType{}, nil
	}

	// Apply new rule to outstanding prompts.
	metadata := &prompting.Metadata{
		User:      userID,
		Snap:      newRule.Snap,
		Interface: newRule.Interface,
	}
	satisfiedPromptIDs, err = m.prompts.HandleNewRule(metadata, newRule.Constraints, newRule.Outcome)
	if err != nil {
		// Should not occur, as outcome and constraints have already been
		// validated. However, it's possible an error could occur if the prompt
		// DB was already closed. This should be an internal error. However, we
		// can't un-send the reply, and this should only be the case if the
		// prompting system is shutting down, so don't actually return an error.

		// XXX: this log leaks information about internal activity
		logger.Noticef("WARNING: error when handling new rule as a result of reply: %v", err)
	}

	return satisfiedPromptIDs, nil
}

// Rules returns all rules for the user with the given user ID and,
// optionally, only those for the given snap and/or interface.
func (m *InterfacesRequestsManager) Rules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
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
func (m *InterfacesRequestsManager) AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*requestrules.Rule, error) {
	newRule, err := m.rules.AddRule(userID, snap, iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	// Apply new rule to outstanding prompts.
	metadata := &prompting.Metadata{
		User:      userID,
		Snap:      newRule.Snap,
		Interface: newRule.Interface,
	}
	if _, err = m.prompts.HandleNewRule(metadata, newRule.Constraints, newRule.Outcome); err != nil {
		// Should not occur, as outcome and constraints have already been
		// validated. However, it's possible an error could occur if the prompt
		// DB was already closed. This should be an internal error. However,
		// this should only be the case if the prompting system is shutting
		// down, so don't actually return an error.

		// XXX: this log leaks information about internal activity
		logger.Noticef("WARNING: error when handling new rule as a result of reply: %v", err)
	}
	return newRule, nil
}

// RemoveRules removes all rules for the user with the given user ID and the
// given snap and/or interface. Snap and iface can't both be unspecified.
func (m *InterfacesRequestsManager) RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	if snap == "" && iface == "" {
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
	rule, err := m.rules.RuleWithID(userID, ruleID)
	return rule, err
}

// PatchRule updates the rule with the given ID using the provided contents.
// Any of the given fields which are empty/nil are not updated in the rule.
func (m *InterfacesRequestsManager) PatchRule(userID uint32, ruleID prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*requestrules.Rule, error) {
	return m.rules.PatchRule(userID, ruleID, constraints, outcome, lifespan, duration)
}

func (m *InterfacesRequestsManager) RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	rule, err := m.rules.RemoveRule(userID, ruleID)
	return rule, err
}
