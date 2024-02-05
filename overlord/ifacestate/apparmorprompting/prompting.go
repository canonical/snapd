package apparmorprompting

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/accessrules"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/promptrequests"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
)

var PromptingEnabled = func() bool {
	return features.AppArmorPrompting.IsEnabled() && notify.SupportAvailable()
}

type Interface interface {
	Connect() error
	Run() error
	Stop() error
}

type Prompting struct {
	tomb     tomb.Tomb
	listener *listener.Listener
	requests *promptrequests.RequestDB
	rules    *accessrules.AccessRuleDB

	notifyRequest func(userID uint32, requestID string, options *state.AddNoticeOptions) error
	notifyRule    func(userID uint32, ruleID string, options *state.AddNoticeOptions) error
}

func New(s *state.State) Interface {
	notifyRequest := func(userID uint32, requestID string, options *state.AddNoticeOptions) error {
		s.Lock()
		defer s.Unlock()
		_, err := s.AddNotice(&userID, state.PromptingRequestNotice, requestID, options)
		return err
	}
	notifyRule := func(userID uint32, ruleID string, options *state.AddNoticeOptions) error {
		s.Lock()
		defer s.Unlock()
		_, err := s.AddNotice(&userID, state.PromptingRuleUpdateNotice, ruleID, options)
		return err
	}
	p := &Prompting{
		notifyRequest: notifyRequest,
		notifyRule:    notifyRule,
	}
	return p
}

func (p *Prompting) Connect() error {
	if !PromptingEnabled() {
		return nil
	}
	if p.requests != nil {
		return fmt.Errorf("cannot connect: listener is already registered")
	}
	l, err := listenerRegister()
	if err != nil {
		return fmt.Errorf("cannot register prompting listener: %v", err)
	}
	p.listener = l
	p.requests = promptrequests.New(p.notifyRequest)
	p.rules, _ = accessrules.New(p.notifyRule) // ignore error (failed to load existing rules)
	return nil
}

var (
	notifySupportAvailable = notify.SupportAvailable
	listenerRegister       = listener.Register
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
	if !PromptingEnabled() {
		return nil
	}
	p.tomb.Go(func() error {
		if p.listener == nil {
			logger.Noticef("listener is nil, exiting Prompting.Run() early")
			return fmt.Errorf("listener is nil, cannot run apparmor prompting")
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
				logger.Noticef("Got from kernel req chan: %v", req)
				if err := p.handleListenerReq(req); err != nil { // no use multithreading, since IsPathAllowed locks
					logger.Noticef("Error while handling request: %+v", err)
				}
			case <-p.tomb.Dying():
				logger.Noticef("Prompting tomb is dying, disconnecting")
				return p.disconnect()
			}
		}
	})
	return nil // TODO: finish this function (is it finished??)
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
	snap, app, err := common.LabelToSnapApp(req.Label())
	if err != nil {
		// the triggering process is not a snap, so treat apparmor label as both snap and app fields
	}

	iface := common.SelectSingleInterface(req.Interfaces())

	path := req.Path()

	permissions, err := common.PermissionMaskToPermissionsList(req.Permission().(notify.FilePermission))
	if err != nil {
		// some permission bits were unrecognized, ignore them
	}

	satisfiedPerms := make([]common.PermissionType, 0, len(permissions))
	for _, perm := range permissions {
		if yesNo, err := p.rules.IsPathAllowed(userID, snap, app, iface, path, perm); err == nil {
			if !yesNo {
				logger.Noticef("request denied by existing rule: %+v", req)
				// TODO: the response puts all original permissions in the
				// Deny field, do we want to differentiate the denied bits from
				// the others?
				return req.Reply(false)
			}
			satisfiedPerms = append(satisfiedPerms, perm)
		}
	}
	if len(satisfiedPerms) == len(permissions) {
		logger.Noticef("request allowed by existing rule: %+v", req)
		return req.Reply(true)
	}

	newRequest, merged := p.requests.AddOrMerge(userID, snap, app, iface, path, permissions, req)
	if merged {
		logger.Noticef("new request merged with identical existing request: %+v", newRequest)
		return nil
	}

	logger.Noticef("adding request to internal storage: %+v", newRequest)

	return nil
}

func (p *Prompting) Stop() error {
	p.tomb.Kill(nil)
	return p.tomb.Wait()
}

func (p *Prompting) GetRequests(userID uint32) ([]*promptrequests.PromptRequest, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	reqs := p.requests.Requests(userID)
	return reqs, nil
}

func (p *Prompting) GetRequest(userID uint32, requestID string) (*promptrequests.PromptRequest, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	req, err := p.requests.RequestWithID(userID, requestID)
	return req, err
}

type PromptReply struct {
	Outcome     common.OutcomeType      `json:"action"`
	Lifespan    common.LifespanType     `json:"lifespan"`
	Duration    string                  `json:"duration,omitempty"`
	PathPattern string                  `json:"path-pattern"`
	Permissions []common.PermissionType `json:"permissions"`
}

func (p *Prompting) PostRequest(userID uint32, requestID string, reply *PromptReply) ([]string, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	req, err := p.requests.Reply(userID, requestID, reply.Outcome)
	if err != nil {
		return nil, err
	}

	if reply.Lifespan == common.LifespanSingle {
		return make([]string, 0), nil
	}

	// Check that reply.PathPattern contains original requested path.
	// AppArmor is responsible for pre-vetting that all paths which appear
	// in requests from the kernel are allowed by the appropriate
	// interfaces, so we do not assert anything else particular about the
	// reply.PathPattern.
	// TODO: Should this be reconsidered?
	matches, err := common.PathPatternMatches(reply.PathPattern, req.Path)
	if err != nil {
		return nil, common.ErrInvalidPathPattern
	}
	if !matches {
		return nil, fmt.Errorf("path pattern in reply does not match originally requested path: '%s' does not match '%s'; skipping rule generation", reply.PathPattern, req.Path)
	}

	// Create new rule based on the reply.
	newRule, err := p.rules.CreateAccessRule(userID, req.Snap, req.App, req.Interface, reply.PathPattern, reply.Outcome, reply.Lifespan, reply.Duration, reply.Permissions)
	if err != nil {
		// XXX: should only occur if identical path to an existing rule with
		// overlapping permissions
		// TODO: extract conflicting permissions, retry CreateAccessRule with
		// conflicting permissions removed
		// TODO: what to do if new reply has different Outcome from previous
		// conflicting rule? Modify old rule to remove conflicting permissions,
		// then re-add new rule? This should probably be built into a version of
		// CreateAccessRule (CreateAccessRuleFromReply ?)
		return nil, err
	}

	// Apply new rule to outstanding prompt requests.
	satisfiedReqIDs, err := p.requests.HandleNewRule(userID, newRule.Snap, newRule.App, newRule.Interface, newRule.PathPattern, newRule.Outcome, newRule.Permissions)
	if err != nil {
		return nil, err
	}

	return satisfiedReqIDs, nil
}

type PostRulesCreateRuleContents struct {
	Snap        string                  `json:"snap"`
	App         string                  `json:"app"`
	Interface   string                  `json:"interface"`
	PathPattern string                  `json:"path-pattern"`
	Outcome     common.OutcomeType      `json:"outcome"`
	Lifespan    common.LifespanType     `json:"lifespan"`
	Duration    string                  `json:"duration,omitempty"`
	Permissions []common.PermissionType `json:"permissions"`
}

type PostRulesDeleteSelectors struct {
	Snap      string `json:"snap"`
	App       string `json:"app,omitempty"`
	Interface string `json:"interface,omitempty"`
}

type PostRulesRequestBody struct {
	Action          string                         `json:"action"`
	CreateRules     []*PostRulesCreateRuleContents `json:"rules,omitempty"`
	DeleteSelectors []*PostRulesDeleteSelectors    `json:"selectors,omitempty"`
}

type PostRuleModifyRuleContents struct {
	PathPattern string                  `json:"path-pattern,omitempty"`
	Outcome     common.OutcomeType      `json:"outcome,omitempty"`
	Lifespan    common.LifespanType     `json:"lifespan,omitempty"`
	Duration    string                  `json:"duration,omitempty"`
	Permissions []common.PermissionType `json:"permissions,omitempty"`
}

type PostRuleRequestBody struct {
	Action string                      `json:"action"`
	Rule   *PostRuleModifyRuleContents `json:"rule,omitempty"`
}

func (p *Prompting) GetRules(userID uint32, snap string, app string, iface string) ([]*accessrules.AccessRule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	// Daemon already checked that if app != "" or iface != "", then snap != ""
	if iface != "" {
		if app != "" {
			rules := p.rules.RulesForSnapAppInterface(userID, snap, app, iface)
			return rules, nil
		}
		rules := p.rules.RulesForSnapInterface(userID, snap, iface)
		return rules, nil
	}
	if app != "" {
		rules := p.rules.RulesForSnapApp(userID, snap, app)
		return rules, nil
	}
	if snap != "" {
		rules := p.rules.RulesForSnap(userID, snap)
		return rules, nil
	}
	rules := p.rules.Rules(userID)
	return rules, nil
}

func (p *Prompting) PostRulesCreate(userID uint32, rules []*PostRulesCreateRuleContents) ([]*accessrules.AccessRule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	createdRules := make([]*accessrules.AccessRule, 0, len(rules))
	errors := make([]error, 0)
	for _, ruleContents := range rules {
		snap := ruleContents.Snap
		app := ruleContents.App
		iface := ruleContents.Interface
		pathPattern := ruleContents.PathPattern
		outcome := ruleContents.Outcome
		lifespan := ruleContents.Lifespan
		duration := ruleContents.Duration
		permissions := ruleContents.Permissions
		newRule, err := p.rules.CreateAccessRule(userID, snap, app, iface, pathPattern, outcome, lifespan, duration, permissions)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		createdRules = append(createdRules, newRule)
		// Apply new rule to outstanding requests. If error occurs,
		// include it in the list of errors from creating rules.
		if _, err := p.requests.HandleNewRule(userID, newRule.Snap, newRule.App, newRule.Interface, newRule.PathPattern, newRule.Outcome, newRule.Permissions); err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		err := fmt.Errorf("")
		for i, e := range errors {
			err = fmt.Errorf("%w%+v: %v; ", err, rules[i], e)
		}
		return createdRules, err
	}
	return createdRules, nil
}

func (p *Prompting) PostRulesDelete(userID uint32, deleteSelectors []*PostRulesDeleteSelectors) ([]*accessrules.AccessRule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	deletedRules := make([]*accessrules.AccessRule, 0)
	for _, selector := range deleteSelectors {
		snap := selector.Snap
		app := selector.App
		iface := selector.Interface
		var rulesToDelete []*accessrules.AccessRule
		// Already checked that snap != ""
		if iface != "" {
			if app != "" {
				rulesToDelete = p.rules.RulesForSnapAppInterface(userID, snap, app, iface)
			} else {
				rulesToDelete = p.rules.RulesForSnapInterface(userID, snap, iface)
			}
		} else if app != "" {
			rulesToDelete = p.rules.RulesForSnapApp(userID, snap, app)
		} else {
			rulesToDelete = p.rules.RulesForSnap(userID, snap)
		}
		for _, rule := range rulesToDelete {
			deletedRule, err := p.rules.DeleteAccessRule(userID, rule.ID)
			if err != nil {
				continue
			}
			deletedRules = append(deletedRules, deletedRule)
		}
	}
	return deletedRules, nil
}

func (p *Prompting) GetRule(userID uint32, ruleID string) (*accessrules.AccessRule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	rule, err := p.rules.RuleWithID(userID, ruleID)
	return rule, err
}

func (p *Prompting) PostRuleModify(userID uint32, ruleID string, contents *PostRuleModifyRuleContents) (*accessrules.AccessRule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	pathPattern := contents.PathPattern
	outcome := contents.Outcome
	lifespan := contents.Lifespan
	duration := contents.Duration
	permissions := contents.Permissions
	rule, err := p.rules.ModifyAccessRule(userID, ruleID, pathPattern, outcome, lifespan, duration, permissions)
	return rule, err
}

func (p *Prompting) PostRuleDelete(userID uint32, ruleID string) (*accessrules.AccessRule, error) {
	if !PromptingEnabled() {
		return nil, fmt.Errorf("AppArmor Prompting is not enabled")
	}
	rule, err := p.rules.DeleteAccessRule(userID, ruleID)
	return rule, err
}
