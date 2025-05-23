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

type JournalStreamFileOptions struct {
	Namespace   string
	Identifier  string
	UnitName    string
	Priority    syslog.Priority
	LevelPrefix bool
}

// NewJournalStreamFile creates log stream file descriptor to the journal. The
// semantics is identical to that of sd_journal_stream_fd(3) call.
func NewJournalStreamFile(opts *JournalStreamFileOptions) (*os.File, error) {
	var journalPath string
	if opts.Namespace != "" {
		journalPath = fmt.Sprintf("%s/journal.%s/stdout", dirs.SnapSystemdRunDir, opts.Namespace)
	} else {
		journalPath = fmt.Sprintf("%s/journal/stdout", dirs.SnapSystemdRunDir)
	}

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: journalPath})
	if err != nil {
		return nil, err
	}
	// does not affect *os.File created through conn.File() later on
	defer conn.Close()

	if err := conn.CloseRead(); err != nil {
		return nil, err
	}

	// journald actually tries to force this into 8mb in spite of kernel
	// limits, however let us not do that.
	if err := conn.SetWriteBuffer(int(8 * quantity.SizeMiB)); err != nil {
		return nil, err
	}

	var levelPrefix int
	if opts.LevelPrefix {
		levelPrefix = 1
	}

	// header contents taken from the original systemd code:
	// https://github.com/systemd/systemd/blob/97a33b126c845327a3a19d6e66f05684823868fb/src/journal/journal-send.c#L395
	setupStr := fmt.Sprintf("%s\n%s\n%d\n%d\n%d\n%d\n%d\n",
		opts.Identifier, /* syslog_identifier */
		opts.UnitName,   /* unit-name */
		opts.Priority,   /* syslog_priority */
		levelPrefix,     /* syslog_level_prefix */
		0,               /* false */
		0,               /* is_kmsg_output */
		0,               /* is_terminal_output */
	)
	if _, err := conn.Write([]byte(setupStr)); err != nil {
		return nil, fmt.Errorf("failed to write header: %v", err)
	}
	return conn.File()
}
