// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

// Package service manages persistent snap user units through their public user
// buses, independently of the session agent used for desktop notifications.
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/systemd"
	systemduser "github.com/snapcore/snapd/systemd/user"
	"github.com/snapcore/snapd/usersession/userbus"
)

// Manager is the systemd protocol needed by a single user's service operation.
type Manager interface {
	Status(context.Context, []string) ([]*systemd.UnitStatus, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	ReloadOrRestart(context.Context, string) error
	Enable(context.Context, []string) ([]systemduser.UnitFileChange, error)
	Disable(context.Context, []string) error
	Reload(context.Context) error
}

var openManager = func(ctx context.Context, uid int) (Manager, func(), error) {
	// The operation context can expire before its rollback. Keep the bridge
	// alive until the owner closes it; each method still has a bounded context.
	signals := systemduser.NewSignalHandler()
	lifetime, cancel := context.WithCancel(context.Background())
	setupDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			cancel()
		case <-setupDone:
		}
	}()
	conn, err := userbus.Connect(lifetime, uint32(uid), dbus.WithSignalHandler(signals))
	close(setupDone)
	<-watchDone
	if err != nil {
		cancel()
		return nil, nil, err
	}
	mgr, err := systemduser.New(ctx, conn.Conn, signals)
	if err != nil {
		cancel()
		conn.Close()
		return nil, nil, err
	}
	return mgr, func() { mgr.Close(); cancel(); conn.Close() }, nil
}

type listedUnit struct {
	Name, Description, LoadState, ActiveState, SubState, Following string
	Path                                                           dbus.ObjectPath
	JobID                                                          uint32
	JobType                                                        string
	JobPath                                                        dbus.ObjectPath
}

var discover = func(ctx context.Context) ([]int, error) {
	conn, err := dbusutil.SystemBus()
	if err != nil {
		return nil, err
	}
	var units []listedUnit
	err = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units)
	if err != nil {
		return nil, err
	}
	uids := make([]int, 0)
	for _, unit := range units {
		if !strings.HasPrefix(unit.Name, "user@") || !strings.HasSuffix(unit.Name, ".service") || unit.ActiveState != "active" {
			continue
		}
		uid, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(unit.Name, "user@"), ".service"), 10, 32)
		if err != nil || uid == uint64(^uint32(0)) {
			continue
		}
		uids = append(uids, int(uid))
	}
	return normalize(uids)
}

func normalize(uids []int) ([]int, error) {
	result := make([]int, 0, len(uids))
	seen := make(map[int]bool)
	for _, uid := range uids {
		if uid < 0 || uint64(uid) >= uint64(^uint32(0)) {
			return nil, fmt.Errorf("invalid user id %d", uid)
		}
		if !seen[uid] {
			result = append(result, uid)
			seen[uid] = true
		}
	}
	sort.Ints(result)
	return result, nil
}

// Targets is an immutable snapshot of eligible user managers. A snapshot records
// missing explicit targets as failures rather than silently omitting them.
type Targets struct {
	uids    []int
	missing map[int]bool
}

// Select discovers running user@UID.service managers, including lingering users.
// A nil requested slice selects all managers; a non-nil empty slice selects none.
// It must be called without the overlord state lock.
func Select(ctx context.Context, requested []int) (*Targets, error) {
	uids, err := normalize(requested)
	if err != nil {
		return nil, err
	}
	if requested != nil && len(requested) == 0 {
		return &Targets{uids: uids}, nil
	}
	available, err := discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot discover user managers: %w", err)
	}
	available, err = normalize(available)
	if err != nil {
		return nil, err
	}
	if requested == nil {
		return &Targets{uids: available}, nil
	}
	missing := make(map[int]bool)
	for _, uid := range uids {
		i := sort.SearchInts(available, uid)
		if i == len(available) || available[i] != uid {
			missing[uid] = true
		}
	}
	return &Targets{uids: uids, missing: missing}, nil
}

// UIDs returns a copy of the selected UIDs, including unavailable explicit UIDs.
func (t *Targets) UIDs() []int { return append([]int{}, t.uids...) }

type gate struct {
	token chan struct{}
	refs  int
}

var gates = struct {
	sync.Mutex
	byUID map[int]*gate
}{byUID: make(map[int]*gate)}

func acquire(ctx context.Context, uid int) (func(), error) {
	gates.Lock()
	g := gates.byUID[uid]
	if g == nil {
		g = &gate{token: make(chan struct{}, 1)}
		gates.byUID[uid] = g
	}
	g.refs++
	gates.Unlock()
	drop := func() {
		gates.Lock()
		defer gates.Unlock()
		g.refs--
		if g.refs == 0 {
			delete(gates.byUID, uid)
		}
	}
	select {
	case g.token <- struct{}{}:
		return func() { <-g.token; drop() }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

// WithUID runs a complete operation under the shared per-UID gate, owning one
// bridge connection until the operation and any cleanup have finished.
func (t *Targets) WithUID(ctx context.Context, uid int, f func(Manager) error) error {
	i := sort.SearchInts(t.uids, uid)
	if i == len(t.uids) || t.uids[i] != uid {
		return fmt.Errorf("internal error: unselected user %d", uid)
	}
	if t.missing[uid] {
		return fmt.Errorf("user manager for uid %d is unavailable", uid)
	}
	release, err := acquire(ctx, uid)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	mgr, close, err := openManager(ctx, uid)
	if err != nil {
		return fmt.Errorf("user manager for uid %d is unavailable: %w", uid, err)
	}
	defer close()
	return f(mgr)
}

// Failure identifies the user and operation that could not be completed.
type Failure struct {
	UID int
	Err error
}

// Result keeps the original target set and successful UIDs alongside failures,
// so callers can update persistent preferences without rediscovering sessions.
type Result struct {
	Targets, Succeeded []int
	Failures           []Failure
}

// Err combines failures while retaining each underlying error for inspection.
func (r *Result) Err() error {
	var errs []error
	for _, f := range r.Failures {
		errs = append(errs, fmt.Errorf("uid %d: %w", f.UID, f.Err))
	}
	return errors.Join(errs...)
}

// Do fans an operation out to all selected users. A user's failure does not undo
// another user's successful operation. Results are in UID order.
func (t *Targets) Do(ctx context.Context, f func(int, Manager) error) *Result {
	r := &Result{Targets: t.UIDs()}
	errs := make([]error, len(t.uids))
	var wg sync.WaitGroup
	for i, uid := range t.uids {
		wg.Add(1)
		go func(i, uid int) {
			defer wg.Done()
			errs[i] = t.WithUID(ctx, uid, func(m Manager) error { return f(uid, m) })
		}(i, uid)
	}
	wg.Wait()
	for i, uid := range t.uids {
		if errs[i] != nil {
			r.Failures = append(r.Failures, Failure{uid, errs[i]})
		} else {
			r.Succeeded = append(r.Succeeded, uid)
		}
	}
	return r
}

// Status reads all requested units for each selected UID, under the same gate as
// control operations. A failed user's status is never mistaken for an empty list
// of disabled services.
func (t *Targets) Status(ctx context.Context, units []string) (map[int][]*systemd.UnitStatus, *Result) {
	statuses := make(map[int][]*systemd.UnitStatus)
	var mu sync.Mutex
	r := t.Do(ctx, func(uid int, m Manager) error {
		sts, err := m.Status(ctx, units)
		if err == nil {
			mu.Lock()
			statuses[uid] = sts
			mu.Unlock()
		}
		return err
	})
	return statuses, r
}

func validateUnits(units []string) error {
	for _, unit := range units {
		if !strings.HasPrefix(unit, "snap.") || strings.Contains(unit, "/") {
			return fmt.Errorf("cannot manage non-snap unit %q", unit)
		}
	}
	return nil
}

// Start starts units individually in order and cleans up partial starts on error.
// Enable/disable behavior intentionally matches the retained agent endpoint.
func Start(ctx context.Context, m Manager, units []string, enable bool) (retErr error) {
	if err := validateUnits(units); err != nil {
		return err
	}
	if len(units) == 0 {
		return nil
	}
	var started []string
	enabled := false
	defer func() {
		if retErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, unit := range started {
			retErr = errors.Join(retErr, m.Stop(cleanup, unit))
		}
		if enabled {
			retErr = errors.Join(retErr, m.Disable(cleanup, units), m.Reload(cleanup))
		}
	}()
	if enable {
		if _, err := m.Enable(ctx, units); err != nil {
			return err
		}
		enabled = true
		if err := m.Reload(ctx); err != nil {
			return err
		}
	}
	for _, unit := range units {
		if err := m.Start(ctx, unit); err != nil {
			return err
		}
		started = append(started, unit)
	}
	return nil
}

// Stop attempts each stop and disables the batch only if every stop succeeded.
func Stop(ctx context.Context, m Manager, units []string, disable bool) error {
	if err := validateUnits(units); err != nil {
		return err
	}
	if len(units) == 0 {
		return nil
	}
	var errs []error
	for _, unit := range units {
		if err := m.Stop(ctx, unit); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if disable {
		if err := m.Disable(ctx, units); err != nil {
			return err
		}
		return m.Reload(ctx)
	}
	return nil
}

// Restart preserves the stop-then-start behavior of systemd.Systemd.Restart.
func Restart(ctx context.Context, m Manager, units []string, reload bool) error {
	if err := validateUnits(units); err != nil {
		return err
	}
	var errs []error
	for _, unit := range units {
		var err error
		if reload {
			err = m.ReloadOrRestart(ctx, unit)
		} else if err = m.Stop(ctx, unit); err == nil {
			err = m.Start(ctx, unit)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
