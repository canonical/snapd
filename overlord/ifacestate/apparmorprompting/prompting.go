package apparmorprompting

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/accessrules"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/promptrequests"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
)

var userOverride int = 1234

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
}

func New() Interface {
	p := &Prompting{}
	return p
}

func (p *Prompting) Connect() error {
	if !notify.SupportAvailable() {
		return nil
	}
	l, err := listener.Register()
	if err != nil {
		return err
	}
	p.listener = l
	p.requests = promptrequests.New()
	p.rules, _ = accessrules.New() // ignore error (failed to load existing rules)
	return nil
}

func (p *Prompting) disconnect() error {
	if p.listener == nil {
		return nil
	}
	if err := p.listener.Close(); err != nil {
		return err
	}
	return nil
}

func (p *Prompting) handleListenerReq(req *listener.Request) error {
	// user := int(req.SubjectUid) // TODO: undo this! This is just for debugging
	user := userOverride // TODO: undo this! This is just for debugging
	snap, app, err := common.LabelToSnapApp(req.Label)
	if err != nil {
		// the triggering process is not a snap, so treat apparmor label as both snap and app fields
	}

	path := req.Path

	permissions, err := common.PermissionMaskToPermissionsList(req.Permission.(notify.FilePermission))
	if err != nil {
		// some permission bits were unrecognized, ignore them
	}

	satisfiedPerms := make([]common.PermissionType, 0, len(permissions))
	for _, perm := range permissions {
		if yesNo, err := p.rules.IsPathAllowed(user, snap, app, path, perm); err == nil {
			if !yesNo {
				req.YesNo <- false
				// TODO: the response puts all original permissions in the
				// Deny field, do we want to differentiate the denied bits from
				// the others? Also, do we want to use the waiting listener
				// thread to reply, or construct and send the reply directly?
				return nil
			}
			satisfiedPerms = append(satisfiedPerms, perm)
		}
	}
	if len(satisfiedPerms) == len(permissions) {
		req.YesNo <- true
		return nil
	}

	p.requests.Add(user, snap, app, path, permissions, req.YesNo)
	logger.Noticef("adding request to internal storage: user: %v, snap: %v, app: %v, path: %v, permissions: %v", user, snap, app, path, permissions)
	// TODO: notify any listeners to the requests API using p.tomb.Go()
	return nil
}

func (p *Prompting) Run() error {
	p.tomb.Go(func() error {
		if p.listener == nil {
			logger.Noticef("listener is nil, exiting Prompting.Run() early")
			return nil
		}
		p.tomb.Go(func() error {
			p.listener.Run(&p.tomb)
			logger.Noticef("started listener")
			return nil
		})

		logger.Noticef("ready for prompts")
		for {
			logger.Debugf("waiting prompt loop")
			select {
			case req := <-p.listener.R:
				logger.Noticef("Got from kernel req chan: %v", req)
				if err := p.handleListenerReq(req); err != nil { // no use multithreading, since IsPathAllowed locks
					logger.Noticef("Error while handling request: %v", err)
				}
			case err := <-p.listener.E:
				logger.Noticef("Got from kernel error chan: %v", err)
				return err
			case <-p.tomb.Dying():
				logger.Noticef("Prompting tomb is dying, disconnecting")
				return p.disconnect()
			}
		}
	})
	return nil // TODO: finish this function (is it finished??)
}

func (p *Prompting) Stop() error {
	p.tomb.Kill(nil)
	err := p.tomb.Wait()
	p.listener = nil
	p.requests = nil
	p.rules = nil
	return err
}

func (p *Prompting) GetRequests(userId int) ([]*promptrequests.PromptRequest, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	reqs := p.requests.Requests(userId)
	return reqs, nil
}

func (p *Prompting) GetRequest(userId int, requestId string) (*promptrequests.PromptRequest, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	req, err := p.requests.RequestWithId(userId, requestId)
	return req, err
}

type PromptReply struct {
	Outcome     common.OutcomeType      `json:"action"`
	Lifespan    common.LifespanType     `json:"lifespan"`
	Duration    int                     `json:"duration,omitempty"`
	PathPattern string                  `json:"path-pattern"`
	Permissions []common.PermissionType `json:"permissions"`
}

func (p *Prompting) PostRequest(userId int, requestId string, reply *PromptReply) ([]string, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	req, err := p.requests.Reply(userId, requestId, reply.Outcome)
	if err != nil {
		return nil, err
	}

	// Create new rule based on the reply.
	newRule, err := p.rules.CreateAccessRule(userId, req.Snap, req.App, reply.PathPattern, reply.Outcome, reply.Lifespan, reply.Duration, reply.Permissions)
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
	satisfiedReqIds, err := p.requests.HandleNewRule(userId, newRule.Snap, newRule.App, newRule.PathPattern, newRule.Outcome, newRule.Permissions)
	if err != nil {
		return nil, err
	}

	return satisfiedReqIds, nil
}

type PostRulesCreateRuleContents struct {
	Snap        string                  `json:"snap"`
	App         string                  `json:"app"`
	PathPattern string                  `json:"path-pattern"`
	Outcome     common.OutcomeType      `json:"outcome"`
	Lifespan    common.LifespanType     `json:"lifespan"`
	Duration    int                     `json:"duration,omitempty"`
	Permissions []common.PermissionType `json:"permissions"`
}

type PostRulesDeleteSelectors struct {
	Snap string `json:"snap"`
	App  string `json:"app,omitempty"`
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
	Duration    int                     `json:"duration,omitempty"`
	Permissions []common.PermissionType `json:"permissions,omitempty"`
}

type PostRuleRequestBody struct {
	Action string                      `json:"action"`
	Rule   *PostRuleModifyRuleContents `json:"rule,omitempty"`
}

func (p *Prompting) GetRules(userId int, snap string, app string) ([]*accessrules.AccessRule, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	// Daemon already checked that if app != "", then snap != ""
	if app != "" {
		rules := p.rules.RulesForSnapApp(userId, snap, app)
		return rules, nil
	}
	if snap != "" {
		rules := p.rules.RulesForSnap(userId, snap)
		return rules, nil
	}
	rules := p.rules.Rules(userId)
	return rules, nil
}

func (p *Prompting) PostRulesCreate(userId int, rules []*PostRulesCreateRuleContents) ([]*accessrules.AccessRule, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	createdRules := make([]*accessrules.AccessRule, 0, len(rules))
	errors := make([]error, 0)
	for _, ruleContents := range rules {
		snap := ruleContents.Snap
		app := ruleContents.App
		pathPattern := ruleContents.PathPattern
		outcome := ruleContents.Outcome
		lifespan := ruleContents.Lifespan
		duration := ruleContents.Duration
		permissions := ruleContents.Permissions
		newRule, err := p.rules.CreateAccessRule(userId, snap, app, pathPattern, outcome, lifespan, duration, permissions)
		if err != nil {
			errors = append(errors, err)
		} else {
			createdRules = append(createdRules, newRule)
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

func (p *Prompting) PostRulesDelete(userId int, deleteSelectors []*PostRulesDeleteSelectors) ([]*accessrules.AccessRule, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	deletedRules := make([]*accessrules.AccessRule, 0)
	for _, selector := range deleteSelectors {
		snap := selector.Snap
		app := selector.App
		var rulesToDelete []*accessrules.AccessRule
		// Already checked that snap != ""
		if app != "" {
			rulesToDelete = p.rules.RulesForSnapApp(userId, snap, app)
		} else {
			rulesToDelete = p.rules.RulesForSnap(userId, snap)
		}
		for _, rule := range rulesToDelete {
			deletedRule, err := p.rules.DeleteAccessRule(userId, rule.Id)
			if err != nil {
				continue
			}
			deletedRules = append(deletedRules, deletedRule)
		}
	}
	return deletedRules, nil
}

func (p *Prompting) GetRule(userId int, ruleId string) (*accessrules.AccessRule, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	rule, err := p.rules.RuleWithId(userId, ruleId)
	return rule, err
}

func (p *Prompting) PostRuleModify(userId int, ruleId string, contents *PostRuleModifyRuleContents) (*accessrules.AccessRule, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	pathPattern := contents.PathPattern
	outcome := contents.Outcome
	lifespan := contents.Lifespan
	duration := contents.Duration
	permissions := contents.Permissions
	rule, err := p.rules.ModifyAccessRule(userId, ruleId, pathPattern, outcome, lifespan, duration, permissions)
	return rule, err
}

func (p *Prompting) PostRuleDelete(userId int, ruleId string) (*accessrules.AccessRule, error) {
	userId = userOverride // TODO: undo this! This is just for debugging
	rule, err := p.rules.DeleteAccessRule(userId, ruleId)
	return rule, err
}
