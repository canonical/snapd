package promptrequests

import (
	"errors"
	"reflect"
	"sync"

	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
)

var ErrConflictingRequestID = errors.New("a prompt request with the same ID already exists")
var ErrRequestIDNotFound = errors.New("no request with the given ID found for the given user")
var ErrUserNotFound = errors.New("no prompt requests found for the given user")

type PromptRequest struct {
	ID           string                  `json:"id"`
	Timestamp    string                  `json:"timestamp"`
	Snap         string                  `json:"snap"`
	App          string                  `json:"app"`
	Interface    string                  `json:"interface"`
	Path         string                  `json:"path"`
	Permissions  []common.PermissionType `json:"permissions"`
	listenerReqs []*listener.Request     `json:"-"`
}

type userRequestDB struct {
	ByID map[string]*PromptRequest
}

type RequestDB struct {
	PerUser map[uint32]*userRequestDB
	mutex   sync.Mutex
	// Function to issue a notice for a change in a request
	notifyRequest func(userID uint32, requestID string, options *state.AddNoticeOptions) error
}

func New(notifyRequest func(userID uint32, requestID string, options *state.AddNoticeOptions) error) *RequestDB {
	return &RequestDB{
		PerUser:       make(map[uint32]*userRequestDB),
		notifyRequest: notifyRequest,
	}
}

// Creates, adds, and returns a new prompt request from the given parameters.
//
// If the parameters exactly match an existing request, merge it with that
// existing request instead, and do not add a new request. If a new request was
// added, returns the new request and false, indicating the request was not
// merged. If it was merged with an identical existing request, returns the
// existing request and true.
func (rdb *RequestDB) AddOrMerge(user uint32, snap string, app string, iface string, path string, permissions []common.PermissionType, listenerReq *listener.Request) (*PromptRequest, bool) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	userEntry, exists := rdb.PerUser[user]
	if !exists {
		rdb.PerUser[user] = &userRequestDB{
			ByID: make(map[string]*PromptRequest),
		}
		userEntry = rdb.PerUser[user]
	}

	// Search for an identical existing request, merge if found
	for _, req := range userEntry.ByID {
		if req.Snap == snap && req.App == app && req.Path == path && reflect.DeepEqual(req.Permissions, permissions) {
			req.listenerReqs = append(req.listenerReqs, listenerReq)
			return req, true
		}
	}

	id, timestamp := common.NewIDAndTimestamp()
	req := &PromptRequest{
		ID:           id,
		Timestamp:    timestamp,
		Snap:         snap,
		App:          app,
		Interface:    iface,
		Path:         path,
		Permissions:  permissions, // TODO: copy permissions list?
		listenerReqs: []*listener.Request{listenerReq},
	}
	userEntry.ByID[id] = req
	rdb.notifyRequest(user, id, nil)
	return req, false
}

func (rdb *RequestDB) Requests(user uint32) []*PromptRequest {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	userEntry, exists := rdb.PerUser[user]
	if !exists {
		return make([]*PromptRequest, 0)
	}
	requests := make([]*PromptRequest, 0, len(userEntry.ByID))
	for _, req := range userEntry.ByID {
		requests = append(requests, req)
	}
	return requests
}

func (rdb *RequestDB) RequestWithID(user uint32, id string) (*PromptRequest, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	userEntry, exists := rdb.PerUser[user]
	if !exists {
		return nil, ErrUserNotFound
	}
	req, exists := userEntry.ByID[id]
	if !exists {
		return nil, ErrRequestIDNotFound
	}
	return req, nil
}

// Reply resolves the request with the given ID using the given outcome.
func (rdb *RequestDB) Reply(user uint32, id string, outcome common.OutcomeType) (*PromptRequest, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	userEntry, exists := rdb.PerUser[user]
	if !exists || len(userEntry.ByID) == 0 {
		return nil, ErrUserNotFound
	}
	req, exists := userEntry.ByID[id]
	if !exists {
		return nil, ErrRequestIDNotFound
	}
	var outcomeBool bool
	switch outcome {
	case common.OutcomeAllow:
		outcomeBool = true
	case common.OutcomeDeny:
		outcomeBool = false
	default:
		return nil, common.ErrInvalidOutcome
	}
	for _, listenerReq := range req.listenerReqs {
		if err := sendReply(listenerReq, outcomeBool); err != nil {
			return nil, err
		}
	}
	delete(userEntry.ByID, id)
	rdb.notifyRequest(user, id, nil)
	return req, nil
}

var sendReply = func(listenerReq *listener.Request, reply interface{}) error {
	return listenerReq.Reply(reply)
}

// If any existing requests are satisfied by the given rule, send the decision
// along their respective channels, and return their IDs.
func (rdb *RequestDB) HandleNewRule(user uint32, snap string, app string, iface string, pathPattern string, outcome common.OutcomeType, permissions []common.PermissionType) ([]string, error) {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	var outcomeBool bool
	switch outcome {
	case common.OutcomeAllow:
		outcomeBool = true
	case common.OutcomeDeny:
		outcomeBool = false
	default:
		return nil, common.ErrInvalidOutcome
	}
	var satisfiedReqIDs []string
	userEntry, exists := rdb.PerUser[user]
	if !exists {
		return satisfiedReqIDs, nil
	}
	for id, req := range userEntry.ByID {
		if !(req.Snap == snap && req.App == app && req.Interface == iface) {
			continue
		}
		matched, err := common.PathPatternMatches(pathPattern, req.Path)
		if err != nil {
			// Only possible error is ErrBadPattern
			return nil, err
		}
		if !matched {
			continue
		}
		remainingPermissions := req.Permissions
		for _, perm := range permissions {
			remainingPermissions, _ = common.RemovePermissionFromList(remainingPermissions, perm)
		}
		if len(remainingPermissions) > 0 {
			// If we don't satisfy all permissions with the new rule,
			// leave it up to the UI to prompt for all at once.
			continue
		}
		// all permissions of request satisfied
		for _, listenerReq := range req.listenerReqs {
			sendReply(listenerReq, outcomeBool)
		}
		delete(userEntry.ByID, id)
		satisfiedReqIDs = append(satisfiedReqIDs, id)
		rdb.notifyRequest(user, id, nil)
	}
	return satisfiedReqIDs, nil
}
