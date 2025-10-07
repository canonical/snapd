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
	"encoding/binary"
	"fmt"
	"io"
	"sync/atomic"
	"syscall"
)

const (
	// AUDIT_USER_MSG is the audit message type for user-space messages.
	auditUserMsg = 1112

	// NETLINK_AUDIT is the netlink protocol for audit.
	netlinkAudit = 15
)

func init() {
	registerSink(SinkAudit, newAuditSink)
}

// newAuditSink opens a netlink audit socket and returns an [auditWriter]
// that sends each written payload as an AUDIT_USER_MSG. The appID is
// currently unused but accepted for sink signature compatibility.
func newAuditSink(_ string) (io.Writer, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, netlinkAudit)
	if err != nil {
		return nil, fmt.Errorf("cannot open audit socket: %w", err)
	}
	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Pid:    0, // kernel
		Groups: 0,
	}
	if err := syscall.Bind(fd, addr); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("cannot bind audit socket: %w", err)
	}
	return &auditWriter{fd: fd}, nil
}

// auditWriter sends messages to the kernel audit subsystem via a netlink
// socket. Each Write call sends the payload as an AUDIT_USER_MSG.
//
// The writer is safe for sequential use; concurrent use requires external
// synchronization.
type auditWriter struct {
	fd  int
	seq atomic.Uint32
}

// Write sends p as the payload of an AUDIT_USER_MSG netlink message.
// The returned byte count reflects only the original payload length.
func (aw *auditWriter) Write(p []byte) (int, error) {
	msg := aw.buildMessage(p)
	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Pid:    0, // kernel
	}
	if err := syscall.Sendto(aw.fd, msg, 0, addr); err != nil {
		return 0, fmt.Errorf("cannot send audit message: %w", err)
	}
	return len(p), nil
}

// Close closes the underlying netlink socket.
func (aw *auditWriter) Close() error {
	return syscall.Close(aw.fd)
}

// nlmsghdrSize is the size of a netlink message header in bytes
// (uint32 + uint16 + uint16 + uint32 + uint32 = 16).
const nlmsghdrSize = 16

// buildMessage constructs a raw netlink AUDIT_USER_MSG containing payload.
func (aw *auditWriter) buildMessage(payload []byte) []byte {
	totalLen := nlmsghdrSize + uint32(len(payload))
	buf := make([]byte, nlmsgAlign(totalLen))

	// Write header.
	binary.LittleEndian.PutUint32(buf[0:4], totalLen)
	binary.LittleEndian.PutUint16(buf[4:6], auditUserMsg)
	binary.LittleEndian.PutUint16(buf[6:8], 0x01|0x04) // NLM_F_REQUEST | NLM_F_ACK
	binary.LittleEndian.PutUint32(buf[8:12], aw.seq.Add(1))
	binary.LittleEndian.PutUint32(buf[12:16], 0) // pid 0 = kernel

	// Write payload.
	copy(buf[nlmsghdrSize:], payload)
	return buf
}

// nlmsgAlign rounds up to the nearest 4-byte boundary per NLMSG_ALIGN.
func nlmsgAlign(n uint32) uint32 {
	return (n + 3) &^ 3
}
