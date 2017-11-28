// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package hookstate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type hijackFunc func(ctx *Context) error
type hijackKey struct{ hook, snap string }

// HookManager is responsible for the maintenance of hooks in the system state.
// It runs hooks when they're requested, assuming they're present in the given
// snap. Otherwise they're skipped with no error.
type HookManager struct {
	state      *state.State
	runner     *state.TaskRunner
	repository *repository

	contextsMutex sync.RWMutex
	contexts      map[string]*Context

	hijackedMap map[hijackKey]hijackFunc
}

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	Error(err error) error
}

// HandlerGenerator is the function signature required to register for hooks.
type HandlerGenerator func(*Context) Handler

// HookSetup is a reference to a hook within a specific snap.
type HookSetup struct {
	Snap     string        `json:"snap"`
	Revision snap.Revision `json:"revision"`
	Hook     string        `json:"hook"`
	Optional bool          `json:"optional,omitempty"`

	Timeout     time.Duration `json:"timeout,omitempty"`
	IgnoreError bool          `json:"ignore-error,omitempty"`
	TrackError  bool          `json:"track-error,omitempty"`
}

// Manager returns a new HookManager.
func Manager(s *state.State) (*HookManager, error) {
	runner := state.NewTaskRunner(s)

	// Make sure we only run 1 hook task for given snap at a time
	runner.SetBlocked(func(thisTask *state.Task, running []*state.Task) bool {
		// check if we're a hook task, probably not needed but let's take extra care
		if thisTask.Kind() != "run-hook" {
			return false
		}
		var hooksup HookSetup
		if thisTask.Get("hook-setup", &hooksup) != nil {
			return false
		}
		thisSnapName := hooksup.Snap
		// examine all hook tasks, block thisTask if we find any other hook task affecting same snap
		for _, t := range running {
			if t.Kind() != "run-hook" || t.Get("hook-setup", &hooksup) != nil {
				continue // ignore errors and continue checking remaining tasks
			}
			if hooksup.Snap == thisSnapName {
				// found hook task affecting same snap, block thisTask.
				return true
			}
		}
		return false
	})

	manager := &HookManager{
		state:       s,
		runner:      runner,
		repository:  newRepository(),
		contexts:    make(map[string]*Context),
		hijackedMap: make(map[hijackKey]hijackFunc),
	}

	runner.AddHandler("run-hook", manager.doRunHook, nil)

	setupHooks(manager)

	return manager, nil
}

// Register registers a function to create Handler values whenever hooks
// matching the provided pattern are run.
func (m *HookManager) Register(pattern *regexp.Regexp, generator HandlerGenerator) {
	m.repository.addHandlerGenerator(pattern, generator)
}

// Ensure implements StateManager.Ensure.
func (m *HookManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *HookManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *HookManager) Stop() {
	m.runner.Stop()
}

func (m *HookManager) hijacked(hookName, snapName string) hijackFunc {
	return m.hijackedMap[hijackKey{hookName, snapName}]
}

func (m *HookManager) RegisterHijack(hookName, snapName string, f hijackFunc) {
	if _, ok := m.hijackedMap[hijackKey{hookName, snapName}]; ok {
		panic(fmt.Sprintf("hook %s for snap %s already hijacked", hookName, snapName))
	}
	m.hijackedMap[hijackKey{hookName, snapName}] = f
}

func (m *HookManager) ephemeralContext(cookieID string) (context *Context, err error) {
	var contexts map[string]string
	m.state.Lock()
	defer m.state.Unlock()
	err = m.state.Get("snap-cookies", &contexts)
	if err != nil {
		return nil, fmt.Errorf("cannot get snap cookies: %v", err)
	}
	if snapName, ok := contexts[cookieID]; ok {
		// create new ephemeral cookie
		context, err = NewContext(nil, m.state, &HookSetup{Snap: snapName}, nil, cookieID)
		return context, err
	}
	return nil, fmt.Errorf("invalid snap cookie requested")
}

// Context obtains the context for the given cookie ID.
func (m *HookManager) Context(cookieID string) (*Context, error) {
	m.contextsMutex.RLock()
	defer m.contextsMutex.RUnlock()

	var err error
	context, ok := m.contexts[cookieID]
	if !ok {
		context, err = m.ephemeralContext(cookieID)
		if err != nil {
			return nil, err
		}
	}

	return context, nil
}

func hookSetup(task *state.Task) (*HookSetup, *snapstate.SnapState, error) {
	var hooksup HookSetup
	err := task.Get("hook-setup", &hooksup)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot extract hook setup from task: %s", err)
	}

	var snapst snapstate.SnapState
	err = snapstate.Get(task.State(), hooksup.Snap, &snapst)
	if err == state.ErrNoState {
		return nil, nil, fmt.Errorf("cannot find %q snap", hooksup.Snap)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot handle %q snap: %v", hooksup.Snap, err)
	}

	return &hooksup, &snapst, nil
}

// doRunHook actually runs the hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) doRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	hooksup, snapst, err := hookSetup(task)
	task.State().Unlock()
	if err != nil {
		return err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return fmt.Errorf("cannot read %q snap details: %v", hooksup.Snap, err)
	}

	mustHijack := m.hijacked(hooksup.Hook, hooksup.Snap) != nil
	hookExists := info.Hooks[hooksup.Hook] != nil
	if !mustHijack && !hookExists && !hooksup.Optional {
		return fmt.Errorf("snap %q has no %q hook", hooksup.Snap, hooksup.Hook)
	}

	context, err := NewContext(task, task.State(), hooksup, nil, "")
	if err != nil {
		return err
	}

	// Obtain a handler for this hook. The repository returns a list since it's
	// possible for regular expressions to overlap, but multiple handlers is an
	// error (as is no handler).
	handlers := m.repository.generateHandlers(context)
	handlersCount := len(handlers)
	if handlersCount == 0 {
		// Do not report error if hook handler doesn't exist as long as the hook is optional.
		// This is to avoid issues when downgrading to an old core snap that doesn't know about
		// particular hook type and a task for it exists (e.g. "post-refresh" hook).
		if hooksup.Optional {
			return nil
		}
		return fmt.Errorf("internal error: no registered handlers for hook %q", hooksup.Hook)
	}
	if handlersCount > 1 {
		return fmt.Errorf("internal error: %d handlers registered for hook %q, expected 1", handlersCount, hooksup.Hook)
	}
	context.handler = handlers[0]

	contextID := context.ID()
	m.contextsMutex.Lock()
	m.contexts[contextID] = context
	m.contextsMutex.Unlock()

	defer func() {
		m.contextsMutex.Lock()
		delete(m.contexts, contextID)
		m.contextsMutex.Unlock()
	}()

	if err = context.Handler().Before(); err != nil {
		return err
	}

	// some hooks get hijacked, e.g. the core configuration
	var output []byte
	if f := m.hijacked(hooksup.Hook, hooksup.Snap); f != nil {
		context.Lock()
		err = f(context)
		context.Unlock()
	} else if hookExists {
		output, err = runHook(context, tomb)
	}
	if err != nil {
		if hooksup.TrackError {
			trackHookError(context, output, err)
		}
		err = osutil.OutputErr(output, err)
		if hooksup.IgnoreError {
			task.State().Lock()
			task.Errorf("ignoring failure in hook %q: %v", hooksup.Hook, err)
			task.State().Unlock()
		} else {
			if handlerErr := context.Handler().Error(err); handlerErr != nil {
				return handlerErr
			}

			return fmt.Errorf("run hook %q: %v", hooksup.Hook, err)
		}
	}

	if err = context.Handler().Done(); err != nil {
		return err
	}

	context.Lock()
	defer context.Unlock()
	if err = context.Done(); err != nil {
		return err
	}

	return nil
}

func runHookImpl(c *Context, tomb *tomb.Tomb) ([]byte, error) {
	return runHookAndWait(c.SnapName(), c.SnapRevision(), c.HookName(), c.ID(), c.Timeout(), tomb)
}

var runHook = runHookImpl

// MockRunHook mocks the actual invocation of hooks for tests.
func MockRunHook(hookInvoke func(c *Context, tomb *tomb.Tomb) ([]byte, error)) (restore func()) {
	oldRunHook := runHook
	runHook = hookInvoke
	return func() {
		runHook = oldRunHook
	}
}

var osReadlink = os.Readlink

// snapCmd returns the "snap" command to run. If snapd is re-execed
// it will be the snap command from the core snap, otherwise it will
// be the system "snap" command (c.f. LP: #1668738).
func snapCmd() string {
	// sensible default, assume PATH is correct
	snapCmd := "snap"

	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v, using default snap command", err)
		return snapCmd
	}
	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		return snapCmd
	}

	// snap is running from the core snap, we know the relative
	// location of "snap" from "snapd"
	return filepath.Join(filepath.Dir(exe), "../../bin/snap")
}

var defaultHookTimeout = 10 * time.Minute

func runHookAndWait(snapName string, revision snap.Revision, hookName, hookContext string, timeout time.Duration, tomb *tomb.Tomb) ([]byte, error) {
	argv := []string{snapCmd(), "run", "--hook", hookName, "-r", revision.String(), snapName}
	if timeout == 0 {
		timeout = defaultHookTimeout
	}

	env := []string{
		// Make sure the hook has its context defined so it can
		// communicate via the REST API.
		fmt.Sprintf("SNAP_COOKIE=%s", hookContext),
		// Set SNAP_CONTEXT too for compatibility with old snapctl
		// binary when transitioning to a new core - otherwise configure
		// hook would fail during transition.
		fmt.Sprintf("SNAP_CONTEXT=%s", hookContext),
	}

	return osutil.RunAndWait(argv, env, timeout, tomb)
}

var errtrackerReport = errtracker.Report

func trackHookError(context *Context, output []byte, err error) {
	errmsg := fmt.Sprintf("hook %s in snap %q failed: %v", context.HookName(), context.SnapName(), osutil.OutputErr(output, err))
	dupSig := fmt.Sprintf("hook:%s:%s:%s\n%s", context.SnapName(), context.HookName(), err, output)
	extra := map[string]string{
		"HookName": context.HookName(),
	}
	if context.setup.IgnoreError {
		extra["IgnoreError"] = "1"
	}
	oopsid, err := errtrackerReport(context.SnapName(), errmsg, dupSig, extra)
	if err == nil {
		logger.Noticef("Reported hook failure from %q for snap %q as %s", context.HookName(), context.SnapName(), oopsid)
	} else {
		logger.Debugf("Cannot report hook failure: %s", err)
	}
}
