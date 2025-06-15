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

package notices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	timeNow = time.Now
)

type noticeManagerKey struct{}

// NoticeManager holds the internal notice state and manages retrieving and
// adding notices.
type NoticeManager struct {
	noticeMu            sync.RWMutex
	lastNoticeID        int
	lastNoticeTimestamp time.Time
	noticeCond          *sync.Cond
	notices             map[noticeKey]*Notice
}

// Manager returns a new NoticeManager.
func Manager(st *state.State) (*NoticeManager, error) {
	m := &NoticeManager{
		notices: map[noticeKey]*Notice{},
	}
	m.noticeCond = sync.NewCond(&m.noticeMu)

	// Save manager in the state cache so that users of the state can access
	// it without needing a reference to the overlord itself.
	st.Cache(noticeManagerKey{}, m)

	// Also save a callback so that the state is able to record change update
	// notices without needing to import the notices package.
	addChangeUpdateNotice := func(ch *state.Change) error {
		opts := &AddNoticeOptions{
			Data: map[string]string{"kind": ch.Kind()},
		}
		_, err := m.AddNotice(nil, ChangeUpdateNotice, ch.ID(), opts)
		return err
	}
	st.Cache(state.AddChangeUpdateNoticeKey{}, addChangeUpdateNotice)

	return m, nil
}

// Ensure implements StateManager.Ensure. Prunes expired notices.
func (m *NoticeManager) Ensure() error {
	now := time.Now()
	for k, n := range m.notices {
		if n.expired(now) {
			delete(m.notices, k)
		}
	}
	// XXX: do we need to proactively save to disk if any were pruned?
	return nil
}

// StartUp implements StateStarterUp.Startup.
//
// Load any existing notices from disk to populate the notice manager.
//
// XXX: Should all of this be done during Manager() creation instead?
func (m *NoticeManager) StartUp() error {
	f, err := os.Open(filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "notices.json"))
	defer f.Close()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cannot open notice state file: %w", err)
	}
	if err = json.NewDecoder(f).Decode(m); err != nil {
		return fmt.Errorf("cannot unmarshal notice state file: %w", err)
	}
	return nil
}

type marshalledNoticeState struct {
	Notices             []*Notice `json:"notices,omitempty"`
	LastNoticeID        int       `json:"last-nodice-id"`
	LastNoticeTimestamp time.Time `json:"last-notice-timestamp,omitzero"`
}

func (m *NoticeManager) MarshalJSON() ([]byte, error) {
	m.noticeMu.RLock()
	defer m.noticeMu.RUnlock()

	return json.Marshal(marshalledNoticeState{
		Notices:             m.flattenNotices(nil),
		LastNoticeID:        m.lastNoticeID,
		LastNoticeTimestamp: m.lastNoticeTimestamp,
	})
}

func (m *NoticeManager) UnmarshalJSON(data []byte) error {
	m.noticeMu.Lock()
	defer m.noticeMu.Unlock()

	var unmarshalled marshalledNoticeState
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	m.unflattenNotices(unmarshalled.Notices)
	m.lastNoticeID = unmarshalled.LastNoticeID
	m.lastNoticeTimestamp = unmarshalled.LastNoticeTimestamp
	return nil
}

// AddNotice records an occurrence of a notice with the specified type and key
// and options.
func (m *NoticeManager) AddNotice(userID *uint32, noticeType NoticeType, key string, options *AddNoticeOptions) (string, error) {
	if options == nil {
		options = &AddNoticeOptions{}
	}
	err := ValidateNotice(noticeType, key, options)
	if err != nil {
		return "", fmt.Errorf("internal error: %w", err)
	}

	m.noticeMu.Lock()
	defer m.noticeMu.Unlock()

	now := options.Time
	if now.IsZero() {
		now = timeNow()
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
		if !now.After(m.lastNoticeTimestamp) {
			now = m.lastNoticeTimestamp.Add(time.Nanosecond)
		}
		m.lastNoticeTimestamp = now
	}
	now = now.UTC()
	newOrRepeated := false
	uid, hasUserID := flattenUserID(userID)
	uniqueKey := noticeKey{hasUserID, uid, noticeType, key}
	notice, ok := m.notices[uniqueKey]
	if !ok {
		// First occurrence of this notice userID+type+key
		m.lastNoticeID++
		notice = &Notice{
			id:            strconv.Itoa(m.lastNoticeID),
			userID:        userID,
			noticeType:    noticeType,
			key:           key,
			firstOccurred: now,
			lastRepeated:  now,
			expireAfter:   defaultNoticeExpireAfter,
			occurrences:   1,
		}
		m.notices[uniqueKey] = notice
		newOrRepeated = true
	} else {
		// Additional occurrence, update existing notice
		notice.occurrences++
		if options.RepeatAfter == 0 || now.After(notice.lastRepeated.Add(options.RepeatAfter)) {
			// Update last repeated time if repeat-after time has elapsed (or is zero)
			notice.lastRepeated = now
			newOrRepeated = true
		}
	}
	notice.lastOccurred = now
	notice.lastData = options.Data
	notice.repeatAfter = options.RepeatAfter

	if newOrRepeated {
		m.noticeCond.Broadcast()
	}

	return notice.id, nil
}

// Notices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time.
func (m *NoticeManager) Notices(filter *NoticeFilter) []*Notice {
	m.noticeMu.RLock()
	defer m.noticeMu.RUnlock()

	notices := m.flattenNotices(filter)
	sort.Slice(notices, func(i, j int) bool {
		return notices[i].lastRepeated.Before(notices[j].lastRepeated)
	})
	return notices
}

// Notice returns a single notice by ID, or nil if not found.
func (m *NoticeManager) Notice(id string) *Notice {
	m.noticeMu.RLock()
	defer m.noticeMu.RUnlock()

	// Could use another map for lookup, but the number of notices will likely
	// be small, and this function is probably only used rarely, so performance
	// is unlikely to matter.
	for _, notice := range m.notices {
		if notice.id == id {
			return notice
		}
	}
	return nil
}

// flattenNotices returns the current notices map as a flat list of notices.
//
// The caller must ensure that the notices lock is held for reading.
func (m *NoticeManager) flattenNotices(filter *NoticeFilter) []*Notice {
	now := time.Now()
	var notices []*Notice
	for _, n := range m.notices {
		if n.expired(now) || !filter.matches(n) {
			continue
		}
		notices = append(notices, n)
	}
	return notices
}

// unflattenNotices rebuilds the notices map from the given list of notices.
//
// The caller must ensure that the notices lock is held for writing.
func (m *NoticeManager) unflattenNotices(flat []*Notice) {
	now := time.Now()
	m.notices = make(map[noticeKey]*Notice)
	for _, n := range flat {
		if n.expired(now) {
			continue
		}
		userID, hasUserID := n.UserID()
		uniqueKey := noticeKey{hasUserID, userID, n.noticeType, n.key}
		m.notices[uniqueKey] = n
	}
}

// WaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by the last-repeated time.
//
// It waits until there is at least one matching notice or the context is
// cancelled. If there are existing notices that match the filter,
// WaitNotices will return them immediately.
func (m *NoticeManager) WaitNotices(ctx context.Context, filter *NoticeFilter) ([]*Notice, error) {
	m.noticeMu.RLock()
	defer m.noticeMu.RUnlock()

	// If there are existing notices, return them right away.
	//
	// State is already locked here by the caller, so notices won't be added
	// concurrently.
	notices := m.Notices(filter)
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
		m.noticeCond.L.Lock()
		defer m.noticeCond.L.Unlock()

		m.noticeCond.Broadcast()
	})
	defer stop()

	for {
		// Wait until a new notice occurs or a context is cancelled.
		m.noticeCond.Wait()

		// If this context is cancelled, return the error.
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, ctxErr
		}

		// Otherwise check if there are now matching notices.
		notices = m.Notices(filter)
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

// noticeManager retrieves the NoticeManager cached in the given state.
//
// If there is no cached NoticeManager, then panics with the given message.
//
// XXX: Should this function just be exported? That would make many things
// simpler.
func noticeManager(st *state.State, errMsg string) *NoticeManager {
	cached := st.Cached(noticeManagerKey{})
	if cached == nil {
		panic(errMsg)
	}
	return cached.(*NoticeManager)
}

// AddNotice records an occurrence of a notice with the specified type, key,
// and options for the notice manager cached in the given state.
func AddNotice(st *state.State, userID *uint32, noticeType NoticeType, key string, options *AddNoticeOptions) (string, error) {
	m := noticeManager(st, "internal error: cannot add a notice before NoticeManager initialization")
	return m.AddNotice(userID, noticeType, key, options)
}

// GetNotices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time, from the notice manager cached in the
// given state.
func GetNotices(st *state.State, filter *NoticeFilter) []*Notice {
	m := noticeManager(st, "internal error: cannot get notices before NoticeManager initialization")
	return m.Notices(filter)
}

// GetNotice returns a single notice by ID, or nil if not found, from the
// notice manager cached in the given state.
func GetNotice(st *state.State, id string) *Notice {
	m := noticeManager(st, "internal error: cannot get notice before NoticeManager initialization")
	return m.Notice(id)
}

// WaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by the last-repeated time,
// from the notice manager cached in the given state.
func WaitNotices(st *state.State, ctx context.Context, filter *NoticeFilter) ([]*Notice, error) {
	m := noticeManager(st, "internal error: cannot wait for notices before NoticeManager initialization")
	return m.WaitNotices(ctx, filter)
}
