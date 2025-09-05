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
)

const (
	// defaultNoticeExpireAfter is the default expiry time for notices.
	defaultNoticeExpireAfter = 7 * 24 * time.Hour
	// maxNoticeKeyLength is the max size in bytes for a notice key.
	maxNoticeKeyLength = 256
)

// Notice represents an aggregated notice. The combination of type and key is unique.
type Notice struct {
	// Server-generated unique ID for this notice (a surrogate key).
	//
	// Currently this is a monotonically increasing number, but that may well
	// change in future. If your code relies on it being a number, it will break.
	id string

	// The UID of the user who may view this notice (often its creator).
	// A nil userID means that the notice is public (viewable by all users).
	userID *uint32

	// The notice type represents a group of notices originating from a common
	// source. For example, notices which provide human-readable warnings have
	// the type "warning".
	noticeType NoticeType

	// The notice key is a string that differentiates notices of this type.
	// Notices recorded with the type and key of an existing notice count as
	// an occurrence of that notice.
	//
	// This is limited to a maximum of MaxNoticeKeyLength bytes when added
	// (it's an error to add a notice with a longer key).
	key string

	// The first time one of these notices (type and key combination) occurs.
	firstOccurred time.Time

	// The last time one of these notices occurred. This is updated every time
	// one of these notices occurs.
	lastOccurred time.Time

	// The time this notice was last "repeated". This is set when one of these
	// notices first occurs, and updated when it reoccurs at least
	// repeatAfter after the previous lastRepeated time.
	//
	// Notices and WaitNotices return notices ordered by lastRepeated time, so
	// repeated notices will appear at the end of the returned list.
	lastRepeated time.Time

	// The number of times one of these notices has occurred.
	occurrences int

	// Additional data captured from the last occurrence of one of these notices.
	lastData map[string]string

	// How long after one of these was last repeated should we allow it to repeat.
	repeatAfter time.Duration

	// How long since one of these last occurred until we should drop the notice.
	//
	// The repeatAfter duration must be less than this, because the notice
	// won't be tracked after it expires.
	expireAfter time.Duration
}

// NewNotice returns a new notice with the given details.
func NewNotice(id string, userID *uint32, nType NoticeType, key string, timestamp time.Time, data map[string]string, repeatAfter time.Duration, expireAfter time.Duration) *Notice {
	return &Notice{
		id:            id,
		userID:        userID,
		noticeType:    nType,
		key:           key,
		firstOccurred: timestamp,
		lastOccurred:  timestamp,
		lastRepeated:  timestamp,
		occurrences:   1,
		lastData:      data,
		repeatAfter:   repeatAfter,
		expireAfter:   expireAfter,
	}
}

// Reoccur updates the receiving notice to re-occur with the given timestamp
// and data. Depending on its repeat after duration, the lastRepeated timestamp
// may be updated. Returns whether the notice was repeated.
func (n *Notice) Reoccur(now time.Time, data map[string]string, repeatAfter time.Duration) (repeated bool) {
	n.occurrences++
	repeated = false
	if repeatAfter == 0 || now.After(n.lastRepeated.Add(repeatAfter)) {
		// Update last repeated time if repeat-after time has elapsed (or is zero)
		// XXX: this is what was used previously, but it seems strange to look
		// at the repeatAfter argument instead of n.repeatAfter when deciding if
		// the lastRepeated timestamp should be updated for an existing notice.
		// It seems like the saved n.repeatAfter should be used when deciding
		// whether the current call should cause the notice to be repeated, and
		// then the given repeatAfter argument should be stored as n.repeatAfter
		// and used next time the notice is re-recorded. Otherwise, n.repeatAfter
		// is never used, so what's the point of storing it in the notice?
		n.lastRepeated = now
		repeated = true
	}
	n.lastOccurred = now
	n.lastData = data
	n.repeatAfter = repeatAfter
	return repeated
}

// DeepCopy returns a deep copy of the receiver.
func (n *Notice) DeepCopy() *Notice {
	// Create deep copies of non-primitive fields (strings are fine)
	var userID *uint32
	if n.userID != nil {
		userIDVal := *n.userID
		userID = &userIDVal
	}
	var lastData map[string]string
	if len(n.lastData) > 0 {
		lastData = make(map[string]string, len(n.lastData))
		for k, v := range n.lastData {
			lastData[k] = v
		}
	}

	return &Notice{
		id:            n.id,
		userID:        userID,
		noticeType:    n.noticeType,
		key:           n.key,
		firstOccurred: n.firstOccurred,
		lastOccurred:  n.lastOccurred,
		lastRepeated:  n.lastRepeated,
		occurrences:   n.occurrences,
		lastData:      lastData,
		repeatAfter:   n.repeatAfter,
		expireAfter:   n.expireAfter,
	}
}

func (n *Notice) String() string {
	userIDStr := "public"
	if n.userID != nil {
		userIDStr = strconv.FormatUint(uint64(*n.userID), 10)
	}
	return fmt.Sprintf("Notice %s (%s:%s:%s)", n.id, userIDStr, n.noticeType, n.key)
}

// ID returns the notice's ID.
func (n *Notice) ID() string {
	return n.id
}

// UserID returns the value of the notice's user ID and whether it is set.
// If it is nil, then the returned userID is 0, and isSet is false.
func (n *Notice) UserID() (userID uint32, isSet bool) {
	// Importantly, doesn't expose the address of the notice's user ID, so the
	// caller cannot mutate the value.
	return flattenUserID(n.userID)
}

// Type returns the notice type which represents a group of notices
// originating from a common source.
func (n *Notice) Type() NoticeType {
	return n.noticeType
}

// Key returns the notice's key.
func (n *Notice) Key() string {
	return n.key
}

// LastRepeated returns the last repeated timestamp for this notice.
func (n *Notice) LastRepeated() time.Time {
	return n.lastRepeated
}

// LastData returns the last data associated with this notice.
func (n *Notice) LastData() map[string]string {
	return n.lastData
}

func flattenUserID(userID *uint32) (uid uint32, isSet bool) {
	if userID == nil {
		return 0, false
	}
	return *userID, true
}

// Expired reports whether this notice has expired (relative to the given "now").
func (n *Notice) Expired(now time.Time) bool {
	return n.lastOccurred.Add(n.expireAfter).Before(now)
}

// jsonNotice exists so we can control how a Notice is marshalled to JSON. It
// needs to live in this package (rather than the API) because we save state
// to disk as JSON.
type jsonNotice struct {
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

func (n *Notice) MarshalJSON() ([]byte, error) {
	jn := jsonNotice{
		ID:            n.id,
		UserID:        n.userID,
		Type:          string(n.noticeType),
		Key:           n.key,
		FirstOccurred: n.firstOccurred,
		LastOccurred:  n.lastOccurred,
		LastRepeated:  n.lastRepeated,
		Occurrences:   n.occurrences,
		LastData:      n.lastData,
	}
	if n.repeatAfter != 0 {
		jn.RepeatAfter = n.repeatAfter.String()
	}
	if n.expireAfter != 0 {
		jn.ExpireAfter = n.expireAfter.String()
	}
	return json.Marshal(jn)
}

func (n *Notice) UnmarshalJSON(data []byte) error {
	var jn jsonNotice
	err := json.Unmarshal(data, &jn)
	if err != nil {
		return err
	}
	n.id = jn.ID
	n.userID = jn.UserID
	n.noticeType = NoticeType(jn.Type)
	n.key = jn.Key
	n.firstOccurred = jn.FirstOccurred
	n.lastOccurred = jn.LastOccurred
	n.lastRepeated = jn.LastRepeated
	n.occurrences = jn.Occurrences
	n.lastData = jn.LastData
	if jn.RepeatAfter != "" {
		n.repeatAfter, err = time.ParseDuration(jn.RepeatAfter)
		if err != nil {
			return fmt.Errorf("invalid repeat-after duration: %w", err)
		}
	}
	if jn.ExpireAfter != "" {
		n.expireAfter, err = time.ParseDuration(jn.ExpireAfter)
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

	// Recorded whenever an auto-refresh is inhibited for one or more snaps.
	RefreshInhibitNotice NoticeType = "refresh-inhibit"

	// Recorded by "snap run" command when it is inhibited from running a
	// a snap due an ongoing refresh.
	SnapRunInhibitNotice NoticeType = "snap-run-inhibit"

	// Recorded whenever a request prompt is created or resolved. The key for
	// interfaces-requests-prompt notices is the request prompt ID.
	InterfacesRequestsPromptNotice NoticeType = "interfaces-requests-prompt"

	// Recorded whenever a request rule is created, modified, deleted, or
	// expired. The key for interfaces-requests-rule-update notices is the
	// rule ID.
	InterfacesRequestsRuleUpdateNotice NoticeType = "interfaces-requests-rule-update"
)

func (t NoticeType) Valid() bool {
	switch t {
	case ChangeUpdateNotice, WarningNotice, RefreshInhibitNotice, SnapRunInhibitNotice, InterfacesRequestsPromptNotice, InterfacesRequestsRuleUpdateNotice:
		return true
	}
	return false
}

// NextNoticeTimestamp computes a notice timestamp which is guaranteed to be
// after the current lastNoticeTimestamp, then updates lastNoticeTimestamp to
// the result and returns it.
func (s *State) NextNoticeTimestamp() time.Time {
	s.lastNoticeTimestampMu.Lock()
	defer s.lastNoticeTimestampMu.Unlock()
	now := timeNow().UTC()
	// Ensure that two notices never have the same sent time.
	//
	// Since the Notices API receives an "after:" parameter with the
	// date and time of the last received notice to filter all the
	// previous notices and avoid duplicates, if two or more notices
	// have the same date and time, only the first will be emitted,
	// and the others will be silently discarded. This can happen in
	// systems that don't guarantee a granularity of one nanosecond
	// in their timers, which can happen in some not-so-old devices,
	// where the HPET is used instead of internal high resolution
	// timers, or in other architectures different from the X86_64.
	if !now.After(s.lastNoticeTimestamp) {
		now = s.lastNoticeTimestamp.Add(time.Nanosecond)
	}
	s.lastNoticeTimestamp = now
	return s.lastNoticeTimestamp
}

// getLastNoticeTimestamp returns the current lastNoticeTimestamp.
func (s *State) getLastNoticeTimestamp() time.Time {
	s.lastNoticeTimestampMu.Lock()
	defer s.lastNoticeTimestampMu.Unlock()
	return s.lastNoticeTimestamp
}

// HandleReportedLastNoticeTimestamp updates lastNoticeTimestamp to the given
// time if the given time is after the current lastNoticeTimestamp.
//
// This method should only be called during startup to ensure that the
// lastNoticeTimestamp value is the last timestamp of all notices across all
// notice backends.
func (s *State) HandleReportedLastNoticeTimestamp(t time.Time) {
	s.lastNoticeTimestampMu.Lock()
	defer s.lastNoticeTimestampMu.Unlock()
	if t.After(s.lastNoticeTimestamp) {
		s.lastNoticeTimestamp = t
	}
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
}

// AddNotice records an occurrence of a notice with the specified type and key
// and options.
func (s *State) AddNotice(userID *uint32, noticeType NoticeType, key string, options *AddNoticeOptions) (string, error) {
	if options == nil {
		options = &AddNoticeOptions{}
	}
	err := ValidateNotice(noticeType, key, options)
	if err != nil {
		return "", fmt.Errorf("internal error: %w", err)
	}

	s.writing()
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	now := options.Time
	if now.IsZero() {
		now = s.NextNoticeTimestamp()
	}
	now = now.UTC()
	newOrRepeated := false
	uid, hasUserID := flattenUserID(userID)
	uniqueKey := noticeKey{hasUserID, uid, noticeType, key}
	notice, ok := s.notices[uniqueKey]
	if !ok {
		// First occurrence of this notice userID+type+key
		s.lastNoticeId++
		notice = NewNotice(strconv.Itoa(s.lastNoticeId), userID, noticeType, key, now, options.Data, options.RepeatAfter, defaultNoticeExpireAfter)
		s.notices[uniqueKey] = notice
		newOrRepeated = true
	} else {
		// Additional occurrence, update existing notice
		newOrRepeated = notice.Reoccur(now, options.Data, options.RepeatAfter)
	}

	if newOrRepeated {
		s.noticeCond.Broadcast()
	}

	return notice.id, nil
}

// ValidateNotice validates notice type and key before adding.
func ValidateNotice(noticeType NoticeType, key string, options *AddNoticeOptions) error {
	if !noticeType.Valid() {
		return fmt.Errorf("cannot add notice with invalid type %q", noticeType)
	}
	if key == "" {
		return fmt.Errorf("cannot add %s notice with invalid key %q", noticeType, key)
	}
	if len(key) > maxNoticeKeyLength {
		return fmt.Errorf("cannot add %s notice with invalid key: key must be %d bytes or less", noticeType, maxNoticeKeyLength)
	}
	if noticeType == RefreshInhibitNotice && key != "-" {
		return fmt.Errorf(`cannot add %s notice with invalid key %q: only "-" key is supported`, noticeType, key)
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

	// BeforeOrAt, if set, includes only notices that were last repeated before
	// or at this time.
	BeforeOrAt time.Time
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
	if len(f.Types) > 0 && !sliceContains(f.Types, n.noticeType) {
		return false
	}
	if len(f.Keys) > 0 && !sliceContains(f.Keys, n.key) {
		return false
	}
	if !f.After.IsZero() && !n.lastRepeated.After(f.After) {
		return false
	}
	if !f.BeforeOrAt.IsZero() && n.lastRepeated.After(f.BeforeOrAt) {
		// XXX: there's a chance for a notice which would otherwise be included
		// to be omitted here, if it is repeated after the BeforeOrAt timestamp.
		// For example, if a notice is first recorded between the After and
		// BeforeOrAt timestamps, then we want it to be included, since it's a
		// new notice within the requested timeframe, but if that notice is
		// repeated after the BeforeOrAt timestamp, it will be omitted for being
		// too new. We consider this to be acceptable: the newer notice can be
		// retrieved by a future request, and potentially has more up-to-date
		// data, so it supercedes the occurrence of the notice which is being
		// omitted.
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

// futureNoticesPossible returns true if it is possible for future notices to
// be recorded which match the filter, given that any new notices must have a
// timestamp later than the given now timestamp.
func (f *NoticeFilter) futureNoticesPossible(now time.Time) bool {
	if f == nil {
		return true
	}
	if f.BeforeOrAt.IsZero() {
		return true
	}
	if !f.BeforeOrAt.Before(now) {
		return true
	}
	return false
}

// DrainNotices finds all notices in the state that match the filter (if any),
// removes them from state, and returns them, ordered by the last-repeated time.
//
// This should only be called by the notice manager in order to migrate notices
// from state to another notice backend.
func (s *State) DrainNotices(filter *NoticeFilter) []*Notice {
	s.writing()
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	now := time.Now()
	var toRemove []noticeKey
	var notices []*Notice
	for k, n := range s.notices {
		if n.Expired(now) || !filter.matches(n) {
			continue
		}
		toRemove = append(toRemove, k)
		notices = append(notices, n)
	}
	for _, k := range toRemove {
		delete(s.notices, k)
	}
	SortNotices(notices)
	return notices
}

// SortNotices sorts the given slice of notices according to the lastRepeated
// timestamp.
func SortNotices(notices []*Notice) {
	sort.Slice(notices, func(i, j int) bool {
		return notices[i].lastRepeated.Before(notices[j].lastRepeated)
	})
}

// Notices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time.
func (s *State) Notices(filter *NoticeFilter) []*Notice {
	s.noticesMu.RLock()
	defer s.noticesMu.RUnlock()
	return s.doNotices(filter)
}

// doNotices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time. The caller must hold the noticesMu for
// reading.
func (s *State) doNotices(filter *NoticeFilter) []*Notice {
	notices := s.filterNotices(filter)
	SortNotices(notices)
	return notices
}

// Notice returns a single notice by ID, or nil if not found.
func (s *State) Notice(id string) *Notice {
	s.noticesMu.RLock()
	defer s.noticesMu.RUnlock()

	// Could use another map for lookup, but the number of notices will likely
	// be small, and this function is probably only used rarely, so performance
	// is unlikely to matter.
	for _, notice := range s.notices {
		if notice.id == id {
			return notice
		}
	}
	return nil
}

// flattenNotices loops over the notices map and returns all non-expired notices
// so that they can be marshalled to disk. The notices are not sorted.
//
// State lock does not need to be held, and this method acquires noticesMu for
// reading, so noticesMu must not be held for writing by the caller.
func (s *State) flattenNotices() []*Notice {
	s.noticesMu.RLock()
	defer s.noticesMu.RUnlock()
	return s.filterNotices(nil)
}

// filterNotices returns the list of notices that match the filter (if any),
// without sorting them. The caller must hold the noticesMu for reading.
func (s *State) filterNotices(filter *NoticeFilter) []*Notice {
	now := time.Now()
	var notices []*Notice
	for _, n := range s.notices {
		if n.Expired(now) || !filter.matches(n) {
			continue
		}
		notices = append(notices, n)
	}
	return notices
}

// unflattenNotices takes a flat list of notices and replaces the notices map
// with them, ignoring expired notices in the process.
//
// Call with the state lock held. Acquires the notices lock for writing.
func (s *State) unflattenNotices(flat []*Notice) {
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()
	now := time.Now()
	s.notices = make(map[noticeKey]*Notice)
	for _, n := range flat {
		if n.Expired(now) {
			continue
		}
		userID, hasUserID := n.UserID()
		uniqueKey := noticeKey{hasUserID, userID, n.noticeType, n.key}
		// TODO: migrate any notices for types which should no longer be stored
		// in state to their appropriate backends, and don't include in state.
		s.notices[uniqueKey] = n
	}
}

// WaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by the last-repeated time.
//
// It waits till there is at least one matching notice, the context is
// cancelled, or the timestamp of the BeforeOrAt filter has passed (if it is
// nonzero). If there are existing notices that match the filter, WaitNotices
// will return them immediately.
//
// The caller should not hold state lock, since this function will not release
// state lock while waiting for a new notice to be added. Adding a new notice
// requires both state lock and an internal notices RWMutex to be held for
// writing. Previously, state lock was used as the noticeCond locker, which is
// unlocked when noticeCond.Wait is called, but now the notices RWMutex RLocker
// is used instead, so a locked state would remain locked and noticeCond.Wait
// would block until the context is cancelled.
func (s *State) WaitNotices(ctx context.Context, filter *NoticeFilter) ([]*Notice, error) {
	s.noticesMu.RLock()
	defer s.noticesMu.RUnlock()

	// It's important that we do not attempt to lock noticesMu for reading again
	// during the rest of the function call, since any attempt to lock it for
	// writing (either within the context timeout callback or externally) will
	// block any attempted call to RLock in this function, and thus prevent us
	// from releasing the RLock we already hold, and lead to deadlock.

	// If there are existing notices, return them right away.
	//
	// noticesMu is already locked here, so notices won't be added concurrently.
	notices := s.doNotices(filter)
	if len(notices) > 0 {
		return notices, nil
	}

	// When the context is done/cancelled, wake up the waiters so that they
	// can check their ctx.Err() and return if they're cancelled.
	//
	// TODO: replace this with context.AfterFunc once we're on Go 1.21.
	stop := contextAfterFunc(ctx, func() {
		// We need to acquire a lock mutually exclusive with the cond lock here
		// to be sure that the Broadcast below won't occur before the call to
		// Wait, which would result in a missed signal (and deadlock). Since
		// the cond lock is noticesMu.RLocker(), we need to acquire the lock
		// for writing.
		s.noticesMu.Lock()
		defer s.noticesMu.Unlock()

		s.noticeCond.Broadcast()
	})
	defer stop()

	for {
		// Since the noticesMu is held for writing for the duration of
		// AddNotice, there can be no notices destined for the state notices
		// map currently in-flight which have timestamps before now but have
		// not yet been added to the notices map. Therefore, if the current
		// time is after the BeforeOrAt filter, we know there can be no new
		// notices which match the filter.
		now := time.Now()
		if !filter.futureNoticesPossible(now) {
			return nil, nil
		}

		// Wait till a new notice occurs or a context is cancelled.
		// This unlocks noticeCond.L, so for this reason, it is essential that
		// noticeCond.L is noticesMu.RLocker, since that is what we hold during
		// this function call.
		s.noticeCond.Wait()

		// If this context is cancelled, return the error.
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, ctxErr
		}

		// Otherwise check if there are now matching notices.
		notices = s.doNotices(filter)
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
