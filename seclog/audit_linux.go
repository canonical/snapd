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
	"sync/atomic"
	"syscall"

	"github.com/snapcore/snapd/arch"
)

const (
	// AUDIT_TRUSTED_APP is the audit message type for trusted application messages.
	// See https://github.com/linux-audit/audit-userspace/blob/a54613b2b6233669972d55f1f5463ae4757700be/lib/audit-records.h#L75
	auditTrustedApp = 1121
)

// syscallOps abstracts the syscall operations needed to open,
// send to, and close a netlink socket. Production code uses [realSyscallOps];
// tests can substitute a recording or stubbing implementation.
type syscallOps interface {
	Socket(domain, typ, proto int) (int, error)
	Sendto(fd int, payload []byte, flags int, to syscall.Sockaddr) error
	Close(fd int) error
}

// realSyscallOps delegates every operation to the corresponding syscall.
type realSyscallOps struct{}

func (realSyscallOps) Socket(domain, typ, proto int) (int, error) {
	return syscall.Socket(domain, typ, proto)
}

func (realSyscallOps) Sendto(fd int, payload []byte, flags int, to syscall.Sockaddr) error {
	return syscall.Sendto(fd, payload, flags, to)
}

func (realSyscallOps) Close(fd int) error {
	return syscall.Close(fd)
}

var sys syscallOps = realSyscallOps{}

// AuditWriter implements [io.WriteCloser].
// It must be created via [OpenAuditWriter]; the zero value is not usable.
type AuditWriter struct {
	fd     int
	seq    uint32
	opened bool
}

// OpenAuditWriter opens a netlink audit socket and returns an [AuditWriter]
// that sends each written payload as an AUDIT_TRUSTED_APP.
func OpenAuditWriter() (*AuditWriter, error) {
	// SOCK_CLOEXEC prevents the fd from leaking to child processes.
	fd, err := sys.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_AUDIT)
	if err != nil {
		return nil, fmt.Errorf("cannot open audit socket: %v", err)
	}
	return &AuditWriter{fd: fd, opened: true}, nil
}

// Write sends payload as an AUDIT_TRUSTED_APP netlink message.
// The returned byte count reflects only the original payload length.
// Concurrent use requires external synchronization.
func (aw *AuditWriter) Write(payload []byte) (int, error) {
	if !aw.opened {
		return 0, fmt.Errorf("cannot send audit message: not open")
	}
	msg := aw.buildMessage(payload)
	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Pid:    0, // kernel
	}
	// TODO: request and handle ACK from kernel audit subsystem
	if err := sys.Sendto(aw.fd, msg, 0, addr); err != nil {
		return 0, fmt.Errorf("cannot send audit message: %v", err)
	}
	return len(payload), nil
}

// Close closes the underlying netlink socket.
func (aw *AuditWriter) Close() error {
	if !aw.opened {
		return fmt.Errorf("cannot close audit writer: not open")
	}
	aw.opened = false
	return sys.Close(aw.fd)
}

// buildMessage constructs a raw netlink message containing the given payload.
// The header layout follows struct nlmsghdr from
// https://github.com/torvalds/linux/blob/254f49634ee16a731174d2ae34bc50bd5f45e731/include/uapi/linux/netlink.h#L45
func (aw *AuditWriter) buildMessage(payload []byte) []byte {
	// The kernel forcibly null-terminates the payload data. We include an extra
	// byte to avoid overwriting message data.
	totalLen := nlmsgAlign(syscall.SizeofNlMsghdr + uint32(len(payload)) + 1)
	buf := make([]byte, totalLen)

	// Write header in native byte order (netlink uses host endianness).
	//   [0:4]   uint32 Length of message including header
	//   [4:6]   uint16 Message content type
	//   [6:8]   uint16 Flags
	//   [8:12]  uint32 Sequence number
	//   [12:16] uint32 Sending process port ID
	// TODO: Upgrade from fire-and-forget to use NLM_F_ACK and handle
	// acknowledgments.
	arch.Endian().PutUint32(buf[0:4], totalLen)
	arch.Endian().PutUint16(buf[4:6], auditTrustedApp)
	arch.Endian().PutUint16(buf[6:8], syscall.NLM_F_REQUEST)
	arch.Endian().PutUint32(buf[8:12], aw.nextSeq())
	arch.Endian().PutUint32(buf[12:16], 0)

	// Write payload.
	copy(buf[syscall.SizeofNlMsghdr:], payload)
	return buf
}

// nlmsgAlign rounds up to the nearest 4-byte boundary per NLMSG_ALIGN.
func nlmsgAlign(size uint32) uint32 {
	return (size + 3) &^ 3
}

// nextSeq returns the next non-zero sequence number, skipping zero on
// wrap to allow unambiguous ACK matching (mirrors audit-userspace).
func (aw *AuditWriter) nextSeq() uint32 {
	s := atomic.AddUint32(&aw.seq, 1)
	if s == 0 {
		s = atomic.AddUint32(&aw.seq, 1)
	}
	return s
}
