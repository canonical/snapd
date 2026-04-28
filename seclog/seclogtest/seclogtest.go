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
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Package seclogtest provides helpers for testing code that emits
// security audit events via the seclog package.
package seclogtest

import (
	"bytes"
	"fmt"
	"io"

	"github.com/snapcore/snapd/seclog"
)

// MockSecurityLogger implements seclog.SecurityLogger and writes event
// names plus key identifying data to a buffer. This lets tests verify
// that the right events are emitted without depending on slog or JSON.
type MockSecurityLogger struct {
	buf *bytes.Buffer
}

// Ensure MockSecurityLogger implements seclog.SecurityLogger.
var _ seclog.SecurityLogger = (*MockSecurityLogger)(nil)

// NewMockSecurityLogger returns a MockSecurityLogger that writes to the
// given buffer.
func NewMockSecurityLogger(buf *bytes.Buffer) *MockSecurityLogger {
	return &MockSecurityLogger{buf: buf}
}

// LogAny implements [seclog.SecurityLogger.LogAny].
func (m *MockSecurityLogger) LogAny(event seclog.Event, description string, attrs ...seclog.Attr) {
	fmt.Fprintf(m.buf, "%s %s", event.Name, description)
	for _, a := range attrs {
		fmt.Fprintf(m.buf, " [%s=%#v]", a.Key, a.Value)
	}
	fmt.Fprintln(m.buf)
}

// NewMockSlogLogger returns a buffer and a constructor function matching the
// seclog.NewSlogLogger signature. The constructor ignores its arguments and
// returns a MockSecurityLogger backed by the buffer. This is intended for
// mocking the newSlogLogger variable in tests.
func NewMockSlogLogger() (*bytes.Buffer, func(io.Writer, string, seclog.Level) seclog.SecurityLogger) {
	buf := &bytes.Buffer{}
	fn := func(_ io.Writer, _ string, _ seclog.Level) seclog.SecurityLogger {
		return NewMockSecurityLogger(buf)
	}
	return buf, fn
}
