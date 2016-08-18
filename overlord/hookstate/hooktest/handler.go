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

// MockHandler is a mock hookstate.Handler.
type MockHandler struct {
	BeforeCalled bool
	BeforeError  bool

	DoneCalled bool
	DoneError  bool

	ErrorCalled bool
	ErrorError  bool
	Err         error
}

// NewMockHandler returns a new MockHandler.
func NewMockHandler() *MockHandler {
	return &MockHandler{
		BeforeCalled: false,
		BeforeError:  false,

		DoneCalled: false,
		DoneError:  false,

		ErrorCalled: false,
		ErrorError:  false,
		Err:         nil,
	}
}

// Before satisfies hookstate.Handler.Before
func (h *MockHandler) Before() error {
	h.BeforeCalled = true
	if h.BeforeError {
		return fmt.Errorf("before failed at user request")
	}
	return nil
}

// Done satisfies hookstate.Handler.Done
func (h *MockHandler) Done() error {
	h.DoneCalled = true
	if h.DoneError {
		return fmt.Errorf("done failed at user request")
	}
	return nil
}

// Error satisfies hookstate.Handler.Error
func (h *MockHandler) Error(err error) error {
	h.Err = err
	h.ErrorCalled = true
	if h.ErrorError {
		return fmt.Errorf("error failed at user request")
	}
	return nil
}
