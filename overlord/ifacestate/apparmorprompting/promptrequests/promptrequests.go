package promptrequests

import (
	"errors"
	"sync"

	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
)

var ErrConflictingRequestID = errors.New("a prompt request with the same ID already exists")
var ErrRequestIDNotFound = errors.New("no request with the given ID found for the given user")
var ErrUserNotFound = errors.New("no prompt requests found for the given user")

type PromptRequest struct {
	ID          string                  `json:"id"`
	Timestamp   string                  `json:"timestamp"`
	Snap        string                  `json:"snap"`
	App         string                  `json:"app"`
	Path        string                  `json:"path"`
	Permissions []common.PermissionType `json:"permissions"`
	listenerReq *listener.Request       `json:"-"`
}

type userRequestDB struct {
	ByID map[string]*PromptRequest
}

type RequestDB struct {
	PerUser map[uint32]*userRequestDB
	mutex   sync.Mutex
}

func New() *RequestDB {
	return &RequestDB{
		PerUser: make(map[uint32]*userRequestDB),
	}
}

// Creates, adds, and returns a new prompt request from the given parameters.
func (rdb *RequestDB) Add(user uint32, snap string, app string, path string, permissions []common.PermissionType, listenerReq *listener.Request) *PromptRequest {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	userEntry, exists := rdb.PerUser[user]
	if !exists {
		rdb.PerUser[user] = &userRequestDB{
			ByID: make(map[string]*PromptRequest),
		}
		userEntry = rdb.PerUser[user]
	}
	id, timestamp := common.NewIDAndTimestamp()
	req := &PromptRequest{
		ID:          id,
		Timestamp:   timestamp,
		Snap:        snap,
		App:         app,
		Path:        path,
		Permissions: permissions, // TODO: copy permissions list?
		listenerReq: listenerReq,
	}
	userEntry.ByID[id] = req
	return req
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

func (rdb *RequestDB) Reply(user uint32, id string, outcome common.OutcomeType) error {
	rdb.mutex.Lock()
	defer rdb.mutex.Unlock()
	userEntry, exists := rdb.PerUser[user]
	if !exists || len(userEntry.ByID) == 0 {
		return ErrUserNotFound
	}
	req, exists := userEntry.ByID[id]
	if !exists {
		return ErrRequestIDNotFound
	}
	var outcomeBool bool
	switch outcome {
	case common.OutcomeAllow:
		outcomeBool = true
	case common.OutcomeDeny:
		outcomeBool = false
	default:
		return common.ErrInvalidOutcome
	}
	if err := sendReply(req.listenerReq, outcomeBool); err != nil {
		return err
	}
	delete(userEntry.ByID, id)
	return nil
}

var sendReply = func(listenerReq *listener.Request, reply interface{}) error {
	return listenerReq.Reply(reply)
}

// If any existing requests are satisfied by the given rule, send the decision
// along their respective channels, and return their IDs.
func (rdb *RequestDB) HandleNewRule(user uint32, snap string, app string, pathPattern string, outcome common.OutcomeType, permissions []common.PermissionType) ([]string, error) {
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
		if !(snap == req.Snap && app == req.App) {
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
		sendReply(req.listenerReq, outcomeBool)
		delete(userEntry.ByID, id)
		satisfiedReqIDs = append(satisfiedReqIDs, id)
	}
	return satisfiedReqIDs, nil
}
