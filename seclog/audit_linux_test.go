// -*- Mode: Go; indent-tabs-mode: t -*-
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

package seclog_test

import (
	"encoding/binary"
	"fmt"
	"slices"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seclog"
)

type AuditSuite struct{}

var _ = Suite(&AuditSuite{})

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

func (s *AuditSuite) TestBuildMessageHeaderLayout(c *C) {
	aw := &seclog.AuditWriter{}

	payload := []byte("hello")
	msg := seclog.AuditWriterBuildMessage(aw, payload)

	// Total length: 16 (header) + 5 (payload) = 21, aligned to 24.
	c.Assert(len(msg), Equals, 24)

	// nlmsghdr fields in native byte order.
	totalLen := binary.NativeEndian.Uint32(msg[0:4])
	c.Check(totalLen, Equals, uint32(21))

	msgType := binary.NativeEndian.Uint16(msg[4:6])
	c.Check(msgType, Equals, uint16(seclog.AuditTrustedApp))

	flags := binary.NativeEndian.Uint16(msg[6:8])
	c.Check(flags, Equals, uint16(syscall.NLM_F_REQUEST))

	seq := binary.NativeEndian.Uint32(msg[8:12])
	c.Check(seq, Equals, uint32(1))

	portID := binary.NativeEndian.Uint32(msg[12:16])
	c.Check(portID, Equals, uint32(0))

	// Payload follows header.
	c.Check(string(msg[seclog.NlmsghdrSize:seclog.NlmsghdrSize+5]), Equals, "hello")

	// Padding bytes after payload should be zero.
	c.Check(msg[21], Equals, byte(0))
	c.Check(msg[22], Equals, byte(0))
	c.Check(msg[23], Equals, byte(0))
}

func (s *AuditSuite) TestBuildMessagePortID(c *C) {
	aw := &seclog.AuditWriter{}
	seclog.AuditWriterSetPortID(aw, 42)

	msg := seclog.AuditWriterBuildMessage(aw, []byte("x"))

	portID := binary.NativeEndian.Uint32(msg[12:16])
	c.Check(portID, Equals, uint32(42))
}

func (s *AuditSuite) TestBuildMessageSequenceIncrements(c *C) {
	aw := &seclog.AuditWriter{}

	msg1 := seclog.AuditWriterBuildMessage(aw, []byte("a"))
	msg2 := seclog.AuditWriterBuildMessage(aw, []byte("b"))
	msg3 := seclog.AuditWriterBuildMessage(aw, []byte("c"))

	seq1 := binary.NativeEndian.Uint32(msg1[8:12])
	seq2 := binary.NativeEndian.Uint32(msg2[8:12])
	seq3 := binary.NativeEndian.Uint32(msg3[8:12])

	c.Check(seq1, Equals, uint32(1))
	c.Check(seq2, Equals, uint32(2))
	c.Check(seq3, Equals, uint32(3))
}

func (s *AuditSuite) TestBuildMessageAlignedPayload(c *C) {
	aw := &seclog.AuditWriter{}

	// Payload of exactly 4 bytes: total = 20 which is already aligned.
	msg := seclog.AuditWriterBuildMessage(aw, []byte("abcd"))
	c.Check(len(msg), Equals, 20)

	totalLen := binary.NativeEndian.Uint32(msg[0:4])
	c.Check(totalLen, Equals, uint32(20))
}

func (s *AuditSuite) TestBuildMessageEmptyPayload(c *C) {
	aw := &seclog.AuditWriter{}

	msg := seclog.AuditWriterBuildMessage(aw, []byte{})

	// 16-byte header, already aligned.
	c.Check(len(msg), Equals, 16)

	totalLen := binary.NativeEndian.Uint32(msg[0:4])
	c.Check(totalLen, Equals, uint32(16))
}

func (s *AuditSuite) TestNlmsghdrSizeConstant(c *C) {
	// nlmsghdr is: uint32 + uint16 + uint16 + uint32 + uint32 = 16
	c.Check(seclog.NlmsghdrSize, Equals, 16)
}

func (s *AuditSuite) TestAuditSinkRegistered(c *C) {
	// The init() in audit_linux.go registers SinkAudit.
	// Setup should not fail with "unknown sink" for SinkAudit.
	// We verify indirectly: if the sink were missing, Setup would
	// return "unknown sink".
	restore := seclog.MockImplementations(map[seclog.Impl]seclog.ImplFactory{})
	defer restore()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, "test", seclog.LevelInfo)
	// This should fail with "unknown implementation" (not "unknown sink"),
	// proving the audit sink is registered.
	c.Check(err, ErrorMatches, `cannot set up security logger: unknown implementation "slog"`)
}

// mockNetlinkOps records calls and returns configurable results.
type mockNetlinkOps struct {
	socketFD    int
	socketErr   error
	bindErr     error
	getsockname syscall.Sockaddr
	getsocknErr error
	sendtoData  []byte
	sendtoErr   error
	closedFDs   []int
	closeErr    error
}

func (m *mockNetlinkOps) Socket(domain, typ, proto int) (int, error) {
	return m.socketFD, m.socketErr
}

func (m *mockNetlinkOps) Bind(fd int, sa syscall.Sockaddr) error {
	return m.bindErr
}

func (m *mockNetlinkOps) Getsockname(fd int) (syscall.Sockaddr, error) {
	return m.getsockname, m.getsocknErr
}

func (m *mockNetlinkOps) Sendto(fd int, p []byte, flags int, to syscall.Sockaddr) error {
	m.sendtoData = slices.Clone(p)
	return m.sendtoErr
}

func (m *mockNetlinkOps) Close(fd int) error {
	m.closedFDs = append(m.closedFDs, fd)
	return m.closeErr
}

// Ensure mockNetlinkOps satisfies the interface.
var _ seclog.NetlinkOps = (*mockNetlinkOps)(nil)

func (s *AuditSuite) TestOpenSuccess(c *C) {
	mock := &mockNetlinkOps{
		socketFD: 42,
		getsockname: &syscall.SockaddrNetlink{
			Family: syscall.AF_NETLINK,
			Pid:    99,
		},
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	writer, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, IsNil)
	c.Assert(writer, NotNil)
}

func (s *AuditSuite) TestOpenSocketError(c *C) {
	mock := &mockNetlinkOps{
		socketErr: fmt.Errorf("permission denied"),
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	_, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, ErrorMatches, "cannot open audit socket: permission denied")
}

func (s *AuditSuite) TestOpenBindError(c *C) {
	mock := &mockNetlinkOps{
		socketFD: 10,
		bindErr:  fmt.Errorf("address in use"),
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	_, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, ErrorMatches, "cannot bind audit socket: address in use")
	// Socket should have been closed on bind failure.
	c.Check(mock.closedFDs, DeepEquals, []int{10})
}

func (s *AuditSuite) TestOpenGetsocknameError(c *C) {
	mock := &mockNetlinkOps{
		socketFD:    10,
		getsocknErr: fmt.Errorf("bad fd"),
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	_, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, ErrorMatches, "cannot get audit socket port ID: bad fd")
	c.Check(mock.closedFDs, DeepEquals, []int{10})
}

func (s *AuditSuite) TestOpenGetsocknameWrongAddressType(c *C) {
	mock := &mockNetlinkOps{
		socketFD: 10,
		// Return a non-netlink address type.
		getsockname: &syscall.SockaddrUnix{Name: "/tmp/sock"},
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	_, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, ErrorMatches, "cannot get audit socket port ID: unexpected socket address type")
	c.Check(mock.closedFDs, DeepEquals, []int{10})
}

func (s *AuditSuite) TestWriteSendtoError(c *C) {
	mock := &mockNetlinkOps{
		socketFD: 7,
		getsockname: &syscall.SockaddrNetlink{
			Family: syscall.AF_NETLINK,
			Pid:    1,
		},
		sendtoErr: fmt.Errorf("no buffer space"),
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	writer, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, IsNil)

	_, err = writer.Write([]byte("test"))
	c.Assert(err, ErrorMatches, "cannot send audit message: no buffer space")
}

func (s *AuditSuite) TestWriteSuccess(c *C) {
	mock := &mockNetlinkOps{
		socketFD: 7,
		getsockname: &syscall.SockaddrNetlink{
			Family: syscall.AF_NETLINK,
			Pid:    1,
		},
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	writer, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, IsNil)

	n, err := writer.Write([]byte("hello"))
	c.Assert(err, IsNil)
	c.Check(n, Equals, 5)
	// The mock captured the raw netlink message.
	c.Check(len(mock.sendtoData) > seclog.NlmsghdrSize, Equals, true)
}

func (s *AuditSuite) TestClose(c *C) {
	mock := &mockNetlinkOps{
		socketFD: 7,
		getsockname: &syscall.SockaddrNetlink{
			Family: syscall.AF_NETLINK,
			Pid:    1,
		},
	}
	restore := seclog.MockNetlink(mock)
	defer restore()

	writer, err := seclog.AuditSinkFactory{}.Open("test")
	c.Assert(err, IsNil)

	closer, ok := writer.(interface{ Close() error })
	c.Assert(ok, Equals, true)
	err = closer.Close()
	c.Assert(err, IsNil)
	c.Check(mock.closedFDs, DeepEquals, []int{7})
}
