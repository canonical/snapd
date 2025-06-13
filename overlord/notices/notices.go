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

	"github.com/snapcore/snapd/logger"
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
	// It waits till there is at least one matching notice or the context is
	// cancelled. If there are existing notices that match the filter,
	// WaitNotices will return them immediately.
	BackendWaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error)
}

// NoticeManager provides an abstraction layer over multiple notice backends,
// ensuring correctness and consistency of notices and providing functions to
// query notices across those backends.
type NoticeManager struct {
	// state is a wrapper around the snapd state so that the manager can provide
	// unique notice IDs and timestamps to all backends, and to make the state
	// itself be a notice backend.
	state stateBackend

	// lock guards against new backends being registered. It must be held for
	// writing when adding a new backend, and held for reading when using any
	// of the other methods which touch the backends.
	lock sync.RWMutex
	// backends is the list of all notice backends which are registered as
	// providers for at least one notice type.
	backends []NoticeBackend
	// idNamespaceToBackend maps from a prefix used to namespace notice IDs to
	// the backend which registered that namespace.
	//
	// No two backends may register the same namespace, but a given backend may
	// register the same namespace multiple times (e.g. for different notice
	// types).
	//
	// For If the registered namespace is not "", then namespaced IDs must be
	// of the form "<prefix>-<id>". If the namespace is "", then the IDs must
	// not contain '-'.
	idNamespaceToBackend map[string]NoticeBackend
	// backendTypeToNamespace maps from backend+type combination to the ID
	// prefix namespace registered with that backend and type.
	backendTypeToNamespace map[backendWithType]string
	// noticeTypeBackends maps from notice type to the set of notice backends
	// which are capable of providing notices of that type.
	noticeTypeBackends map[state.NoticeType][]NoticeBackend
}

type backendWithType struct {
	backend    NoticeBackend
	noticeType state.NoticeType
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
		state:                  wrapper,
		backends:               make([]NoticeBackend, 0, 1),
		idNamespaceToBackend:   make(map[string]NoticeBackend),
		backendTypeToNamespace: make(map[backendWithType]string),
		noticeTypeBackends:     make(map[state.NoticeType][]NoticeBackend),
	}

	// The state is always a backend for several notice types, with no namespace
	for _, typ := range []state.NoticeType{
		state.ChangeUpdateNotice,
		state.WarningNotice,
		state.RefreshInhibitNotice,
		state.SnapRunInhibitNotice,
	} {
		// State validates its own notices, so ignore the validateNotices closure
		_, err := nm.RegisterBackend(wrapper, typ, "")
		if err != nil {
			// Should not occur
			logger.Panicf("failed to register state as a notice backend for type %v", typ)
		}
	}

	return nm
}

// RegisterBackend registers the given backend with the notice manager as a
// provider of notices of the given type with IDs prefixed by the given
// namespace.
//
// Returns a closure which can be used by the backend to validate new notices
// added by that backend. The backend is responsible for ensuring that the
// notices which it serves are valid according to the closure.
func (nm *NoticeManager) RegisterBackend(bknd NoticeBackend, typ state.NoticeType, namespace string) (validateNotice func(id string, noticeType state.NoticeType, key string, options *state.AddNoticeOptions) error, retErr error) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	// Check that this namespace is not already registered to another backend
	if existingBknd, ok := nm.idNamespaceToBackend[namespace]; ok && existingBknd != bknd {
		return nil, fmt.Errorf("internal error: cannot register notice backend with namespace which is already registered to a different backend: %q", namespace)
	}

	// Check that there is not already a different namespace registered to this
	// combination of backend and type
	bkndAndTyp := backendWithType{
		backend:    bknd,
		noticeType: typ,
	}
	if existingNs, ok := nm.backendTypeToNamespace[bkndAndTyp]; ok && existingNs != namespace {
		return nil, fmt.Errorf("internal error: cannot register namespace %q with backend and notice type which are already registered with a different namespace: %q", namespace, existingNs)
	}

	// from this point on, no errors can occur, so free to mutate nm

	if !backendsContain(nm.backends, bknd) {
		nm.backends = append(nm.backends, bknd)
	}

	nm.idNamespaceToBackend[namespace] = bknd
	nm.backendTypeToNamespace[bkndAndTyp] = namespace

	typeBackends, ok := nm.noticeTypeBackends[typ]
	if !ok {
		nm.noticeTypeBackends[typ] = []NoticeBackend{bknd}
	} else if !backendsContain(typeBackends, bknd) {
		nm.noticeTypeBackends[typ] = append(typeBackends, bknd)
	}

	// XXX: should we automatically call BackendNotices() on this backend with
	// a filter for this type and use this to bump the last notice timestamp
	// (if necessary), rather than requiring the backends to do it manually?
	// That would require that the backend has reloaded all its notices by the
	// time it is registered. Which is true of both state and the prompting
	// notice backends.

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
		if ok {
			if namespace == "" {
				return fmt.Errorf("cannot add notice with ID prefix to notice backend registered with empty namespace: %q", id)
			}
			if prefix != namespace {
				return fmt.Errorf("cannot add notice with ID prefix not matching the namespace registered to the notice backend: %q != %q", id, namespace)
			}
		} else {
			if namespace != "" {
				return fmt.Errorf("cannot add notice with no ID prefix to notice backend registered with namespace %q: %q", namespace, id)
			}
		}
		return nil
	}

	return validateNotice, nil
}

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
	split := strings.SplitN(id, "-", 2)
	if len(split) > 1 {
		return split[0], true
	}
	return "", false
}

// NextNoticeTimestamp returns a timestamp which is guaranteed to be after the
// previous notice timestamp, and updates the last notice timestamp in the
// state.
func (nm *NoticeManager) NextNoticeTimestamp() time.Time {
	return nm.state.NextNoticeTimestamp()
}

// ReportLastNoticeTimestamp should be called by each notice backend during
// startup so that the manager can ensure that the last notice timestamp in the
// state is equal to the last notice timestamp of all notices across all
// backends.
func (nm *NoticeManager) ReportLastNoticeTimestamp(t time.Time) {
	nm.state.HandleReportedLastNoticeTimestamp(t)
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
	nm.lock.RLock()
	defer nm.lock.RUnlock()

	backendsToCheck := nm.relevantBackendsForFilter(filter)
	now := time.Now()

	return nm.doNotices(now, backendsToCheck, filter)
}

// doNotices checks the given backends for notices matching the given filter
// which occurred before the given timestamp.
//
// The caller must ensure that the notice manager lock is held for reading.
func (nm *NoticeManager) doNotices(now time.Time, backendsToCheck []NoticeBackend, filter *state.NoticeFilter) []*state.Notice {
	// Ensure all backends have the same Before time so there is no race
	// between one backend returning its existing notices and then another
	// backend recording a new notice. As such, if the filter has Before set in
	// the future, replace it with the current timestamp.
	if filter.Before.IsZero() || filter.Before.After(now) {
		// Don't mutate the existing filter, so make a copy
		newFilter := *filter
		newFilter.Before = now
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

// relevantBackendsForFilter returns all backends which are registered as
// providers of any of the types included in the given filter. If the filter
// specifies no types, then all backends are returned.
//
// The caller must ensure that the notice manager lock is held for reading.
func (nm *NoticeManager) relevantBackendsForFilter(filter *state.NoticeFilter) []NoticeBackend {
	if len(filter.Types) == 0 {
		// No types specified, so assume all backends are relevant
		return nm.backends
	}
	backendsSet := make(map[NoticeBackend]bool)
	for _, typ := range filter.Types {
		backends, ok := nm.noticeTypeBackends[typ]
		if !ok {
			// No backend registered for this type, so there's no way notices
			// of this type can exist.
			continue
		}
		for _, backend := range backends {
			backendsSet[backend] = true
		}
	}

	backendsList := make([]NoticeBackend, 0, len(backendsSet))
	for backend := range backendsSet {
		backendsList = append(backendsList, backend)
	}
	return backendsList
}

// Notice returns a single notice by ID, or nil if not found. Because we have
// no information about which backend (if any) provides the notice with that
// ID, we query each backend in its own goroutine, cancelling outstanding
// queries once one of the backends replies. This means that finding a notice
// in one backend is not impeded by a slow lock in another backend (e.g. state).
//
// The caller must not hold state lock, as the manager may need to take it to
// check notices from state.
func (nm *NoticeManager) Notice(id string) *state.Notice {
	nm.lock.RLock()
	defer nm.lock.RUnlock()

	noticeChan := make(chan *state.Notice)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup // so we know if all backends returned nil
	wg.Add(len(nm.backends))
	// Set up watcher to catch if all backends return nil
	go func() {
		wg.Wait()
		// All backends returned, so either one sent a notice over the
		// channel or none sent anything. In the latter case, send nil so
		// the parent thread knows to stop waiting.
		select {
		case noticeChan <- nil:
			// No other backend sent a notice, so we sent the caller nil
		case <-ctx.Done():
			// Some other backend sent a notice
		}
	}()
	// Now query each backend
	queryBackend := func(bknd NoticeBackend) {
		defer wg.Done()
		notice := bknd.BackendNotice(id)
		if notice == nil {
			return
		}
		select {
		case noticeChan <- notice:
			// The caller received the notice, all good
		case <-ctx.Done():
			// Some other backend replied, which should not occur unless
			// somehow multiple backends are responsible for the same notice
		}
	}
	for _, backend := range nm.backends {
		go queryBackend(backend)
	}

	notice := <-noticeChan
	return notice
}

// WaitNotices waits for notices that match the filter to exist or occur,
// returning the list of matching notices ordered by the last-repeated time,
// across all backends relevant to the given filter. A backend is relevant if
// it is registered as a provider of any of the types included in the filter,
// or if the filter does not specify any types.
//
// It waits till there is at least one matching notice or the context is
// cancelled. If there are existing notices that match the filter,
// WaitNotices will return them immediately.
//
// All of this holds true across all backends which are relevant to the given
// filter. A backend is relevant if it is registered as a provider of any of
// the types included in the filter, or if the filter does not specify any
// types.
//
// The caller must not hold state lock, as the manager may need to take it to
// check notices from state.
func (nm *NoticeManager) WaitNotices(ctx context.Context, filter *state.NoticeFilter) ([]*state.Notice, error) {
	nm.lock.RLock()
	defer nm.lock.RUnlock()

	backendsToCheck := nm.relevantBackendsForFilter(filter)
	if len(backendsToCheck) == 0 {
		// XXX: should this be an error, or empty list? Or should the request
		// just hang indefinitely? (The latter is what the API states)
		//return nil, fmt.Errorf("no backends can produce notices matching the filter")
		return []*state.Notice{}, nil
	}

	now := time.Now()

	// If there are existing notices, return them right away.
	// Notices() sets a Before filter if there isn't one already set, so there
	// can be no race between one backend returning its existing notices and
	// then another backend recording a new notice.
	notices := nm.doNotices(now, backendsToCheck, filter)
	if len(notices) > 0 {
		return notices, nil
	}

	if !filter.Before.IsZero() && !filter.Before.After(now) {
		// Since each backend returned, none can create a notice with a
		// timestamp before now, and since the original filter's Before field
		// is <= now, no notices can be created matching the filter.
		return notices, nil
	}

	// Ask each backend to return the first notice it finds, unless cancelled
	noticesChan := make(chan []*state.Notice)
	backendCtx, cancel := context.WithCancel(ctx)
	// TODO: on go 1.20+, use WithCancelCause instead of separate channel
	allBackendsReturned := make(chan struct{})
	defer cancel()        // Ensure the context is eventually cancelled; maybe cancel early.
	var wg sync.WaitGroup // Keep track of if all backends return empty notices.
	wg.Add(len(backendsToCheck))
	// Set up watcher in case all backends return empty notices, which should
	// only occur if they know they can't possibly produce notices which match
	// the filter.
	go func() {
		wg.Wait()
		// All backends returned, so the caller either already received a
		// non-empty list of notices, or all backends returned an empty list.
		// If the latter, then no backends are capable of producing notices
		// which match the filter.
		close(allBackendsReturned)
	}()
	queryBackend := func(bknd NoticeBackend) {
		defer wg.Done()
		// Ignore error, as it should only ever be the context cancellation
		// error, which we'll handle below.
		backendNotices, _ := bknd.BackendWaitNotices(backendCtx, filter)
		select {
		case noticesChan <- backendNotices:
			// Successfully sent notices back to caller
		case <-backendCtx.Done():
			// Some other backend replied first
		}
	}
	for _, backend := range backendsToCheck {
		go queryBackend(backend)
	}

	for len(notices) == 0 {
		select {
		case notices = <-noticesChan:
			// A backend returned one or more notices
		case <-allBackendsReturned:
			// All backends sent empty notices over their respective channels,
			// so none were capable of producing notices matching the filter.

			// XXX: should this be an error, or empty list? Or should the
			// request just hang indefinitely? (The latter is what the API states)
			//return nil, fmt.Errorf("filter cannot match any notices")
			return []*state.Notice{}, nil
		case <-ctx.Done():
			// Request was cancelled
			return nil, ctx.Err()
		}
		// It's possible a backend returned no notices (e.g. because it
		// determined that it's impossible for any notices to match the filter)
		// so try again -- that backend should not try to send another
	}
	// Cancel the requests to the other backends
	cancel()

	// Get the last repeated timestamp of the newest received notice
	lastRepeated := notices[len(notices)-1].LastRepeated

	// Re-query all backends for any notices which occurred before the last
	// repeated timestamp. Add a nanosecond to it so that that notice can still
	// be included.
	now = lastRepeated.Add(time.Nanosecond)
	// XXX: there's a chance some notices will be excluded here. In particular,
	// a notice previously returned from one of the backends could have
	// re-occurred since then, thus making its last-repeated timestamp after
	// the now timestamp and causing it to be omitted from the final
	// response. This is acceptable, as the new occurrence can be retrieved by
	// a future request, and potentially has more up-to-date data, so it
	// supercedes the occurrence of the notice which is being omitted.
	notices = nm.doNotices(now, backendsToCheck, filter)
	return notices, nil
}
