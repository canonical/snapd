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
	"fmt"
	"io"
	"log/syslog"
	"os"

	"github.com/snapcore/snapd/systemd"
)

const securityNamespace = "snapd-security"

func init() {
	registerSink(SinkJournal, newJournalSink)
}

// newJournalSink opens a journald stream for the "snapd-security" namespace
// and returns a [journalWriter] that prepends syslog priority prefixes to every
// written line. The resulting writer is suitable as the output sink for a
// structured security logger.
func newJournalSink(appID string) (io.Writer, error) {
	f, err := newJournalStream(appID)
	if err != nil {
		return nil, err
	}
	return newJournalWriter(f), nil
}

// journalWriter implements [levelWriter] by wrapping an [io.Writer] and
// prepending a syslog-style "<N>" priority prefix to each Write call. When
// used with a journald stream opened in level-prefix mode, the prefix
// overrides the per-message PRIORITY field. journald strips the prefix from
// the stored MESSAGE content.
//
// SetLevel must be called before each Write to select the priority for the
// upcoming message. Concurrent use of SetLevel and Write requires external
// synchronization.
type journalWriter struct {
	w     io.Writer
	level Level
}

// Ensure [journalWriter] implements [levelWriter].
var _ levelWriter = (*journalWriter)(nil)

// newJournalWriter returns a [journalWriter] that writes to the given
// writer with per-message syslog priority prefixes.
func newJournalWriter(w io.Writer) *journalWriter {
	return &journalWriter{w: w, level: LevelInfo}
}

// SetLevel sets the syslog priority for the next Write call.
func (jw *journalWriter) SetLevel(level Level) {
	jw.level = level
}

// Close closes the underlying writer if it implements [io.Closer].
func (jw *journalWriter) Close() error {
	if closer, ok := jw.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Write prepends "<N>" to p and writes the result to the underlying writer.
// The returned byte count reflects only the original payload, excluding the
// prefix, to satisfy [io.Writer] callers that compare n against len(p).
func (jw *journalWriter) Write(p []byte) (int, error) {
	prefix := fmt.Sprintf("<%d>", syslogPriority(jw.level))
	buf := make([]byte, len(prefix)+len(p))
	copy(buf, prefix)
	copy(buf[len(prefix):], p)
	n, err := jw.w.Write(buf)
	// Report bytes written minus the prefix length so that
	// callers see n == len(p) on success.
	if n >= len(prefix) {
		return n - len(prefix), err
	}
	return 0, err
}

// newJournalStream opens a journald stream connection to the
// "snapd-security" namespace. The stream uses level-prefix mode so that
// each written line can override PRIORITY per message by prepending a
// "<N>" syslog priority prefix. journald strips the prefix from the
// stored MESSAGE.
//
// The returned *os.File is suitable as the underlying writer for a
// [journalWriter].
func newJournalStream(appID string) (*os.File, error) {
	return systemd.NewJournalStreamFile(systemd.JournalStreamFileParams{
		Namespace:   securityNamespace,
		Identifier:  appID,
		Priority:    syslog.LOG_DEBUG,
		LevelPrefix: true,
	})
}

// syslogPriority maps a security log [Level] to the equivalent syslog
// priority used by journald for the PRIORITY field.
func syslogPriority(level Level) syslog.Priority {
	switch {
	case level >= LevelCritical:
		return syslog.LOG_CRIT
	case level >= LevelError:
		return syslog.LOG_ERR
	case level >= LevelWarn:
		return syslog.LOG_WARNING
	case level >= LevelInfo:
		return syslog.LOG_INFO
	default:
		return syslog.LOG_DEBUG
	}
}
