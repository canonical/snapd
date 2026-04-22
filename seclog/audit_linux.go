// -*- Mode: Go; indent-tabs-mode: t -*-

// go1.21 is required for binary.NativeEndian which is used to serialize
// netlink headers in host byte order. NativeEndian is supported on all
// architectures snapd targets: amd64, arm, arm64, ppc64le, riscv64.
// See https://cs.opensource.google/go/go/+/refs/tags/go1.26.2:src/encoding/binary/native_endian_little.go
// The nonativeendian tag allows excluding this file on toolchains that
// lack NativeEndian support.
// See https://go.dev/doc/go1.21#encoding/binary
//go:build go1.21 && !nonativeendian

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
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"syscall"
)

const (
	// AUDIT_TRUSTED_APP is the audit message type for trusted application messages.
	// See https://github.com/linux-audit/audit-userspace/blob/master/lib/audit-records.h
	auditTrustedApp = 1121

	// NETLINK_AUDIT is the netlink protocol for audit.
	// See https://github.com/torvalds/linux/blob/master/include/uapi/linux/netlink.h
	netlinkAudit = 9
)

// netlinkOps abstracts the syscall operations needed to open, bind, query,
// send to, and close a netlink socket. Production code uses [realNetlinkOps];
// tests can substitute a recording or stubbing implementation.
type netlinkOps interface {
	Socket(domain, typ, proto int) (int, error)
	Bind(fd int, sa syscall.Sockaddr) error
	Getsockname(fd int) (syscall.Sockaddr, error)
	Sendto(fd int, p []byte, flags int, to syscall.Sockaddr) error
	Close(fd int) error
}

// realNetlinkOps delegates every operation to the corresponding syscall.
type realNetlinkOps struct{}

func (realNetlinkOps) Socket(domain, typ, proto int) (int, error) {
	return syscall.Socket(domain, typ, proto)
}

func (realNetlinkOps) Bind(fd int, sa syscall.Sockaddr) error {
	return syscall.Bind(fd, sa)
}

func (realNetlinkOps) Getsockname(fd int) (syscall.Sockaddr, error) {
	return syscall.Getsockname(fd)
}

func (realNetlinkOps) Sendto(fd int, p []byte, flags int, to syscall.Sockaddr) error {
	return syscall.Sendto(fd, p, flags, to)
}

func (realNetlinkOps) Close(fd int) error {
	return syscall.Close(fd)
}

var netlink netlinkOps = realNetlinkOps{}

func init() {
	registerSink(SinkAudit, auditSinkFactory{})
}

// auditSinkFactory implements [sinkFactory] for the kernel audit sink.
type auditSinkFactory struct{}

// Ensure [auditSinkFactory] implements [sinkFactory].
var _ sinkFactory = auditSinkFactory{}

// Open opens a netlink audit socket and returns an [auditWriter]
// that sends each written payload as an AUDIT_TRUSTED_APP. The appID is
// currently unused but accepted for sink factory compatibility.
func (auditSinkFactory) Open(_ string) (io.Writer, error) {
	// SOCK_CLOEXEC prevents the fd from leaking to child processes.
	fd, err := netlink.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, netlinkAudit)
	if err != nil {
		return nil, fmt.Errorf("cannot open audit socket: %w", err)
	}
	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Pid:    0, // let kernel assign port ID
		Groups: 0,
	}
	if err := netlink.Bind(fd, addr); err != nil {
		netlink.Close(fd)
		return nil, fmt.Errorf("cannot bind audit socket: %w", err)
	}
	portID, err := getPortID(fd)
	if err != nil {
		netlink.Close(fd)
		return nil, fmt.Errorf("cannot get audit socket port ID: %w", err)
	}
	return &auditWriter{fd: fd, portID: portID}, nil
}

// getPortID returns the kernel-assigned port ID of the netlink socket.
// When binding with Pid 0, the kernel assigns a unique port ID that may
// or may not equal the process PID. This value must be used in outgoing
// netlink message headers.
func getPortID(fd int) (uint32, error) {
	sa, err := netlink.Getsockname(fd)
	if err != nil {
		return 0, err
	}
	addr, ok := sa.(*syscall.SockaddrNetlink)
	if !ok {
		return 0, errors.New("unexpected socket address type")
	}
	return addr.Pid, nil
}

// auditWriter sends messages to the kernel audit subsystem via a netlink
// socket. Each Write call sends the payload as an AUDIT_TRUSTED_APP.
//
// The writer is safe for sequential use; concurrent use requires external
// synchronization.
type auditWriter struct {
	fd     int
	portID uint32
	seq    atomic.Uint32
}

// Write sends p as the payload of an AUDIT_TRUSTED_APP netlink message.
// The returned byte count reflects only the original payload length.
func (aw *auditWriter) Write(payload []byte) (int, error) {
	msg := aw.buildMessage(payload)
	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Pid:    0, // kernel
	}
	if err := netlink.Sendto(aw.fd, msg, 0, addr); err != nil {
		return 0, fmt.Errorf("cannot send audit message: %w", err)
	}
	return len(payload), nil
}

// Close closes the underlying netlink socket.
func (aw *auditWriter) Close() error {
	return netlink.Close(aw.fd)
}

// nlmsghdrSize is the size of a netlink message header in bytes
// (uint32 + uint16 + uint16 + uint32 + uint32 = 16).
const nlmsghdrSize = 16

// buildMessage constructs a raw netlink AUDIT_TRUSTED_APP containing payload.
// The header layout follows struct nlmsghdr from
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/netlink.h#L45
func (aw *auditWriter) buildMessage(payload []byte) []byte {
	totalLen := nlmsghdrSize + uint32(len(payload))
	buf := make([]byte, nlmsgAlign(totalLen))

	// Write header in native byte order (netlink uses host endianness).
	// NativeEndian is supported on all architectures snapd targets:
	// amd64, arm, arm64, ppc64le, riscv64.
	// See https://cs.opensource.google/go/go/+/refs/tags/go1.26.2:src/encoding/binary/native_endian_little.go
	binary.NativeEndian.PutUint32(buf[0:4], totalLen)
	binary.NativeEndian.PutUint16(buf[4:6], auditTrustedApp)
	binary.NativeEndian.PutUint16(buf[6:8], syscall.NLM_F_REQUEST) // fire-and-forget, no ACK
	binary.NativeEndian.PutUint32(buf[8:12], aw.seq.Add(1))
	binary.NativeEndian.PutUint32(buf[12:16], aw.portID)

	// Write payload.
	copy(buf[nlmsghdrSize:], payload)
	return buf
}

// nlmsgAlign rounds up to the nearest 4-byte boundary per NLMSG_ALIGN.
func nlmsgAlign(size uint32) uint32 {
	return (size + 3) &^ 3
}
