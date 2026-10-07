// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
 */

package userbus

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"golang.org/x/sys/unix"
)

// Authentication is a request/reply exchange until BEGIN, which has no reply.
// Read exactly through each CRLF so we cannot consume (and lose descriptors
// from) a binary message pipelined immediately after BEGIN.
func relayAuthentication(bus, client *net.UnixConn) error {
	for {
		line, err := authLine(client)
		if err != nil {
			return err
		}
		if _, err := bus.Write(line); err != nil {
			return err
		}
		if string(line) == "BEGIN\r\n" {
			return nil
		}
		line, err = authLine(bus)
		if err != nil {
			return err
		}
		if _, err := client.Write(line); err != nil {
			return err
		}
	}
}

func authLine(conn *net.UnixConn) ([]byte, error) {
	var b [1]byte
	line := make([]byte, 0, 128)
	for len(line) < 4096 {
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return nil, err
		}
		line = append(line, b[0])
		if n := len(line); n >= 2 && line[n-2] == '\r' && line[n-1] == '\n' {
			return line, nil
		}
	}
	return nil, fmt.Errorf("oversized D-Bus authentication line")
}

// relay forwards whole D-Bus wire messages without decoding their bodies or
// rewriting serials or FD indices. recvmsg can coalesce plain bytes preceding
// an SCM_RIGHTS-bearing sendmsg, so forwarding arbitrary chunks would move the
// descriptors into the preceding message. Restrict reads to message boundaries.
func relay(dst, src *net.UnixConn) error {
	for {
		reader := &messageReader{conn: src}
		data, err := reader.readMessage()
		if err == nil {
			err = forward(dst, data, reader.fds)
		}
		for _, fd := range reader.fds {
			unix.Close(fd)
		}
		if err != nil {
			return err
		}
	}
}

type messageReader struct {
	conn    *net.UnixConn
	fds     []int
	control [4096]byte
}

func (r *messageReader) Read(data []byte) (int, error) {
	n, oobn, flags, _, readErr := r.conn.ReadMsgUnix(data, r.control[:])
	fds, err := receivedRights(r.control[:oobn])
	r.fds = append(r.fds, fds...)
	if err != nil {
		return 0, err
	}
	if flags&unix.MSG_CTRUNC != 0 || len(r.fds) > 253 {
		return 0, fmt.Errorf("too many or truncated user bus file descriptors")
	}
	return n, readErr
}

func (r *messageReader) readMessage() ([]byte, error) {
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	var order binary.ByteOrder
	switch header[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("invalid D-Bus byte order")
	}
	if header[3] != 1 {
		return nil, fmt.Errorf("unsupported D-Bus protocol version")
	}
	bodyLen := uint64(order.Uint32(header[4:8]))
	fieldsLen := uint64(order.Uint32(header[12:16]))
	size := 16 + ((fieldsLen + 7) &^ uint64(7)) + bodyLen
	if size > 128*1024*1024 {
		return nil, fmt.Errorf("oversized D-Bus message")
	}
	data := make([]byte, int(size))
	copy(data, header[:])
	_, err := io.ReadFull(r, data[16:])
	return data, err
}

func receivedRights(control []byte) ([]int, error) {
	messages, err := unix.ParseSocketControlMessage(control)
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, message := range messages {
		// Credentials belong to each local connection; never forward them.
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			continue
		}
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			return fds, err
		}
		fds = append(fds, rights...)
		for _, fd := range rights {
			unix.CloseOnExec(fd)
		}
	}
	return fds, nil
}

func forward(dst *net.UnixConn, data []byte, fds []int) error {
	var control []byte
	if len(fds) > 0 {
		control = unix.UnixRights(fds...)
	}
	n, oobn, err := dst.WriteMsgUnix(data, control, nil)
	if err != nil {
		return err
	}
	if oobn != len(control) || n == 0 {
		return io.ErrShortWrite
	}
	if n < len(data) {
		_, err = dst.Write(data[n:])
	}
	return err
}
