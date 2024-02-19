// Copyright (c) 2023 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var (
	noticesCmd = &Command{
		Path:       "/v2/notices",
		GET:        getNotices,
		ReadAccess: openAccess{},
	}

	noticeCmd = &Command{
		Path:       "/v2/notices/{id}",
		GET:        getNotice,
		ReadAccess: openAccess{},
	}
)

func getNotices(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()

	requestUID, err := uidFromRequest(r)
	if err != nil {
		return Forbidden("cannot determine UID of request, so cannot retrieve notices")
	}

	// By default, return notices with the request UID and public notices.
	userID := &requestUID

	if len(query["user-id"]) > 0 {
		if requestUID != 0 {
			return Forbidden(`only admins may use the "user-id" filter`)
		}
		userID, err = sanitizeUserIDFilter(query["user-id"])
		if err != nil {
			return BadRequest(`invalid "user-id" filter: %v`, err)
		}
	}

	if len(query["select"]) > 0 {
		if requestUID != 0 {
			return Forbidden(`only admins may use the "select" filter`)
		}
		if len(query["user-id"]) > 0 {
			return BadRequest(`cannot use both "select" and "user-id" parameters`)
		}
		if query.Get("select") != "all" {
			return BadRequest(`invalid "select" filter: must be "all"`)
		}
		// Clear the userID filter so all notices will be returned.
		userID = nil
	}

	types, err := sanitizeTypesFilter(query["types"])
	if err != nil {
		// Caller did provide a types filter, but they're all invalid notice types.
		// Return no notices, rather than the default of all notices.
		return SyncResponse([]*noticeInfo{})
	}

	keys := strutil.MultiCommaSeparatedList(query["keys"])

	after, err := parseOptionalTime(query.Get("after"))
	if err != nil {
		return BadRequest(`invalid "after" timestamp: %v`, err)
	}

	filter := &state.NoticeFilter{
		UserID: userID,
		Types:  types,
		Keys:   keys,
		After:  after,
	}

	timeout, err := parseOptionalDuration(query.Get("timeout"))
	if err != nil {
		return BadRequest("invalid timeout: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var notices []*state.Notice

	if timeout != 0 {
		// Wait up to timeout for notices matching given filter to occur
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		notices, err = st.WaitNotices(ctx, filter)
		if errors.Is(err, context.Canceled) {
			return BadRequest("request canceled")
		}
		// DeadlineExceeded will occur if timeout elapses; in that case return
		// an empty list of notices, not an error.
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return InternalError("cannot wait for notices: %s", err)
		}
	} else {
		// No timeout given, fetch currently-available notices
		notices = st.Notices(filter)
	}

	noticeInfos := make([]*noticeInfo, 0, len(notices))
	for _, notice := range notices {
		noticeInfos = append(noticeInfos, notice2noticeInfo(notice))
	}
	return SyncResponse(noticeInfos)
}

// Get the UID of the request. If the UID is not known, return an error.
func uidFromRequest(r *http.Request) (uint32, error) {
	cred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return 0, fmt.Errorf("could not parse request UID")
	}
	return cred.Uid, nil
}

// Construct the user IDs filter which will be passed to state.Notices.
// Must only be called if the query user ID argument is set.
func sanitizeUserIDFilter(queryUserID []string) (*uint32, error) {
	userIDStrs := strutil.MultiCommaSeparatedList(queryUserID)
	if len(userIDStrs) != 1 {
		return nil, fmt.Errorf(`must only include one "user-id"`)
	}
	userIDInt, err := strconv.ParseInt(userIDStrs[0], 10, 64)
	if err != nil {
		return nil, err
	}
	if userIDInt < 0 || userIDInt > math.MaxUint32 {
		return nil, fmt.Errorf("user ID is not a valid uint32: %d", userIDInt)
	}
	userID := uint32(userIDInt)
	return &userID, nil
}

// Construct the types filter which will be passed to state.Notices.
func sanitizeTypesFilter(queryTypes []string) ([]state.NoticeType, error) {
	typeStrs := strutil.MultiCommaSeparatedList(queryTypes)
	types := make([]state.NoticeType, 0, len(typeStrs))
	for _, typeStr := range typeStrs {
		noticeType := state.NoticeType(typeStr)
		if !noticeType.Valid() {
			// Ignore invalid notice types (so requests from newer clients
			// with unknown types succeed).
			continue
		}
		types = append(types, noticeType)
	}
	if len(types) == 0 && len(typeStrs) > 0 {
		return nil, errors.New("all requested notice types invalid")
	}
	return types, nil
}

func getNotice(c *Command, r *http.Request, user *auth.UserState) Response {
	requestUID, err := uidFromRequest(r)
	if err != nil {
		return Forbidden("cannot determine UID of request, so cannot retrieve notice")
	}
	noticeID := muxVars(r)["id"]
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	notice := st.Notice(noticeID)
	if notice == nil {
		return NotFound("cannot find notice with id %q", noticeID)
	}
	if !noticeViewableByUser(notice, requestUID) {
		return Forbidden("not allowed to access notice with id %q", noticeID)
	}
	return SyncResponse(notice2noticeInfo(notice))
}

// Only the user associated with the given notice, as well as the root user,
// may view the notice. Snapd does also have authenticated admins which are not
// root, but at the moment we do not have a level of notice visibility which
// grants access to those admins, as well as root and the notice's user.
func noticeViewableByUser(notice *state.Notice, requestUID uint32) bool {
	userID, isSet := notice.UserID()
	if !isSet {
		return true
	}
	// Root is allowed to view any notice.
	if requestUID == 0 {
		return true
	}
	return requestUID == userID
}

type noticeInfo struct {
	ID            string            `json:"id"`
	UserID        *uint32           `json:"user-id"`
	Type          string            `json:"type"`
	Key           string            `json:"key"`
	FirstOccurred time.Time         `json:"first-occurred"`
	LastOccurred  time.Time         `json:"last-occurred"`
	LastRepeated  time.Time         `json:"last-repeated"`
	Occurrences   int               `json:"occurrences"`
	LastData      map[string]string `json:"last-data,omitempty"`
	RepeatAfter   string            `json:"repeat-after,omitempty"`
	ExpireAfter   string            `json:"expire-after,omitempty"`
}

func notice2noticeInfo(n *state.Notice) *noticeInfo {
	info := noticeInfo{
		ID:            n.ID,
		Type:          string(n.Type),
		Key:           n.Key,
		FirstOccurred: n.FirstOccurred,
		LastOccurred:  n.LastOccurred,
		LastRepeated:  n.LastRepeated,
		Occurrences:   n.Occurrences,
		LastData:      n.LastData,
	}
	if n.RepeatAfter != 0 {
		info.RepeatAfter = n.RepeatAfter.String()
	}
	if n.ExpireAfter != 0 {
		info.ExpireAfter = n.ExpireAfter.String()
	}
	if userID, isSet := n.UserID(); isSet {
		info.UserID = &userID
	}
	return &info
}
