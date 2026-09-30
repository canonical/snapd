// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package overlord

import (
	"errors"
	"fmt"
	"sync"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
)

// StateManager is implemented by types responsible for observing
// the system and manipulating it to reflect the desired state.
type StateManager interface {
	// Ensure forces a complete evaluation of the current state.
	// See StateEngine.Ensure for more details.
	Ensure() error
}

// StateStarterUp is optionally implemented by StateManager that have expensive
// initialization to perform before the main Overlord loop.
type StateStarterUp interface {
	// StartUp asks manager to perform any expensive initialization.
	StartUp() error
}

// StateWaiter is optionally implemented by StateManagers that have running
// activities that can be waited.
type StateWaiter interface {
	// Wait asks manager to wait for all running activities to
	// finish.
	Wait()
}

// StateShutDowner is optionally implemented by StateManagers that can start
// quiescing ahead of final shutdown. For example, a manager may stop accepting
// new requests and finish handling existing requests before the daemon's HTTP
// server is stopped.
//
// ShutDown is an optional optimization in the shutdown sequence. The main daemon
// shutdown flow calls ShutDown before Stop, but other callers of Overlord or
// StateEngine, such as tests, may call Stop directly. For managers implementing
// both interfaces, the activities and resources handled by ShutDown must be a
// subset of those handled by Stop; ShutDown must not be required for correct
// cleanup.
type StateShutDowner interface {
	// ShutDown asks the manager to begin quiescing.
	ShutDown()
}

// StateStopper is optionally implemented by StateManagers that have
// running activities or resources that must be cleaned up during final
// shutdown.
type StateStopper interface {
	// Stop asks the manager to terminate all activities running concurrently
	// and perform all required cleanup. It must work correctly whether or not
	// ShutDown was called first, and must not return before these activities
	// are finished.
	Stop()
}

// StateEngine controls the dispatching of state changes to state managers.
//
// Most of the actual work performed by the state engine is in fact done
// by the individual managers registered. These managers must be able to
// cope with Ensure calls in any order, coordinating among themselves
// solely via the state.
type StateEngine struct {
	state     *state.State
	stopped   bool
	startedUp bool
	shutdown  bool
	// managers in use
	mgrLock  sync.Mutex
	managers []StateManager
}

// NewStateEngine returns a new state engine.
func NewStateEngine(s *state.State) *StateEngine {
	return &StateEngine{
		state: s,
	}
}

// State returns the current system state.
func (se *StateEngine) State() *state.State {
	return se.state
}

type startupError struct {
	errs []error
}

func (e *startupError) Error() string {
	return fmt.Sprintf("state startup errors: %v", e.errs)
}

// Is returns true if errors.Is(target) evaluates to true for any of the
// underlying startup errors.
func (e *startupError) Is(target error) bool {
	for _, err := range e.errs {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// StartUp asks all managers to perform any expensive initialization. It is a noop after the first invocation.
func (se *StateEngine) StartUp() error {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.startedUp {
		return nil
	}
	se.startedUp = true
	var errs []error
	for _, m := range se.managers {
		if starterUp, ok := m.(StateStarterUp); ok {
			err := starterUp.StartUp()
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) != 0 {
		return &startupError{errs}
	}
	return nil
}

type ensureError struct {
	errs []error
}

func (e *ensureError) Error() string {
	return fmt.Sprintf("state ensure errors: %v", e.errs)
}

// Ensure asks every manager to ensure that they are doing the necessary
// work to put the current desired system state in place by calling their
// respective Ensure methods.
//
// Managers must evaluate the desired state completely when they receive
// that request, and report whether they found any critical issues. They
// must not perform long running activities during that operation, though.
// These should be performed in properly tracked changes and tasks.
func (se *StateEngine) Ensure() error {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if !se.startedUp {
		return fmt.Errorf("state engine skipped startup")
	}
	if se.stopped {
		return fmt.Errorf("state engine already stopped")
	}
	var errs []error
	for _, m := range se.managers {
		err := m.Ensure()
		if err != nil {
			logger.Noticef("state ensure error: %v", err)
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return &ensureError{errs}
	}
	return nil
}

// AddManager adds the provided manager to take part in state operations.
func (se *StateEngine) AddManager(m StateManager) {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	se.managers = append(se.managers, m)
}

// Wait waits for all managers current activities.
func (se *StateEngine) Wait() {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.stopped {
		return
	}
	for _, m := range se.managers {
		if waiter, ok := m.(StateWaiter); ok {
			waiter.Wait()
		}
	}
}

// ShutDown asks all managers implementing StateShutDowner to begin quiescing.
// This is an optional phase before Stop and is a noop after the first
// invocation.
func (se *StateEngine) ShutDown() {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.shutdown {
		return
	}
	for _, m := range se.managers {
		if shutdowner, ok := m.(StateShutDowner); ok {
			shutdowner.ShutDown()
		}
	}
	se.shutdown = true
}

// Stop asks all managers implementing StateStopper to terminate activities
// running concurrently and perform all required cleanup, whether or not
// ShutDown was called first. It is a noop after the first invocation.
func (se *StateEngine) Stop() {
	se.mgrLock.Lock()
	defer se.mgrLock.Unlock()
	if se.stopped {
		return
	}
	for _, m := range se.managers {
		if stopper, ok := m.(StateStopper); ok {
			stopper.Stop()
		}
	}
	se.stopped = true
}
