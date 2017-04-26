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

package hooktest

import "fmt"
import "sync/atomic"

// MockHandler is a mock hookstate.Handler.
type MockHandler struct {
	BeforeCalled bool
	BeforeError  bool

	DoneCalled bool
	DoneError  bool

	ErrorCalled bool
	ErrorError  bool
	Err         error

	Executed        int32
	TotalExecutions int32
}

// NewMockHandler returns a new MockHandler.
func NewMockHandler() *MockHandler {
	return &MockHandler{}
}

// Before satisfies hookstate.Handler.Before
func (h *MockHandler) Before() error {
	executed := atomic.AddInt32(&h.Executed, 1)
	if executed != 1 {
		panic(fmt.Sprintf("More than one handler executed: %d", executed))
	}
	h.BeforeCalled = true
	if h.BeforeError {
		return fmt.Errorf("Before failed at user request")
	}
	return nil
}

// Done satisfies hookstate.Handler.Done
func (h *MockHandler) Done() error {
	executed := atomic.AddInt32(&h.Executed, -1)
	if executed != 0 {
		panic(fmt.Sprintf("More than one handler executed: %d", executed))
	}
	atomic.AddInt32(&h.TotalExecutions, 1)
	h.DoneCalled = true
	if h.DoneError {
		return fmt.Errorf("Done failed at user request")
	}
	return nil
}

// Error satisfies hookstate.Handler.Error
func (h *MockHandler) Error(err error) error {
	h.Err = err
	h.ErrorCalled = true
	if h.ErrorError {
		return fmt.Errorf("Error failed at user request")
	}
	return nil
}
