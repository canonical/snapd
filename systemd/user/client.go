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

// Package user implements the public systemd manager D-Bus protocol. Connections
// and the selection of the user whose manager is addressed belong to the caller.
package user

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

const (
	managerName       = "org.freedesktop.systemd1"
	managerPath       = dbus.ObjectPath("/org/freedesktop/systemd1")
	managerInterface  = managerName + ".Manager"
	maxPendingSignals = 1024
)

type jobResult struct {
	path   dbus.ObjectPath
	result string
}

type pendingJob struct {
	unit  string
	path  dbus.ObjectPath
	early []jobResult
	done  chan string
}

// Client addresses one generation of a systemd manager. Call Close before
// closing the underlying connection. Methods may be called concurrently, but
// compound service operations must also be serialized by the connection owner.
type Client struct {
	conn     *dbus.Conn
	owner    string
	ctx      context.Context
	cancel   context.CancelFunc
	signals  chan *dbus.Signal
	done     chan struct{}
	overflow <-chan struct{}
	jobs     chan struct{}
	mu       sync.Mutex
	pending  *pendingJob
	err      error
}

// New subscribes to job completion and manager-owner changes. It never activates
// a manager: the public bus name must already have an owner. ctx bounds setup;
// subsequent method contexts are independent of it.
func New(ctx context.Context, conn *dbus.Conn, signals *SignalHandler) (*Client, error) {
	lifetime, cancel := context.WithCancel(conn.Context())
	c := &Client{conn: conn, ctx: lifetime, cancel: cancel, signals: make(chan *dbus.Signal, maxPendingSignals), done: make(chan struct{}), jobs: make(chan struct{}, 1)}
	if signals != nil {
		c.overflow = signals.overflow
	}
	conn.Signal(c.signals)
	go c.watch()
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchObjectPath("/org/freedesktop/DBus"), dbus.WithMatchArg(0, managerName)); err != nil {
		return nil, err
	}
	var owner string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, managerName).Store(&owner); err != nil {
		return nil, fmt.Errorf("cannot find running user manager: %w", err)
	}
	c.mu.Lock()
	c.owner = owner
	c.mu.Unlock()
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender(owner), dbus.WithMatchInterface(managerInterface), dbus.WithMatchMember("JobRemoved"), dbus.WithMatchObjectPath(managerPath)); err != nil {
		return nil, err
	}
	if err := c.call(ctx, managerPath, managerInterface+".Subscribe").Err; err != nil {
		return nil, err
	}
	// Close the race between the first GetNameOwner and installing subscriptions.
	var current string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, managerName).Store(&current); err != nil {
		return nil, err
	}
	if current != owner {
		return nil, fmt.Errorf("user manager changed during connection setup")
	}
	ok = true
	return c, nil
}

// Close stops the signal watcher. The caller still owns the D-Bus connection,
// which must be closed to release the manager subscription and match rules.
func (c *Client) Close() {
	c.cancel()
	<-c.done
}

func (c *Client) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return fmt.Errorf("user manager connection closed")
}

func (c *Client) watch() {
	defer close(c.done)
	defer c.conn.RemoveSignal(c.signals)
	for {
		select {
		case <-c.overflow:
			c.mu.Lock()
			c.err = fmt.Errorf("user manager signal queue overflow")
			c.mu.Unlock()
			c.cancel()
			return
		case <-c.ctx.Done():
			return
		case signal, ok := <-c.signals:
			if !ok {
				c.cancel()
				return
			}
			c.mu.Lock()
			c.handleSignal(signal)
			c.mu.Unlock()
		}
	}
}

// handleSignal never blocks the receiver, including on unrelated jobs. Early
// completions are retained only for the current submission and have a hard cap.
func (c *Client) handleSignal(s *dbus.Signal) {
	if s.Name == "org.freedesktop.DBus.NameOwnerChanged" && s.Sender == "org.freedesktop.DBus" && s.Path == "/org/freedesktop/DBus" {
		var name, old, next string
		if dbus.Store(s.Body, &name, &old, &next) == nil && name == managerName && c.owner != "" && next != c.owner {
			c.err = fmt.Errorf("user manager owner changed")
			c.cancel()
		}
		return
	}
	p := c.pending
	if p == nil || s.Sender != c.owner || s.Path != managerPath || s.Name != managerInterface+".JobRemoved" {
		return
	}
	var id uint32
	var path dbus.ObjectPath
	var unit, result string
	if dbus.Store(s.Body, &id, &path, &unit, &result) != nil || !path.IsValid() || unit == "" {
		return
	}
	// JobRemoved names the canonical unit, even when the request used an
	// alias. Correlate by job path, not by the requested unit name.
	if p.path == "" {
		if len(p.early) == maxPendingSignals {
			c.err = fmt.Errorf("too many early user manager job completions")
			c.cancel()
			return
		}
		p.early = append(p.early, jobResult{path, result})
	} else if p.path == path {
		select {
		case p.done <- result:
		default:
		}
	}
}

func (c *Client) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-c.ctx.Done():
			cancel()
		case <-callCtx.Done():
		}
	}()
	if c.ctx.Err() != nil {
		return &dbus.Call{Err: c.failure()}
	}
	call := c.conn.Object(c.owner, path).CallWithContext(callCtx, method, 0, args...)
	if c.ctx.Err() != nil {
		call.Err = c.failure()
	}
	return call
}

func (c *Client) runJob(ctx context.Context, method, unit string) error {
	select {
	case c.jobs <- struct{}{}:
		defer func() { <-c.jobs }()
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.failure()
	}
	p := &pendingJob{unit: unit, done: make(chan string, 1)}
	c.mu.Lock()
	c.pending = p
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending = nil
		c.mu.Unlock()
	}()
	var path dbus.ObjectPath
	if err := c.call(ctx, managerPath, managerInterface+"."+method, unit, "replace").Store(&path); err != nil {
		return fmt.Errorf("cannot %s %q: %w", method, unit, err)
	}
	if !path.IsValid() || path == "/" {
		return fmt.Errorf("invalid user manager job path %q", path)
	}
	c.mu.Lock()
	p.path = path
	for _, early := range p.early {
		if early.path == path {
			p.done <- early.result
			break
		}
	}
	p.early = nil
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.failure()
	case result := <-p.done:
		select {
		case <-c.overflow:
			return fmt.Errorf("user manager signal queue overflow")
		default:
		}
		if c.ctx.Err() != nil {
			return c.failure()
		}
		// systemctl also treats a skipped job (for example, an unmet unit
		// condition) as successful, rather than as a startup failure.
		if result != "done" && result != "skipped" {
			return fmt.Errorf("cannot %s %q: job result %s", method, unit, result)
		}
		return nil
	}
}

// Start waits for the unit's start job to complete.
func (c *Client) Start(ctx context.Context, unit string) error {
	return c.runJob(ctx, "StartUnit", unit)
}

// Stop waits for the unit's stop job to complete.
func (c *Client) Stop(ctx context.Context, unit string) error { return c.runJob(ctx, "StopUnit", unit) }

// ReloadOrRestart reloads the unit if possible and otherwise restarts it.
func (c *Client) ReloadOrRestart(ctx context.Context, unit string) error {
	return c.runJob(ctx, "ReloadOrRestartUnit", unit)
}

// Reload waits for the manager's deferred reply, sent after the reload finishes.
func (c *Client) Reload(ctx context.Context) error {
	return c.call(ctx, managerPath, managerInterface+".Reload").Err
}

// UnitFileChange describes a persistent unit-file change reported by systemd.
type UnitFileChange struct{ Type, Path, Source string }

// Enable persistently enables units without implicitly reloading the manager.
func (c *Client) Enable(ctx context.Context, units []string) ([]UnitFileChange, error) {
	var carriesInstallInfo bool
	var changes []UnitFileChange
	err := c.call(ctx, managerPath, managerInterface+".EnableUnitFiles", units, false, false).Store(&carriesInstallInfo, &changes)
	return changes, err
}

// Disable persistently disables units without implicitly reloading the manager.
func (c *Client) Disable(ctx context.Context, units []string) error {
	return c.call(ctx, managerPath, managerInterface+".DisableUnitFiles", units, false).Err
}

// Status preserves the systemctl-backed UnitStatus interpretation and requested
// ordering. LoadUnit also handles installed units not currently loaded; it does
// not start them.
func (c *Client) Status(ctx context.Context, units []string) ([]*systemd.UnitStatus, error) {
	statuses := make([]*systemd.UnitStatus, 0, len(units))
	for _, unit := range units {
		var path dbus.ObjectPath
		if err := c.call(ctx, managerPath, managerInterface+".LoadUnit", unit).Store(&path); err != nil {
			if e, ok := err.(dbus.Error); ok && e.Name == managerName+".NoSuchUnit" {
				statuses = append(statuses, &systemd.UnitStatus{Name: unit, Id: unit, Names: []string{unit}})
				continue
			}
			return nil, fmt.Errorf("cannot load unit %q for status: %w", unit, err)
		}
		var props map[string]dbus.Variant
		if err := c.call(ctx, path, "org.freedesktop.DBus.Properties.GetAll", managerName+".Unit").Store(&props); err != nil {
			return nil, err
		}
		var id, active, fileState string
		var names []string
		var reload bool
		for key, dst := range map[string]any{"Id": &id, "Names": &names, "ActiveState": &active, "UnitFileState": &fileState, "NeedDaemonReload": &reload} {
			if err := props[key].Store(dst); err != nil {
				return nil, fmt.Errorf("cannot read %s of unit %q: %w", key, unit, err)
			}
		}
		if unit != id && !strutil.ListContains(names, unit) {
			return nil, fmt.Errorf("cannot get unit status: queried status of %q but got status of %q", unit, id)
		}
		st := &systemd.UnitStatus{Name: unit, Id: id, Names: names, Active: active == "active" || active == "reloading", Enabled: fileState == "enabled" || fileState == "static", Installed: fileState != ""}
		if filepath.Ext(unit) == ".service" {
			var typ dbus.Variant
			if err := c.call(ctx, path, "org.freedesktop.DBus.Properties.Get", managerName+".Service", "Type").Store(&typ); err != nil {
				return nil, err
			}
			if err := typ.Store(&st.Daemon); err != nil {
				return nil, err
			}
			st.NeedDaemonReload = reload
		}
		statuses = append(statuses, st)
	}
	return statuses, nil
}
