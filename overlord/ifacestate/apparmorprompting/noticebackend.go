// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/notices"
	"github.com/snapcore/snapd/overlord/state"
)

const (
	promptNoticeNamespace = "prompt"
	ruleNoticeNamespace   = "rule"
	defaultExpireAfter    = 24 * time.Hour
)

// noticeBackends manages notice backends related to prompting.
type noticeBackends struct {
	promptBackend *noticeTypeBackend
	ruleBackend   *noticeTypeBackend
}

func newNoticeBackends(noticeMgr *notices.NoticeManager) (*noticeBackends, error) {
	nextNoticeTimestamp := noticeMgr.NextNoticeTimestamp

	now := time.Now()

	if err := os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create interfaces requests run directory: %w", err)
	}

	path := filepath.Join(dirs.SnapInterfacesRequestsRunDir, "prompt-notices.json")
	promptNoticeBackend, err := newNoticeTypeBackend(now, nextNoticeTimestamp, path, state.InterfacesRequestsPromptNotice, promptNoticeNamespace)
	if err != nil {
		return nil, err
	}

	path = filepath.Join(dirs.SnapInterfacesRequestsRunDir, "rule-notices.json")
	ruleNoticeBackend, err := newNoticeTypeBackend(now, nextNoticeTimestamp, path, state.InterfacesRequestsRuleUpdateNotice, ruleNoticeNamespace)
	if err != nil {
		return nil, err
	}

	backends := &noticeBackends{
		promptBackend: promptNoticeBackend,
		ruleBackend:   ruleNoticeBackend,
	}

	return backends, nil
}

func (nb *noticeBackends) registerWithManager(noticeMgr *notices.NoticeManager) error {
	drainNotices := true
	for _, bknd := range []*noticeTypeBackend{nb.promptBackend, nb.ruleBackend} {
		// We don't use the validation closure, since notices are produced
		// directly to satisfy validation.
		_, drainedNotices, err := noticeMgr.RegisterBackend(bknd, bknd.noticeType, bknd.namespace, drainNotices)
		if err != nil {
			// This should never occur
			return fmt.Errorf("internal error: cannot register prompting manager as a %s notice backend", bknd.namespace)
		}
		// Drained notices should only occur the first time snapd starts after
		// refreshing to a new release which uses this notice backend. This is
		// a migration to ensure no information is lost when state is no longer
		// responsible for notices of this type.
		for _, notice := range drainedNotices {
			// Re-create each notice in the backend, so no data is lost before
			// a client can receive it. The ID will be different, but the key
			// will be the same.
			userID, _ := notice.UserID() // prompting notices always have UserID
			promptingID, err := prompting.IDFromString(notice.Key())
			if err != nil {
				// Should never occur, as all prompting notices had key set as
				// promptID.String() or ruleID.String()
				continue
			}
			if err = bknd.addNotice(userID, promptingID, notice.LastData()); err != nil {
				// Should never occur, only error would be if two notices with
				// the same key (prompt/rule ID) existed for different users,
				// which should never happen. Or if there's an error saving.
				logger.Noticef("WARNING: cannot migrate notice from state to %s notice backend: %v", bknd.noticeType, err)
				continue
			}
		}
	}
	return nil
}

// noticeTypeBackend manages notices for a particular notice type.
type noticeTypeBackend struct {
	// rwmu must be held for writing when adding a notice and held for reading
	// when reading notices.
	rwmu sync.RWMutex
	// nextNoticeTimestamp is a closure derived from a notice manager which
	// returns a unique and monotonically increasing next notice timestamp.
	nextNoticeTimestamp func() time.Time
	// filepath is the path where notices for this backend are stored on disk.
	filepath string
	// noticeType is the type of notice managed by this backend.
	noticeType state.NoticeType
	// namespace is the prefix for the IDs of notices managed by this backend.
	namespace string
	// userNotices maps from user ID to the list of notices managed by this
	// backend which are associated with that user. The notices in each list
	// must always remain sorted by last repeated timestamp.
	//
	// This is optimized for the prompting use-case: notice requests for only
	// one user at a time, with the most recent notices being the ones most
	// likely to re-occur.
	userNotices map[uint32][]*state.Notice
	// idToNotice maps from notice ID to the notice itself. This is used to
	// efficiently look up the notice associated with a particular ID, and to
	// ensure that no two notices for different users can have the same ID.
	idToNotice map[string]*state.Notice
}

func newNoticeTypeBackend(now time.Time, nextNoticeTimestamp func() time.Time, path string, noticeType state.NoticeType, namespace string) (*noticeTypeBackend, error) {
	ntb := &noticeTypeBackend{
		nextNoticeTimestamp: nextNoticeTimestamp,
		filepath:            path,
		noticeType:          noticeType,
		namespace:           namespace,
	}
	if err := ntb.load(now); err != nil {
		return nil, err
	}
	return ntb, nil
}

// addNotice records an occurrence of a notice with the specified user ID, a
// key equal to the given prompt/rule ID, and the given data, with notice ID
// and type derived from the receiver.
func (ntb *noticeTypeBackend) addNotice(userID uint32, id prompting.IDType, data map[string]string) error {
	ntb.rwmu.Lock()
	defer ntb.rwmu.Unlock()
	key := id.String()
	noticeID := fmt.Sprintf("%s-%s", ntb.namespace, key)

	userNotices, existingNotice, existingIndex, err := ntb.searchExistingNotices(userID, noticeID)
	if err != nil {
		return err
	}

	// Now that errors can't occur (other than save error), get a new unique
	// timestamp from the state, which will bump the state's noticeLastTimestamp
	timestamp := ntb.nextNoticeTimestamp()

	// Check if any notices have expired relative to the new timestamp.
	// Since they're sorted, as soon as we see a non-expired notice, bail out.
	// Do this before potentially calling Reoccur on the existing notice, so we
	// see its original timestamp in the sorted slice.
	expiredCount := 0
	for _, n := range userNotices {
		if !n.Expired(timestamp) {
			break
		}
		expiredCount++
	}

	var newNotice *state.Notice
	if existingNotice != nil && !existingNotice.Expired(timestamp) {
		newNotice = existingNotice.DeepCopy()
		newNotice.Reoccur(timestamp, data, 0)
	} else {
		newNotice = state.NewNotice(noticeID, &userID, ntb.noticeType, key, timestamp, data, 0, defaultExpireAfter)
	}

	newUserNotices := appendNotice(userNotices, newNotice, existingIndex, expiredCount)

	ntb.userNotices[userID] = newUserNotices
	ntb.idToNotice[noticeID] = newNotice

	if err := ntb.save(); err != nil {
		ntb.userNotices[userID] = userNotices
		if existingNotice != nil {
			ntb.idToNotice[noticeID] = existingNotice
		} else {
			delete(ntb.idToNotice, noticeID)
		}
		return fmt.Errorf("cannot add notice to prompting %s backend: %w", ntb.noticeType, err)
	}

	// Now that we've successfully saved, delete the expired notices
	for _, expiredNotice := range userNotices[:expiredCount] {
		delete(ntb.idToNotice, expiredNotice.ID())
	}

	return nil
}

// searchExistingNotice looks up the list of existing notices for the given
// userID and checks whether a notice with the given noticeID already exists.
//
// Returns the slice of existing notices for the given userID. If the notice
// does exist, a pointer to it is returned, along with the index at which it
// occurs in the userNotices slice. If it does not exist, returns a nil
// existingNotice and returns existingIndex of -1.
//
// The caller must ensure that the backend mutex is locked.
func (ntb *noticeTypeBackend) searchExistingNotices(userID uint32, noticeID string) (userNotices []*state.Notice, notice *state.Notice, existingIndex int, err error) {
	notice, ok := ntb.idToNotice[noticeID]
	if !ok {
		userNotices = ntb.userNotices[userID]
		return userNotices, nil, -1, nil
	}

	if existingUserID, ok := notice.UserID(); !ok || existingUserID != userID {
		// This should never occur, since prompting notices always have UserIDs
		// and prompt/rule IDs are globally unique.
		if !ok {
			return nil, nil, -1, fmt.Errorf("cannot add %s notice with ID %s for user %d: notice with the same ID already exists without user", ntb.namespace, noticeID, userID)
		}
		return nil, nil, -1, fmt.Errorf("cannot add %s notice with ID %s for user %d: notice with the same ID already exists for user %d", ntb.namespace, noticeID, userID, existingUserID)
	}

	userNotices, ok = ntb.userNotices[userID]
	if !ok {
		// This should never occur.
		return nil, nil, -1, fmt.Errorf("internal error: notice ID maps to notice with user which doesn't exist in user notices: %v", notice)
	}

	// Find the index of the existing notice with this ID.
	// Since the user notices are sorted by LastRepeated timestamp, and
	// each notice has a unique LastRepeated timestamp, we can use binary
	// search by LastRepeated timestamp.
	// XXX: maybe use slices.BinarySearchFunc instead once on go 1.21+
	existingIndex = sort.Search(len(userNotices), func(i int) bool {
		// Find first index which has a LastRepeated timestamp >= the
		// existing notice, since we're binary searching for that notice.
		return !userNotices[i].LastRepeated().Before(notice.LastRepeated())
	})
	if existingIndex >= len(userNotices) || userNotices[existingIndex] != notice {
		// ID maps to a notice which doesn't actually exist in userNotices.
		// This should never occur.
		return nil, nil, -1, fmt.Errorf("internal error: notice ID maps to notice which doesn't exist in user notices: %v not in %v", notice, userNotices)
	}

	return userNotices, notice, existingIndex, nil
}

// appendNotice returns a new slice of notices by copying non-expired notices
// other than the existing notice, if it exists, and appending the given new
// notice to the end of the slice. If the notice did not previously exist in
// the userNotices slice, existingIndex should be -1. The caller must ensure
// that the given notices are sorted by last repeated timestamp, and the first
// expiredCount notices are expired.
func appendNotice(notices []*state.Notice, newNotice *state.Notice, existingIndex int, expiredCount int) []*state.Notice {
	newNotices := make([]*state.Notice, 0, len(notices)-expiredCount+1)
	for i := expiredCount; i < len(notices); i++ {
		if i != existingIndex {
			newNotices = append(newNotices, notices[i])
		}
	}
	newNotices = append(newNotices, newNotice)
	return newNotices
}

// BackendNotices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time.
//
// The caller must not mutate the data within the returned slice.
func (ntb *noticeTypeBackend) BackendNotices(filter *state.NoticeFilter) []*state.Notice {
	// just a naive stub, for now, which ignores much of the filter
	ntb.rwmu.RLock()
	defer ntb.rwmu.RUnlock()
	now := time.Now()

	// XXX: this is wrong, replace with a real implementation
	var notices []*state.Notice
	for userID, userNotices := range ntb.userNotices {
		if filter != nil && filter.UserID != nil && *filter.UserID != userID {
			continue
		}
		for _, notice := range userNotices {
			if !notice.Expired(now) {
				notices = append(notices, notice)
			}
		}
	}
	state.SortNotices(notices)
	return notices
}

// BackendNotice returns a single notice by ID, or nil if not found.
func (ntb *noticeTypeBackend) BackendNotice(id string) *state.Notice {
	ntb.rwmu.RLock()
	defer ntb.rwmu.RUnlock()
	if noticeEntry, ok := ntb.idToNotice[id]; ok {
		return noticeEntry
	}
	return nil
}

// BackendWaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by last-repeated time.
//
// It waits till there is at least one matching notice, the context is
// cancelled, or the timestamp of the BeforeOrAt filter has passed (if it is
// nonzero). If there are existing notices that match the filter,
// BackendWaitNotices will return them immediately.
func (ntb *noticeTypeBackend) BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	// just a non-functional stub, for now
	return nil, nil
}

type savedNotices struct {
	UserNotices map[uint32][]*state.Notice `json:"user-notices"`
}

// Loads existing notices for this backend from disk.
//
// The caller must ensure that the lock is held for writing.
func (ntb *noticeTypeBackend) load(now time.Time) error {
	f, err := os.Open(ntb.filepath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ntb.userNotices = make(map[uint32][]*state.Notice)
			ntb.idToNotice = make(map[string]*state.Notice)
			return nil
		}
		return fmt.Errorf("cannot open %s notices file: %w", ntb.namespace, err)
	}
	defer f.Close()
	var saved savedNotices
	if err = json.NewDecoder(f).Decode(&saved); err != nil {
		return fmt.Errorf("cannot unmarshal %s notices file: %w", ntb.namespace, err)
	}
	ntb.userNotices = make(map[uint32][]*state.Notice)
	ntb.idToNotice = make(map[string]*state.Notice)
	// Prune expired notices
	for user, notices := range saved.UserNotices {
		ntb.userNotices[user] = notices[:0]
		for i, n := range notices {
			if !n.Expired(now) {
				ntb.userNotices[user] = notices[i:]
				break
			}
		}
		// Construct ID mappings for these notices
		for _, n := range ntb.userNotices[user] {
			ntb.idToNotice[n.ID()] = n
		}
	}
	return nil
}

// Save notices for this backend to disk.
//
// The caller must ensure that the lock is held.
func (ntb *noticeTypeBackend) save() error {
	b, err := json.Marshal(savedNotices{UserNotices: ntb.userNotices})
	if err != nil {
		// Should not occur, marshalling should always succeed
		return fmt.Errorf("cannot marshal %s notices: %w", ntb.namespace, err)
	}
	return osutil.AtomicWriteFile(ntb.filepath, b, 0o600, 0)
}
