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

package seclog

import (
	"io"

	"github.com/snapcore/snapd/testutil"
)

var NewNopLogger = newNopLogger

var RegisterImpl = registerImpl
var RegisterSink = registerSink

type (
	ImplFactory    = implFactory
	SinkFactory    = sinkFactory
	SecurityLogger = securityLogger
)

func MockSinks(m map[Sink]sinkFactory) (restore func()) {
	restore = testutil.Backup(&sinks)
	sinks = m
	return restore
}

// sinkFunc adapts a plain function to the [sinkFactory] interface.
type sinkFunc func(string) (io.Writer, error)

// SinkFunc exports sinkFunc for use in external test packages.
type SinkFunc = sinkFunc

func (f sinkFunc) Open(appID string) (io.Writer, error) { return f(appID) }

// MockNewSink is a convenience wrapper that replaces the audit sink factory
// in the sinks map.
func MockNewSink(f func(string) (io.Writer, error)) (restore func()) {
	restore = testutil.Backup(&sinks)
	sinks = map[Sink]sinkFactory{
		SinkAudit: sinkFunc(f),
	}
	return restore
}

func MockImplementations(m map[Impl]implFactory) (restore func()) {
	restore = testutil.Backup(&implementations)
	implementations = m
	return restore
}

func MockGlobalLogger(l securityLogger) (restore func()) {
	restore = testutil.Backup(&globalLogger)
	globalLogger = l
	return restore
}

func MockGlobalCloser(c io.Closer) (restore func()) {
	restore = testutil.Backup(&globalCloser)
	globalCloser = c
	return restore
}

// LoggerSetup is the exported alias for the unexported loggerSetup type,
// allowing tests to create and mock setup state.
type LoggerSetup = loggerSetup

// NewLoggerSetup constructs a LoggerSetup for use in tests.
func NewLoggerSetup(impl Impl, sink Sink, appID string, minLevel Level) *LoggerSetup {
	return &LoggerSetup{impl: impl, sink: sink, appID: appID, minLevel: minLevel}
}

func MockGlobalSetup(s *LoggerSetup) (restore func()) {
	restore = testutil.Backup(&globalSetup)
	globalSetup = s
	return restore
}

const MaxWriteFailures = maxWriteFailures

func MockWriteFailures(n int) (restore func()) {
	restore = testutil.Backup(&writeFailures)
	writeFailures = n
	return restore
}

func MockFailed(f bool) (restore func()) {
	restore = testutil.Backup(&failed)
	failed = f
	return restore
}

func GetFailed() bool {
	return failed
}

func GetWriteFailures() int {
	return writeFailures
}
