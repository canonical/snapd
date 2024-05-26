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
	"bytes"
	"fmt"
	"log/syslog"
	"net"
	"os"

	"github.com/ddkwork/golibrary/mylog"
)

var journalStdoutPath = "/run/systemd/journal/stdout"

// NewJournalStreamFile creates log stream file descriptor to the journal. The
// semantics is identical to that of sd_journal_stream_fd(3) call.
func NewJournalStreamFile(identifier string, priority syslog.Priority, levelPrefix bool) (*os.File, error) {
	conn := mylog.Check2(net.DialUnix("unix", nil, &net.UnixAddr{Name: journalStdoutPath}))

	// does not affect *os.File created through conn.File() later on
	defer conn.Close()
	mylog.Check(conn.CloseRead())

	// header contents taken from the original systemd code:
	// https://github.com/systemd/systemd/blob/97a33b126c845327a3a19d6e66f05684823868fb/src/journal/journal-send.c#L395
	header := bytes.Buffer{}
	header.WriteString(identifier)
	header.WriteByte('\n')
	header.WriteByte('\n')
	header.WriteByte(byte('0') + byte(priority))
	header.WriteByte('\n')
	var prefix int
	if levelPrefix {
		prefix = 1
	}
	header.WriteByte(byte('0') + byte(prefix))
	header.WriteByte('\n')
	header.WriteByte('0')
	header.WriteByte('\n')
	header.WriteByte('0')
	header.WriteByte('\n')
	header.WriteByte('0')
	header.WriteByte('\n')
	mylog.Check2(conn.Write(header.Bytes()))

	return conn.File()
}
