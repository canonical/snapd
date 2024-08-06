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
)

type Interface interface {
	Connect() error
	Run() error
	Stop() error
}

type Prompting struct {
	tomb     tomb.Tomb
	listener *listener.Listener
	prompts  *requestprompts.PromptDB
	rules    *requestrules.RuleDB

	notifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error
	notifyRule   func(userID uint32, ruleID prompting.IDType, data map[string]string) error
}

func New(s *state.State) Interface {
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
	p := &Prompting{
		notifyPrompt: notifyPrompt,
		notifyRule:   notifyRule,
	}
	return p
}

func (p *Prompting) Connect() (retErr error) {
	if p.prompts != nil {
		return fmt.Errorf("cannot connect: listener is already registered")
	}
	listenerBackend, err := listenerRegister()
	if err != nil {
		return fmt.Errorf("cannot register prompting listener: %w", err)
	}
	defer func() {
		if retErr != nil {
			listenerBackend.Close()
		}
	}()
	promptsBackend, err := requestprompts.New(p.notifyPrompt)
	if err != nil {
		return fmt.Errorf("cannot open request prompts backend: %w", err)
	}
	defer func() {
		if retErr != nil {
			promptsBackend.Close()
		}
	}()
	rulesBackend, err := requestrules.New(p.notifyRule)
	if err != nil {
		return fmt.Errorf("cannot open request rules backend: %w", err)
	}
	p.listener = listenerBackend
	p.prompts = promptsBackend
	p.rules = rulesBackend
	return nil
}

var (
	listenerRegister = listener.Register
)

func (p *Prompting) disconnect() error {
	if p.listener == nil {
		return nil
	}
	defer func() {
		p.listener = nil
	}()
	if err := listenerClose(p.listener); err != nil {
		return err
	}
	return nil
}

var listenerClose = func(l *listener.Listener) error {
	return l.Close()
}

func (p *Prompting) Run() error {
	p.tomb.Go(func() error {
		if p.listener == nil {
			logger.Noticef("listener is nil, exiting Prompting.Run() early")
			return fmt.Errorf("listener is nil, cannot run AppArmor prompting")
		}
		p.tomb.Go(func() error {
			logger.Noticef("starting listener")
			if err := listenerRun(p.listener); err != listener.ErrClosed {
				return err
			}
			return nil
		})

		logger.Noticef("ready for prompts")
		for {
			logger.Debugf("waiting prompt loop")
			select {
			case req, ok := <-listenerReqs(p.listener):
				if !ok {
					// Reqs() closed, so either errored or Stop() was called.
					// In either case, the listener Close() method has already
					// been called, and the tomb error will be set to the return
					// value of the Run() call from the previous tracked goroutine.
					logger.Noticef("listener closed requests channel")
					return p.disconnect()
				}
				// XXX: this debug log leaks information about internal activity
				logger.Debugf("Got from kernel req chan: %v", req)
				if err := p.handleListenerReq(req); err != nil { // no use multithreading, since IsPathAllowed locks
					// XXX: this log leaks information about internal activity
					logger.Noticef("Error while handling request: %+v", err)
				}
			case <-p.tomb.Dying():
				logger.Noticef("Prompting tomb is dying, disconnecting")
				return p.disconnect()
			}
		}
	})
	return nil
}

var (
	listenerRun = func(l *listener.Listener) error {
		return l.Run()
	}
	listenerReqs = func(l *listener.Listener) <-chan *listener.Request {
		return l.Reqs()
	}
)

func (p *Prompting) handleListenerReq(req *listener.Request) error {
	userID := uint32(req.SubjectUID())
	var snap string
	tag, err := naming.ParseSecurityTag(req.Label())
	if err != nil {
		// the triggering process is not a snap, so treat apparmor label as snap field
		snap = req.Label()
	} else {
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
		if yesNo, err := p.rules.IsPathAllowed(userID, snap, iface, path, perm); err == nil {
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
		responsePermissions, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, satisfiedPerms)
		// Error should not occur, but if it does, responsePermissions are set
		// to none, leaving it to AppArmor to default deny
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

	newPrompt, merged, err := p.prompts.AddOrMerge(metadata, path, permissions, remainingPerms, req)
	if err != nil {
		// XXX: this debug log leaks information about internal activity
		logger.Debugf("error while adding prompt to prompt DB: %+v: %v", req, err)
		// Allow any satisfied permissions, AppArmor will auto-deny the rest
		responsePermissions, _ := prompting.AbstractPermissionsToAppArmorPermissions(iface, satisfiedPerms)
		// Error should not occur, but if it does, responsePermissions are set
		// to none, leaving it to AppArmor to default deny
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

func (p *Prompting) Stop() error {
	p.tomb.Kill(nil)
	p.prompts.Close()
	return p.tomb.Wait()
}

// GetPrompts returns all prompts for the user with the given user ID.
func (p *Prompting) GetPrompts(userID uint32) ([]*requestprompts.Prompt, error) {
	return p.prompts.Prompts(userID)
}

// GetPromptWithID returns the prompt with the given ID for the given user.
func (p *Prompting) GetPromptWithID(userID uint32, promptID prompting.IDType) (*requestprompts.Prompt, error) {
	return p.prompts.PromptWithID(userID, promptID)
}

// HandleReply checks that the given reply contents are valid, satisfies the
// original request, and does not conflict with any existing rules (if lifespan
// is not "single"). If all of these are true, sends a reply for the prompt with
// the given ID, and both creates a new rule and checks any outstanding prompts
// against it, if the lifespan is not "single".
func (p *Prompting) HandleReply(userID uint32, promptID prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (satisfiedPromptIDs []prompting.IDType, retErr error) {
	prompt, err := p.prompts.PromptWithID(userID, promptID)
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

	// TODO: once support for sending back bitmask of allowed permissions lands,
	// do we want to allow only replying to a select subset of permissions, and
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
		newRule, err = p.rules.AddRule(userID, prompt.Snap, prompt.Interface, constraints, outcome, lifespan, duration)
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
				p.rules.RemoveRule(userID, newRule.ID)
			}
		}()
	}

	prompt, retErr = p.prompts.Reply(userID, promptID, outcome)
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
	satisfiedPromptIDs, err = p.prompts.HandleNewRule(metadata, newRule.Constraints, newRule.Outcome)
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

// GetRules returns all rules for the user with the given user ID and,
// optionally, only those for the given snap and/or interface.
func (p *Prompting) GetRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	if snap != "" {
		if iface != "" {
			rules := p.rules.RulesForSnapInterface(userID, snap, iface)
			return rules, nil
		}
		rules := p.rules.RulesForSnap(userID, snap)
		return rules, nil
	}
	if iface != "" {
		rules := p.rules.RulesForInterface(userID, iface)
		return rules, nil
	}
	rules := p.rules.Rules(userID)
	return rules, nil
}

// AddRule creates a new rule with the given contents and then checks it against
// outstanding prompts, resolving any prompts which it satisfies.
func (p *Prompting) AddRule(userID uint32, snap string, iface string, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*requestrules.Rule, error) {
	newRule, err := p.rules.AddRule(userID, snap, iface, constraints, outcome, lifespan, duration)
	if err != nil {
		return nil, err
	}
	// Apply new rule to outstanding prompts.
	metadata := &prompting.Metadata{
		User:      userID,
		Snap:      newRule.Snap,
		Interface: newRule.Interface,
	}
	if _, err = p.prompts.HandleNewRule(metadata, newRule.Constraints, newRule.Outcome); err != nil {
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
func (p *Prompting) RemoveRules(userID uint32, snap string, iface string) ([]*requestrules.Rule, error) {
	if snap == "" && iface == "" {
		return nil, fmt.Errorf("cannot remove rules for unspecified snap and interface")
	}
	if snap != "" {
		if iface != "" {
			return p.rules.RemoveRulesForSnapInterface(userID, snap, iface)
		} else {
			return p.rules.RemoveRulesForSnap(userID, snap)
		}
	}
	return p.rules.RemoveRulesForInterface(userID, iface)
}

// GetRule returns the rule with the given ID for the given user.
func (p *Prompting) GetRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	rule, err := p.rules.RuleWithID(userID, ruleID)
	return rule, err
}

// PatchRule updates the rule with the given ID using the provided contents.
// Any of the given fields which are empty/nil are not updated in the rule.
func (p *Prompting) PatchRule(userID uint32, ruleID prompting.IDType, constraints *prompting.Constraints, outcome prompting.OutcomeType, lifespan prompting.LifespanType, duration string) (*requestrules.Rule, error) {
	return p.rules.PatchRule(userID, ruleID, constraints, outcome, lifespan, duration)
}

func (p *Prompting) RemoveRule(userID uint32, ruleID prompting.IDType) (*requestrules.Rule, error) {
	rule, err := p.rules.RemoveRule(userID, ruleID)
	return rule, err
}
