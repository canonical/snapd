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

// mockLogger implements seclog.SecurityLogger and writes event
// names plus key identifying data to a buffer. This lets tests verify
// that the right events are emitted without depending on slog or JSON.
type mockLogger struct {
	buf *bytes.Buffer
}

// Ensure mockLogger implements seclog.SecurityLogger.
var _ seclog.SecurityLogger = (*mockLogger)(nil)

// MockSecurityLogger returns a [seclog.SecurityLogger] that writes to the
// given buffer.
func MockSecurityLogger(buf *bytes.Buffer) seclog.SecurityLogger {
	return &mockLogger{buf: buf}
}

// LogAny implements [seclog.SecurityLogger.LogAny].
func (m *mockLogger) LogAny(event seclog.Event, description string, attrs ...seclog.Attr) {
	fmt.Fprintf(m.buf, "%s %s", event.Name, description)
	for _, a := range attrs {
		fmt.Fprintf(m.buf, " [%s=%#v]", a.Key, a.Value)
	}
	fmt.Fprintln(m.buf)
}

// MockSlogLogger returns a buffer and a constructor function matching the
// seclog.NewSlogLogger signature. The constructor ignores its arguments and
// returns a mockLogger backed by the buffer. This is intended for
// mocking the newSlogLogger variable in tests.
func MockSlogLogger() (*bytes.Buffer, func(io.Writer, string, seclog.Level) seclog.SecurityLogger) {
	buf := &bytes.Buffer{}
	fn := func(io.Writer, string, seclog.Level) seclog.SecurityLogger {
		return MockSecurityLogger(buf)
	}
	return buf, fn
}
