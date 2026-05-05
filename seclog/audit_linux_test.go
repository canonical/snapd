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

package seclog_test

import (
	"fmt"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/seclog"
)

type AuditSuite struct{}

var _ = Suite(&AuditSuite{})

// mockSyscallOps records calls and returns configurable results.
type mockSyscallOps struct {
	socketFD   int
	socketErr  error
	sendtoData []byte
	sendtoErr  error
	closedFDs  []int
	closeErr   error
}

func (m *mockSyscallOps) Socket(domain, typ, proto int) (int, error) {
	return m.socketFD, m.socketErr
}

func (m *mockSyscallOps) Sendto(fd int, payload []byte, flags int, to syscall.Sockaddr) error {
	m.sendtoData = append([]byte(nil), payload...)
	return m.sendtoErr
}

func (m *mockSyscallOps) Close(fd int) error {
	m.closedFDs = append(m.closedFDs, fd)
	return m.closeErr
}

// Ensure mockSyscallOps satisfies the interface.
var _ seclog.SyscallOps = (*mockSyscallOps)(nil)

func (s *AuditSuite) TestOpenSuccess(c *C) {
	mock := &mockSyscallOps{
		socketFD: 42,
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	writer, err := seclog.OpenAuditWriter()
	c.Assert(err, IsNil)
	c.Assert(writer, NotNil)
}

func (s *AuditSuite) TestOpenSocketError(c *C) {
	mock := &mockSyscallOps{
		socketErr: fmt.Errorf("permission denied"),
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	_, err := seclog.OpenAuditWriter()
	c.Assert(err, ErrorMatches, "cannot open audit socket: permission denied")
}

func (s *AuditSuite) TestWriteSuccess(c *C) {
	mock := &mockSyscallOps{
		socketFD: 7,
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	writer, err := seclog.OpenAuditWriter()
	c.Assert(err, IsNil)

	n, err := writer.Write([]byte("hello"))
	c.Assert(err, IsNil)
	c.Check(n, Equals, 5)
	// The mock captured the raw netlink message.
	c.Check(len(mock.sendtoData) > syscall.SizeofNlMsghdr, Equals, true)
}

func (s *AuditSuite) TestWriteSendtoError(c *C) {
	mock := &mockSyscallOps{
		socketFD:  7,
		sendtoErr: fmt.Errorf("no buffer space"),
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	writer, err := seclog.OpenAuditWriter()
	c.Assert(err, IsNil)

	_, err = writer.Write([]byte("test"))
	c.Assert(err, ErrorMatches, "cannot send audit message: no buffer space")
}

func (s *AuditSuite) TestClose(c *C) {
	mock := &mockSyscallOps{
		socketFD: 7,
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	writer, err := seclog.OpenAuditWriter()
	c.Assert(err, IsNil)

	err = writer.Close()
	c.Assert(err, IsNil)
	c.Check(mock.closedFDs, DeepEquals, []int{7})
}

func (s *AuditSuite) TestCloseNotOpen(c *C) {
	mock := &mockSyscallOps{
		socketFD: 7,
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	writer, err := seclog.OpenAuditWriter()
	c.Assert(err, IsNil)

	err = writer.Close()
	c.Assert(err, IsNil)

	// Second close must fail.
	err = writer.Close()
	c.Assert(err, ErrorMatches, "cannot close audit writer: not open")
	// Only one fd was closed.
	c.Check(mock.closedFDs, DeepEquals, []int{7})
}

func (s *AuditSuite) TestWriteAfterClose(c *C) {
	mock := &mockSyscallOps{
		socketFD: 7,
	}
	restore := seclog.MockSyscallOps(mock)
	defer restore()

	writer, err := seclog.OpenAuditWriter()
	c.Assert(err, IsNil)

	err = writer.Close()
	c.Assert(err, IsNil)

	_, err = writer.Write([]byte("test"))
	c.Assert(err, ErrorMatches, "cannot send audit message: not open")
}

func (s *AuditSuite) TestBuildMessageHeaderLayout(c *C) {
	aw := &seclog.AuditWriter{}

	payload := []byte("hello")
	msg := seclog.AuditWriterBuildMessage(aw, payload)

	// Total length: NLMSG_SPACE(5 + 1) = align(22) = 24.
	c.Assert(len(msg), Equals, 24)

	// nlmsghdr fields in native byte order.
	totalLen := arch.Endian().Uint32(msg[0:4])
	c.Check(totalLen, Equals, uint32(24))

	msgType := arch.Endian().Uint16(msg[4:6])
	c.Check(msgType, Equals, uint16(seclog.AuditTrustedApp))

	flags := arch.Endian().Uint16(msg[6:8])
	c.Check(flags, Equals, uint16(syscall.NLM_F_REQUEST))

	seq := arch.Endian().Uint32(msg[8:12])
	c.Check(seq, Equals, uint32(1))

	portID := arch.Endian().Uint32(msg[12:16])
	c.Check(portID, Equals, uint32(0))

	// Payload follows header.
	c.Check(string(msg[syscall.SizeofNlMsghdr:syscall.SizeofNlMsghdr+5]), Equals, "hello")

	// NUL byte for kernel null-termination.
	c.Check(msg[21], Equals, byte(0))
	// Padding bytes should be zero.
	c.Check(msg[22], Equals, byte(0))
	c.Check(msg[23], Equals, byte(0))
}

func (s *AuditSuite) TestBuildMessageSequenceIncrements(c *C) {
	aw := &seclog.AuditWriter{}

	msg1 := seclog.AuditWriterBuildMessage(aw, []byte("a"))
	msg2 := seclog.AuditWriterBuildMessage(aw, []byte("b"))
	msg3 := seclog.AuditWriterBuildMessage(aw, []byte("c"))

	seq1 := arch.Endian().Uint32(msg1[8:12])
	seq2 := arch.Endian().Uint32(msg2[8:12])
	seq3 := arch.Endian().Uint32(msg3[8:12])

	c.Check(seq1, Equals, uint32(1))
	c.Check(seq2, Equals, uint32(2))
	c.Check(seq3, Equals, uint32(3))
}

func (s *AuditSuite) TestBuildMessageSequenceSkipsZero(c *C) {
	aw := &seclog.AuditWriter{}

	// Set sequence just before wraparound.
	seclog.AuditWriterSetSeq(aw, ^uint32(0)) // math.MaxUint32

	msg1 := seclog.AuditWriterBuildMessage(aw, []byte("x"))
	msg2 := seclog.AuditWriterBuildMessage(aw, []byte("y"))

	seq1 := arch.Endian().Uint32(msg1[8:12])
	seq2 := arch.Endian().Uint32(msg2[8:12])

	// Should skip 0 and go to 1, then 2.
	c.Check(seq1, Equals, uint32(1))
	c.Check(seq2, Equals, uint32(2))
}

func (s *AuditSuite) TestBuildMessageAlignedPayload(c *C) {
	aw := &seclog.AuditWriter{}

	// Payload of exactly 4 bytes: NLMSG_SPACE(4 + 1) = align(21) = 24.
	msg := seclog.AuditWriterBuildMessage(aw, []byte("abcd"))
	c.Check(len(msg), Equals, 24)

	totalLen := arch.Endian().Uint32(msg[0:4])
	c.Check(totalLen, Equals, uint32(24))
}

func (s *AuditSuite) TestBuildMessageEmptyPayload(c *C) {
	aw := &seclog.AuditWriter{}

	msg := seclog.AuditWriterBuildMessage(aw, []byte{})

	// NLMSG_SPACE(0 + 1) = align(17) = 20.
	c.Check(len(msg), Equals, 20)

	totalLen := arch.Endian().Uint32(msg[0:4])
	c.Check(totalLen, Equals, uint32(20))
}

func (s *AuditSuite) TestNlmsgAlignAlreadyAligned(c *C) {
	c.Check(seclog.NlmsgAlign(0), Equals, uint32(0))
	c.Check(seclog.NlmsgAlign(4), Equals, uint32(4))
	c.Check(seclog.NlmsgAlign(8), Equals, uint32(8))
	c.Check(seclog.NlmsgAlign(16), Equals, uint32(16))
}

func (s *AuditSuite) TestNlmsgAlignRoundsUp(c *C) {
	c.Check(seclog.NlmsgAlign(1), Equals, uint32(4))
	c.Check(seclog.NlmsgAlign(2), Equals, uint32(4))
	c.Check(seclog.NlmsgAlign(3), Equals, uint32(4))
	c.Check(seclog.NlmsgAlign(5), Equals, uint32(8))
	c.Check(seclog.NlmsgAlign(17), Equals, uint32(20))
}
