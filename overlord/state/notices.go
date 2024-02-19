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

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/snapcore/snapd/logger"
)

const (
	// defaultNoticeExpireAfter is the default expiry time for notices.
	defaultNoticeExpireAfter = 7 * 24 * time.Hour
)

// Notice represents an aggregated notice. The combination of type and key is unique.
type Notice struct {
	// Server-generated unique ID for this notice (a surrogate key).
	//
	// Currently this is a monotonically increasing number, but that may well
	// change in future. If your code relies on it being a number, it will break.
	ID string

	// The UID of the user who may view this notice (often its creator).
	// A nil userID means that the notice is public (viewable by all users).
	userID *uint32

	// The notice type represents a group of notices originating from a common
	// source. For example, notices which provide human-readable warnings have
	// the type "warning".
	Type NoticeType

	// The notice key is a string that differentiates notices of this type.
	// Notices recorded with the type and key of an existing notice count as
	// an occurrence of that notice.
	//
	// This is limited to a maximum of MaxNoticeKeyLength bytes when added
	// (it's an error to add a notice with a longer key).
	Key string

	// The first time one of these notices (type and key combination) occurs.
	FirstOccurred time.Time

	// The last time one of these notices occurred. This is updated every time
	// one of these notices occurs.
	LastOccurred time.Time

	// The time this notice was last "repeated". This is set when one of these
	// notices first occurs, and updated when it reoccurs at least
	// repeatAfter after the previous lastRepeated time.
	//
	// Notices and WaitNotices return notices ordered by lastRepeated time, so
	// repeated notices will appear at the end of the returned list.
	LastRepeated time.Time

	// The number of times one of these notices has occurred.
	Occurrences int

	// Additional data captured from the last occurrence of one of these notices.
	LastData map[string]string

	// How long after one of these was last repeated should we allow it to repeat.
	RepeatAfter time.Duration

	// How long since one of these last occurred until we should drop the notice.
	//
	// The repeatAfter duration must be less than this, because the notice
	// won't be tracked after it expires.
	ExpireAfter time.Duration

	// Data used by AddNoticeOptions.RepeatCheck to update options
	// dynamically based on existing state. It is kept for stateful tracking
	// throughout the notice's lifetime.
	repeatCheckData *json.RawMessage
}

func (n *Notice) String() string {
	userIDStr := "public"
	if n.userID != nil {
		userIDStr = strconv.FormatUint(uint64(*n.userID), 10)
	}
	return fmt.Sprintf("Notice %s (%s:%s:%s)", n.ID, userIDStr, n.Type, n.Key)
}

// UserID returns the value of the notice's user ID and whether it is set.
// If it is nil, then the returned userID is 0, and isSet is false.
func (n *Notice) UserID() (userID uint32, isSet bool) {
	// Importantly, doesn't expose the address of the notice's user ID, so the
	// caller cannot mutate the value.
	return flattenUserID(n.userID)
}

func flattenUserID(userID *uint32) (uid uint32, isSet bool) {
	if userID == nil {
		return 0, false
	}
	return *userID, true
}

// expired reports whether this notice has expired (relative to the given "now").
func (n *Notice) expired(now time.Time) bool {
	return n.LastOccurred.Add(n.ExpireAfter).Before(now)
}

// GetRepeatCheckData unmarshals the previously stored repeat check data (from
// AddNoticeOptions or RepeatCheck) into the value parameter
func (n *Notice) GetRepeatCheckValue(value interface{}) error {
	if n.repeatCheckData == nil {
		return ErrNoState
	}
	if err := json.Unmarshal(*n.repeatCheckData, value); err != nil {
		return fmt.Errorf("internal error: could not unmarshal RepeatCheckData: %v", err)
	}
	return nil
}

func (n *Notice) setRepeatCheckValue(v interface{}) {
	if v == nil {
		// keep data
		return
	}
	serialized, err := json.Marshal(v)
	if err != nil {
		logger.Panicf("internal error: could not marshal RepeatCheckData value: %v", err)
	}
	rawJson := json.RawMessage(serialized)
	n.repeatCheckData = &rawJson
}

type marshalledNotice struct {
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

	RepeatCheckData *json.RawMessage `json:"repeat-check-data,omitempty"`
}

func (n *Notice) MarshalJSON() ([]byte, error) {
	jn := marshalledNotice{
		ID:              n.ID,
		UserID:          n.userID,
		Type:            string(n.Type),
		Key:             n.Key,
		FirstOccurred:   n.FirstOccurred,
		LastOccurred:    n.LastOccurred,
		LastRepeated:    n.LastRepeated,
		Occurrences:     n.Occurrences,
		LastData:        n.LastData,
		RepeatCheckData: n.repeatCheckData,
	}
	if n.RepeatAfter != 0 {
		jn.RepeatAfter = n.RepeatAfter.String()
	}
	if n.ExpireAfter != 0 {
		jn.ExpireAfter = n.ExpireAfter.String()
	}
	return json.Marshal(jn)
}

func (n *Notice) UnmarshalJSON(data []byte) error {
	var jn marshalledNotice
	err := json.Unmarshal(data, &jn)
	if err != nil {
		return err
	}
	n.ID = jn.ID
	n.userID = jn.UserID
	n.Type = NoticeType(jn.Type)
	n.Key = jn.Key
	n.FirstOccurred = jn.FirstOccurred
	n.LastOccurred = jn.LastOccurred
	n.LastRepeated = jn.LastRepeated
	n.Occurrences = jn.Occurrences
	n.LastData = jn.LastData
	n.repeatCheckData = jn.RepeatCheckData
	if jn.RepeatAfter != "" {
		n.RepeatAfter, err = time.ParseDuration(jn.RepeatAfter)
		if err != nil {
			return fmt.Errorf("invalid repeat-after duration: %w", err)
		}
	}
	if jn.ExpireAfter != "" {
		n.ExpireAfter, err = time.ParseDuration(jn.ExpireAfter)
		if err != nil {
			return fmt.Errorf("invalid expire-after duration: %w", err)
		}
	}
	return nil
}

type NoticeType string

const (
	// Recorded whenever a change is updated: when it is first spawned or its
	// status was updated. The key for change-update notices is the change ID.
	ChangeUpdateNotice NoticeType = "change-update"

	// Warnings are a subset of notices where the key is a human-readable
	// warning message.
	WarningNotice NoticeType = "warning"
)

func (t NoticeType) Valid() bool {
	switch t {
	case ChangeUpdateNotice, WarningNotice:
		return true
	}
	return false
}

// AddNoticeOptions holds optional parameters for an AddNotice call.
type AddNoticeOptions struct {
	// Data is the optional key-value data for this occurrence.
	Data map[string]string

	// RepeatAfter defines how long after this notice was last repeated we
	// should allow it to repeat. Zero means always repeat.
	RepeatAfter time.Duration

	// Time, if set, overrides time.Now() as the notice occurrence time.
	Time time.Time

	// RepeatCheckData is saved for stateful tracking throughout the notice's lifetime.
	//
	// NOTES:
	//	- RepeatCheckData must be JSON marshallable.
	//	- If RepeatCheck is set, its returned data takes priority.
	//	- Notice repeat check data is only updated if the notice is repeated.
	RepeatCheckData interface{}

	// RepeatCheck, if set, returns whether this notice is forced to not be repeated
	// and also returns the new repeat check data.
	//
	// NOTE:
	//	- Current state can be accessed through oldNotice.GetRepeatCheckValue().
	RepeatCheck func(oldNotice *Notice, newNoticeOpts *AddNoticeOptions) (repeatOk bool, newRepeatCheckData interface{}, err error)
}

func (options *AddNoticeOptions) runRepeatCheck(notice *Notice) (repeatOk bool, newRepeatCheckData interface{}, err error) {
	if options.RepeatCheck == nil {
		// Fallback to options.RepeatCheckData
		return true, options.RepeatCheckData, nil
	}
	return options.RepeatCheck(notice, options)
}

// AddNotice records an occurrence of a notice with the specified type and key
// and options.
func (s *State) AddNotice(userID *uint32, noticeType NoticeType, key string, options *AddNoticeOptions) (string, error) {
	if options == nil {
		options = &AddNoticeOptions{}
	}
	err := validateNotice(noticeType, key, options)
	if err != nil {
		return "", err
	}

	s.writing()

	now := options.Time
	if now.IsZero() {
		now = time.Now()
	}
	newOrRepeated := false
	uid, hasUserID := flattenUserID(userID)
	uniqueKey := noticeKey{hasUserID, uid, noticeType, key}
	notice, ok := s.notices[uniqueKey]
	if !ok {
		// First occurrence of this notice userID+type+key
		s.lastNoticeId++
		notice = &Notice{
			ID:            strconv.Itoa(s.lastNoticeId),
			userID:        userID,
			Type:          noticeType,
			Key:           key,
			FirstOccurred: now,
			LastRepeated:  now,
			ExpireAfter:   defaultNoticeExpireAfter,
			Occurrences:   1,
		}
		notice.setRepeatCheckValue(options.RepeatCheckData)
		s.notices[uniqueKey] = notice
		newOrRepeated = true
	} else {
		if options.RepeatAfter == 0 || now.After(notice.LastRepeated.Add(options.RepeatAfter)) {
			// Update last repeated time if repeat-after time has elapsed (or is zero)
			// Consult RepeatCheck as well
			repeatCheckOk, newRepeatCheckData, err := options.runRepeatCheck(notice)
			if err != nil {
				return "", err
			}
			if repeatCheckOk {
				notice.setRepeatCheckValue(newRepeatCheckData)
				notice.LastRepeated = now
				newOrRepeated = true
			}
		}
		// Additional occurrence, update existing notice
		notice.Occurrences++
	}
	notice.LastOccurred = now
	notice.LastData = options.Data
	notice.RepeatAfter = options.RepeatAfter

	if newOrRepeated {
		s.noticeCond.Broadcast()
	}

	return notice.ID, nil
}

func validateNotice(noticeType NoticeType, key string, options *AddNoticeOptions) error {
	if !noticeType.Valid() {
		return fmt.Errorf("internal error: attempted to add notice with invalid type %q", noticeType)
	}
	if key == "" {
		return fmt.Errorf("internal error: attempted to add %s notice with invalid key %q", noticeType, key)
	}
	return nil
}

type noticeKey struct {
	hasUserID  bool
	userID     uint32
	noticeType NoticeType
	key        string
}

// NoticeFilter allows filtering notices by various fields.
type NoticeFilter struct {
	// UserID, if set, includes only notices that have this user ID or are public.
	UserID *uint32

	// Types, if not empty, includes only notices whose type is one of these.
	Types []NoticeType

	// Keys, if not empty, includes only notices whose key is one of these.
	Keys []string

	// After, if set, includes only notices that were last repeated after this time.
	After time.Time
}

// matches reports whether the notice n matches this filter
func (f *NoticeFilter) matches(n *Notice) bool {
	if f == nil {
		return true
	}
	if f.UserID != nil && !(n.userID == nil || *f.UserID == *n.userID) {
		return false
	}
	// Can't use strutil.ListContains as Types is []NoticeType, not []string
	if len(f.Types) > 0 && !sliceContains(f.Types, n.Type) {
		return false
	}
	if len(f.Keys) > 0 && !sliceContains(f.Keys, n.Key) {
		return false
	}
	if !f.After.IsZero() && !n.LastRepeated.After(f.After) {
		return false
	}
	return true
}

func sliceContains[T comparable](haystack []T, needle T) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// Notices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time.
func (s *State) Notices(filter *NoticeFilter) []*Notice {
	s.reading()

	notices := s.flattenNotices(filter)
	sort.Slice(notices, func(i, j int) bool {
		return notices[i].LastRepeated.Before(notices[j].LastRepeated)
	})
	return notices
}

// Notice returns a single notice by ID, or nil if not found.
func (s *State) Notice(id string) *Notice {
	s.reading()

	// Could use another map for lookup, but the number of notices will likely
	// be small, and this function is probably only used rarely, so performance
	// is unlikely to matter.
	for _, notice := range s.notices {
		if notice.ID == id {
			return notice
		}
	}
	return nil
}

func (s *State) flattenNotices(filter *NoticeFilter) []*Notice {
	now := time.Now()
	var notices []*Notice
	for _, n := range s.notices {
		if n.expired(now) || !filter.matches(n) {
			continue
		}
		notices = append(notices, n)
	}
	return notices
}

func (s *State) unflattenNotices(flat []*Notice) {
	now := time.Now()
	s.notices = make(map[noticeKey]*Notice)
	for _, n := range flat {
		if n.expired(now) {
			continue
		}
		userID, hasUserID := n.UserID()
		uniqueKey := noticeKey{hasUserID, userID, n.Type, n.Key}
		s.notices[uniqueKey] = n
	}
}

// WaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by the last-repeated time.
//
// It waits till there is at least one matching notice or the context is
// cancelled. If there are existing notices that match the filter,
// WaitNotices will return them immediately.
func (s *State) WaitNotices(ctx context.Context, filter *NoticeFilter) ([]*Notice, error) {
	s.reading()

	// If there are existing notices, return them right away.
	//
	// State is already locked here by the caller, so notices won't be added
	// concurrently.
	notices := s.Notices(filter)
	if len(notices) > 0 {
		return notices, nil
	}

	// When the context is done/cancelled, wake up the waiters so that they
	// can check their ctx.Err() and return if they're cancelled.
	//
	// TODO: replace this with context.AfterFunc once we're on Go 1.21.
	stop := contextAfterFunc(ctx, func() {
		// We need to acquire the cond lock here to be sure that the Broadcast
		// below won't occur before the call to Wait, which would result in a
		// missed signal (and deadlock).
		s.noticeCond.L.Lock()
		defer s.noticeCond.L.Unlock()

		s.noticeCond.Broadcast()
	})
	defer stop()

	for {
		// Wait till a new notice occurs or a context is cancelled.
		s.noticeCond.Wait()

		// If this context is cancelled, return the error.
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, ctxErr
		}

		// Otherwise check if there are now matching notices.
		notices = s.Notices(filter)
		if len(notices) > 0 {
			return notices, nil
		}
	}
}

// Remove this and just use context.AfterFunc once we're on Go 1.21.
func contextAfterFunc(ctx context.Context, f func()) func() {
	stopCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			f()
		case <-stopCh:
		}
	}()
	stop := func() {
		close(stopCh)
	}
	return stop
}
