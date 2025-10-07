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

var Register = register
var RegisterSink = registerSink

type (
	Provider       = provider
	SecurityLogger = securityLogger
)

func MockSinks(m map[Sink]func(string) (io.Writer, error)) (restore func()) {
	restore = testutil.Backup(&sinks)
	sinks = m
	return restore
}

// MockNewSink is a convenience wrapper that replaces the journal sink factory
// in the sinks map. The rest of the sinks map is preserved.
func MockNewSink(f func(string) (io.Writer, error)) (restore func()) {
	restore = testutil.Backup(&sinks)
	sinks = map[Sink]func(string) (io.Writer, error){
		SinkJournal: f,
		SinkAudit:   newAuditSink,
	}
	return restore
}

func MockProviders(m map[Impl]provider) (restore func()) {
	restore = testutil.Backup(&providers)
	providers = m
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

var SyslogPriority = syslogPriority

var NewJournalWriter = newJournalWriter

type JournalWriter = journalWriter
