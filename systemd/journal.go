// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package systemd

import (
	"fmt"
	"log/syslog"
	"net"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
)

// JournalStreamFileParams contains configuration parameters for the journal stream.
// Most of these are optional, but a stream must have a proper Identifier specified to
// be opened. If a namespace is provided, then the stream will connect to that specific
// journal namespace instead.
type JournalStreamFileParams struct {
	Namespace   string
	Identifier  string
	UnitName    string
	Priority    syslog.Priority
	LevelPrefix bool
}

// NewJournalStreamFile creates log stream file descriptor to the journal. The
// semantics is identical to that of sd_journal_stream_fd(3) call.
func NewJournalStreamFile(params JournalStreamFileParams) (*os.File, error) {
	// an identifier must be provided
	if params.Identifier == "" {
		return nil, fmt.Errorf("internal error: cannot setup a journal stream without an identifier")
	}

	var journalPath string
	if params.Namespace != "" {
		journalPath = fmt.Sprintf("%s/journal.%s/stdout", dirs.SnapSystemdRunDir, params.Namespace)
	} else {
		journalPath = fmt.Sprintf("%s/journal/stdout", dirs.SnapSystemdRunDir)
	}

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: journalPath})
	if err != nil {
		return nil, err
	}
	// does not affect *os.File created through conn.File() later on
	defer conn.Close()

	// systemd closes the read side
	// https://github.com/systemd/systemd/blob/2e8a581b9cc1132743c2341fc334461096266ad4/src/core/exec-invoke.c#L228
	if err := conn.CloseRead(); err != nil {
		return nil, err
	}

	// journald actually tries to force this into 8mb in spite of kernel
	// limits, however let us not do that.
	// https://github.com/systemd/systemd/blob/2e8a581b9cc1132743c2341fc334461096266ad4/src/core/exec-invoke.c#L231
	// intentionally ignore the error like systemd does.
	_ = conn.SetWriteBuffer(int(8 * quantity.SizeMiB))

	var levelPrefix int
	if params.LevelPrefix {
		levelPrefix = 1
	}

	// setup contents taken from the original systemd code:
	// https://github.com/systemd/systemd/blob/2e8a581b9cc1132743c2341fc334461096266ad4/src/core/exec-invoke.c#L233
	setupStr := fmt.Sprintf("%s\n%s\n%d\n%d\n%d\n%d\n%d\n",
		params.Identifier, /* syslog_identifier */
		params.UnitName,   /* unit-name */
		params.Priority,   /* syslog_priority */
		levelPrefix,       /* syslog_level_prefix */
		0,                 /* false */
		0,                 /* is_kmsg_output */
		0,                 /* is_terminal_output */
	)
	if _, err := conn.Write([]byte(setupStr)); err != nil {
		return nil, fmt.Errorf("failed to write header: %v", err)
	}
	return conn.File()
}
