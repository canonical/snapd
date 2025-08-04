// -*- Mode: Go; indent-tabs-mode: t -*-

// Copyright (c) 2025 Canonical Ltd
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

package notices

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

// NoticeBackend defines the functionality required to serve notices.
type NoticeBackend interface {
	// BackendNotices returns the list of notices that match the filter
	// (if any), ordered by the last-repeated time.
	BackendNotices(filter *state.NoticeFilter) []*state.Notice

	// BackendNotice returns a single notice by ID, or nil if not found.
	BackendNotice(id string) *state.Notice

	// BackendWaitNotices waits for notices that match the filter to exist or
	// occur, returning the list of matching notices ordered by last-repeated
	// time.
	//
	// It waits till there is at least one matching notice, the context is
	// cancelled, or the timestamp of the BeforeOrAt filter has passed (if it
	// is nonzero). If there are existing notices that match the filter,
	// BackendWaitNotices will return them immediately.
	BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error)
}

// NoticeManager provides an abstraction layer over multiple notice backends,
// ensuring correctness and consistency of notices and providing functions to
// query notices across those backends.
type NoticeManager struct {
	// state is a wrapper around the snapd state so that the manager can provide
	// unique notice IDs and timestamps to all backends, and to make the state
	// itself be a notice backend. State is an implicit notice backend for any
	// notice types which have no backends registered for them.
	state stateBackend

	// rwMu guards against new backends being registered. It must be held for
	// writing when adding a new backend, and held for reading when using any
	// of the other methods which touch the backends.
	rwMu sync.RWMutex
	// backends is the list of all notice backends which are registered as
	// providers for at least one notice type.
	backends []NoticeBackend
	// idNamespaceToBackend maps from a prefix used to namespace notice IDs to
	// the backend which registered that namespace. This allows querying just
	// the relevant backend when attempting to look up a notice by ID.
	//
	// No two backends may register the same namespace, but a given backend may
	// register the same namespace multiple times (e.g. for different notice
	// types).
	//
	// For registered notice backends (other than state), namespaced IDs must
	// be of the form "<prefix>-<id>". For notices from state, the IDs must not
	// contain '-'.
	idNamespaceToBackend map[string]NoticeBackend
	// noticeTypeBackends maps from notice type to the set of notice backends
	// which are capable of providing notices of that type.
	noticeTypeBackends map[state.NoticeType][]NoticeBackend
}

// stateBackend wraps a state to ensure that the state lock is acquired when
// checking notices.
type stateBackend struct {
	*state.State
}

func (sb stateBackend) BackendNotices(filter *state.NoticeFilter) []*state.Notice {
	sb.Lock()
	defer sb.Unlock()
	return sb.Notices(filter)
}

func (sb stateBackend) BackendNotice(id string) *state.Notice {
	sb.Lock()
	defer sb.Unlock()
	return sb.Notice(id)
}

func (sb stateBackend) BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	sb.Lock()
	defer sb.Unlock()
	return sb.WaitNotices(ctx, filter)
}

// NewNoticeManager returns a new NoticeManager based on the given state, and
// registers that state as a NoticeBackend for notice types it provides.
func NewNoticeManager(st *state.State) *NoticeManager {
	wrapper := stateBackend{st}
	nm := &NoticeManager{
		state:                wrapper,
		backends:             []NoticeBackend{wrapper},
		idNamespaceToBackend: make(map[string]NoticeBackend),
		noticeTypeBackends:   make(map[state.NoticeType][]NoticeBackend),
	}

	return nm
}

// RegisterBackend registers the given backend with the notice manager as a
// provider of notices of the given type with IDs prefixed by the given
// namespace.
//
// The state's last notice timestamp is updated according to the timestamp of
// the most recent notice from this backend which matches the given type
// (retrieved by invoking BackendNotices on this backend), if any such notices
// exist.
//
// Returns a closure which can be used by the backend to validate new notices
// added by that backend. The backend is responsible for ensuring that the
// notices which it serves are valid according to the closure.
func (nm *NoticeManager) RegisterBackend(bknd NoticeBackend, typ state.NoticeType, namespace string) (validateNotice func(id string, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) error, retErr error) {
	if namespace == "" {
		return nil, fmt.Errorf("internal error: cannot register notice backend with empty namespace")
	}
	nm.rwMu.Lock()
	defer nm.rwMu.Unlock()
	// Check that this namespace is not already registered to another backend
	if existingBknd, ok := nm.idNamespaceToBackend[namespace]; ok && existingBknd != bknd {
		return nil, fmt.Errorf("internal error: cannot register notice backend with namespace which is already registered to a different backend: %q", namespace)
	}

	// From this point on, no errors can occur, so free to mutate nm.

	if !backendsContain(nm.backends, bknd) {
		nm.backends = append(nm.backends, bknd)
	}

	nm.idNamespaceToBackend[namespace] = bknd

	typeBackends, ok := nm.noticeTypeBackends[typ]
	if !ok {
		nm.noticeTypeBackends[typ] = []NoticeBackend{bknd}
	} else if !backendsContain(typeBackends, bknd) {
		nm.noticeTypeBackends[typ] = append(typeBackends, bknd)
	}

	// Since each backend may only be registered to a given notice type once,
	// we can automatically update the state's lastNoticeTimestamp according
	// to the timestamp of the last notice of this type from this backend.
	filter := &state.NoticeFilter{Types: []state.NoticeType{typ}}
	notices := bknd.BackendNotices(filter)
	if len(notices) > 0 {
		lastNoticeTimestamp := notices[len(notices)-1].LastRepeated()
		nm.state.HandleReportedLastNoticeTimestamp(lastNoticeTimestamp)
	}

	// XXX: at time of writing, validateNotice is never actually used by any
	// backends.
	validateNotice = func(id string, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) error {
		if err := state.ValidateNotice(noticeType, key, options); err != nil {
			return err
		}
		// Check that the type matches what was registered
		if noticeType != typ {
			return fmt.Errorf("cannot add %s notice to notice backend registered to provide %s notices", noticeType, typ)
		}
		// Check that the ID starts with the namespace prefix or, if the
		// namespace is empty, check that the ID has no prefix.
		prefix, ok := prefixFromID(id)
		if !ok {
			return fmt.Errorf("cannot add notice without ID prefix to notice backend registered with namespace: %q", namespace)
		} else if prefix != namespace {
			return fmt.Errorf("cannot add notice with ID prefix not matching the namespace registered to the notice backend: %q != %q", prefix, namespace)
		}
		return nil
	}

	return validateNotice, nil
}

// TODO: replace this with slices.Contains() once we're on go 1.21+.
func backendsContain(backends []NoticeBackend, backend NoticeBackend) bool {
	for _, bknd := range backends {
		if bknd == backend {
			return true
		}
	}
	return false
}

// prefixFromID returns the namespace prefix from the given ID, if it has one.
func prefixFromID(id string) (prefix string, ok bool) {
	prefix, _, found := strings.Cut(id, "-")
	if found {
		return prefix, true
	}
	return "", false
}

// NextNoticeTimestamp returns a timestamp which is guaranteed to be after the
// previous notice timestamp, and updates the last notice timestamp in the
// state.
func (nm *NoticeManager) NextNoticeTimestamp() time.Time {
	return nm.state.NextNoticeTimestamp()
}

// Notices returns the list of notices that match the filter (if any),
// ordered by the last-repeated time, across all backends relevant to the given
// filter. A backend is relevant if it is registered as a provider of any of
// the types included in the filter, or if the filter does not specify any
// types.
//
// The caller must not hold state lock, as the manager may need to take it to
// check notices from state.
func (nm *NoticeManager) Notices(filter *state.NoticeFilter) []*state.Notice {
	nm.rwMu.RLock()
	defer nm.rwMu.RUnlock()

	backendsToCheck := nm.relevantBackendsForFilter(filter)
	switch len(backendsToCheck) {
	case 0:
		// This should be impossible, since state is always an implicit backend
		// if no other backend is registered for a given type.
		return nil
	case 1:
		return backendsToCheck[0].BackendNotices(filter)
	}

	now := time.Now()
	return doNotices(backendsToCheck, filter, now)
}

// relevantBackendsForFilter returns all backends which are registered as
// providers of any of the types included in the given filter. If the filter
// specifies no types, then all backends are returned.
//
// The caller must ensure that the notice manager rwMu is held for reading.
func (nm *NoticeManager) relevantBackendsForFilter(filter *state.NoticeFilter) []NoticeBackend {
	if filter == nil || len(filter.Types) == 0 {
		// No types specified, so assume all backends are relevant
		return nm.backends
	}
	backendsSet := make(map[NoticeBackend]bool)
	for _, typ := range filter.Types {
		backends, ok := nm.noticeTypeBackends[typ]
		if !ok {
			// No backend registered for this type, so the state is the
			// implicit provider of notices of this type.
			backendsSet[nm.state] = true
			continue
		}
		for _, backend := range backends {
			// If there are backends registered with this type, then state
			// will not be checked.
			// TODO: If in the future we want to allow state and another backend
			// to both provide notices of the same type, we need to allow state
			// to be registered as a backend with an empty namespace.
			backendsSet[backend] = true
		}
	}

	backendsList := make([]NoticeBackend, 0, len(backendsSet))
	for backend := range backendsSet {
		backendsList = append(backendsList, backend)
	}
	return backendsList
}

// doNotices checks the given backends for notices matching the given filter
// which occurred before or at the given timestamp.
func doNotices(backendsToCheck []NoticeBackend, filter *state.NoticeFilter, beforeOrAt time.Time) []*state.Notice {
	// Ensure all backends have the same BeforeOrAt time so there is no race
	// between one backend returning its existing notices and then another
	// backend recording a new notice. As such, if the filter has no BeforeOrAt
	// or has it set in the future, replace it with the given timestamp.
	if filter == nil || filter.BeforeOrAt.IsZero() || filter.BeforeOrAt.After(beforeOrAt) {
		// Don't mutate any existing filter, so make a copy
		var newFilter state.NoticeFilter
		if filter != nil {
			newFilter = *filter
		}
		newFilter.BeforeOrAt = beforeOrAt
		filter = &newFilter
	}

	var notices []*state.Notice
	// TODO: if backends are slow, query each backend in its own goroutine
	for _, backend := range backendsToCheck {
		notices = append(notices, backend.BackendNotices(filter)...)
	}
	state.SortNotices(notices)

	return notices
}

// Notice returns a single notice by ID, or nil if not found.
//
// The namespace prefix of the given ID is used to identify which notice
// backend should be queried. If the ID has no prefix, then snapd state is
// checked.
//
// If the ID has a prefix but no backend is registered with a matching namespace, returns nil.
//
// The caller must not hold state lock, as the manager may need to take it to
// check notices from state.
func (nm *NoticeManager) Notice(id string) *state.Notice {
	nm.rwMu.RLock()
	defer nm.rwMu.RUnlock()

	prefix, ok := prefixFromID(id)
	if !ok {
		// No namespace prefix, so must be from state
		return nm.state.BackendNotice(id)
	}

	backend, ok := nm.idNamespaceToBackend[prefix]
	if !ok {
		// No backend is capable of producing notices with this namespace prefix.
		return nil
	}

	return backend.BackendNotice(id)
}

// WaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by the last-repeated time,
// across all backends relevant to the given filter. A backend is relevant if
// it is registered as a provider of any of the types included in the filter,
// or if the filter does not specify any types.
//
// It waits till there is at least one matching notice, the context is
// cancelled, or the timestamp of the BeforeOrAt filter has passed (if it is
// nonzero). If there are existing notices that match the filter, WaitNotices
// will return them immediately.
//
// All of this holds true across all backends which are relevant to the given
// filter. A backend is relevant if it is registered as a provider of any of
// the types included in the filter, or if the filter does not specify any
// types.
//
// The caller must not hold state lock, as the manager may need to take it to
// check notices from state.
func (nm *NoticeManager) WaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	nm.rwMu.RLock()
	defer nm.rwMu.RUnlock()

	backendsToCheck := nm.relevantBackendsForFilter(filter)
	switch len(backendsToCheck) {
	case 0:
		// This should be impossible, since state is always an implicit backend
		// if no other backend is registered for a given type.
		return nil, nil
	case 1:
		return backendsToCheck[0].BackendWaitNotices(ctx, filter)
	}

	now := time.Now()

	// If there are existing notices, return them right away.
	// Notices() sets a BeforeOrAt filter if there isn't one already set, so
	// there can be no race between one backend returning its existing notices
	// and then another backend recording a new notice.
	notices := doNotices(backendsToCheck, filter, now)
	if len(notices) > 0 {
		return notices, nil
	}

	if filter != nil && !filter.BeforeOrAt.IsZero() && filter.BeforeOrAt.Before(now) {
		// Since each backend returned, none can create a notice with a
		// timestamp before now, and since the original filter's Before field
		// is <= now, no notices can be created matching the filter.
		return nil, nil
	}

	// Ask each backend to return its notices as soon as one is added. Once
	// one backend returns notices, cancel the requests to the other backends.
	// Thus, create a channel with capacity 1 so only one backend can send
	// notices back to the main thread.
	noticesChan := make(chan []*state.Notice, 1)
	backendCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup // Track when all queryBackend calls have returned

	queryBackend := func(bknd NoticeBackend) {
		defer wg.Done()
		// Ignore error, as it should only ever be the context cancellation
		// error, which we'll handle below.
		backendNotices, _ := bknd.BackendWaitNotices(backendCtx, filter)
		if len(backendNotices) == 0 {
			return
		}
		select {
		case noticesChan <- backendNotices:
			// Successfully sent notices back to caller. Cancel any of the other
			// goroutinues that are inside of BackendWaitNotices
			cancel()
		default:
			// This channel was already full, so we know that another backend
			// already wrote some notices to the channel.
		}
	}

	wg.Add(len(backendsToCheck))
	for _, backend := range backendsToCheck {
		go queryBackend(backend)
	}

	// It is important that we wait for all goroutines to exit before we read
	// from noticesChan. Otherwise, we might allow another backend to put
	// something inside of noticeChan and think that it was the first to return.
	wg.Wait()

	select {
	case notices = <-noticesChan:
		// A backend returned one or more notices
	case <-ctx.Done():
		// Request was cancelled
		return nil, ctx.Err()
	default:
		// If no backend wrote any notices to the channel and the context was
		// not cancelled, then no backend was capable of producing a notice
		// matching the filter, so each returned no notices.
		return nil, nil
	}

	// Get the last repeated timestamp of the newest received notice
	lastRepeated := notices[len(notices)-1].LastRepeated()

	// Re-query all backends for any notices which occurred before or at the
	// last repeated timestamp.
	//
	// XXX: there's a chance some notices will be excluded here. In particular,
	// a notice previously returned from one of the backends could have
	// re-occurred since then, thus making its last-repeated timestamp after
	// the now timestamp and causing it to be omitted from the final
	// response. This is acceptable, as the new occurrence can be retrieved by
	// a future request, and potentially has more up-to-date data, so it
	// supercedes the occurrence of the notice which is being omitted.
	notices = doNotices(backendsToCheck, filter, lastRepeated)
	return notices, nil
}
