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

package userbus_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/usersession/userbus"
)

type relaySuite struct{}

var _ = Suite(&relaySuite{})

func pair(c *C) (*net.UnixConn, *net.UnixConn) {
	left, file, err := userbus.SocketPair()
	c.Assert(err, IsNil)
	defer file.Close()
	right, err := net.FileConn(file)
	c.Assert(err, IsNil)
	c.Assert(left.SetDeadline(time.Now().Add(5*time.Second)), IsNil)
	c.Assert(right.SetDeadline(time.Now().Add(5*time.Second)), IsNil)
	return left, right.(*net.UnixConn)
}

func wireMessage(order binary.ByteOrder, body []byte) []byte {
	data := make([]byte, 16+len(body))
	data[0] = 'l'
	if order == binary.BigEndian {
		data[0] = 'B'
	}
	data[1], data[3] = 1, 1
	order.PutUint32(data[4:8], uint32(len(body)))
	order.PutUint32(data[8:12], 1)
	copy(data[16:], body)
	return data
}

func (s *relaySuite) TestRightsStayWithTheirMessage(c *C) {
	input, relayInput := pair(c)
	defer input.Close()
	defer relayInput.Close()
	relayOutput, output := pair(c)
	defer relayOutput.Close()
	defer output.Close()
	file, err := os.CreateTemp(c.MkDir(), "fd")
	c.Assert(err, IsNil)
	defer file.Close()
	_, err = file.WriteString("passed FD")
	c.Assert(err, IsNil)
	_, err = file.Seek(0, io.SeekStart)
	c.Assert(err, IsNil)
	plain := wireMessage(binary.LittleEndian, []byte("plain"))
	withFD := wireMessage(binary.BigEndian, bytes.Repeat([]byte("body"), 17000))
	// Queue both messages before starting the relay. A naive recvmsg loop
	// would coalesce the first message with bytes bearing the second's FD.
	_, err = input.Write(plain)
	c.Assert(err, IsNil)
	_, _, err = input.WriteMsgUnix(withFD, unix.UnixRights(int(file.Fd())), nil)
	c.Assert(err, IsNil)
	c.Assert(input.CloseWrite(), IsNil)
	done := make(chan error, 1)
	go func() { done <- userbus.Relay(relayOutput, relayInput) }()
	first := make([]byte, len(plain))
	oob := make([]byte, 1024)
	n, oobn, _, _, err := output.ReadMsgUnix(first, oob)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len(plain))
	c.Check(oobn, Equals, 0)
	c.Check(first, DeepEquals, plain)
	second := make([]byte, len(withFD))
	n, oobn, _, _, err = output.ReadMsgUnix(second, oob)
	c.Assert(err, IsNil)
	c.Assert(oobn, Not(Equals), 0)
	_, err = io.ReadFull(output, second[n:])
	c.Assert(err, IsNil)
	c.Check(second, DeepEquals, withFD)
	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	c.Assert(err, IsNil)
	c.Assert(messages, HasLen, 1)
	fds, err := unix.ParseUnixRights(&messages[0])
	c.Assert(err, IsNil)
	c.Assert(fds, HasLen, 1)
	received := os.NewFile(uintptr(fds[0]), "received")
	defer received.Close()
	data, err := io.ReadAll(received)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, "passed FD")
	c.Check(errors.Is(<-done, io.EOF), Equals, true)
}

func (s *relaySuite) TestTruncatedMessageClosesDescriptors(c *C) {
	input, relayInput := pair(c)
	defer input.Close()
	defer relayInput.Close()
	relayOutput, output := pair(c)
	defer relayOutput.Close()
	defer output.Close()
	reader, writer, err := os.Pipe()
	c.Assert(err, IsNil)
	defer reader.Close()
	defer writer.Close()
	message := wireMessage(binary.LittleEndian, []byte("missing body"))
	_, _, err = input.WriteMsgUnix(message[:16], unix.UnixRights(int(writer.Fd())), nil)
	c.Assert(err, IsNil)
	writer.Close()
	input.CloseWrite()
	c.Check(errors.Is(userbus.Relay(relayOutput, relayInput), io.EOF), Equals, true)
	// EOF proves the relay closed its received copy of the pipe writer.
	c.Assert(reader.SetReadDeadline(time.Now().Add(time.Second)), IsNil)
	data, err := io.ReadAll(reader)
	c.Check(err, IsNil)
	c.Check(data, HasLen, 0)
}

func (s *relaySuite) TestOversizedMessage(c *C) {
	input, relayInput := pair(c)
	defer input.Close()
	defer relayInput.Close()
	relayOutput, output := pair(c)
	defer relayOutput.Close()
	defer output.Close()
	message := wireMessage(binary.LittleEndian, nil)
	binary.LittleEndian.PutUint32(message[4:8], ^uint32(0))
	_, err := input.Write(message)
	c.Assert(err, IsNil)
	c.Check(userbus.Relay(relayOutput, relayInput), ErrorMatches, "oversized D-Bus message")
}

func (s *relaySuite) TestDisconnectDuringForwardClosesDescriptors(c *C) {
	input, relayInput := pair(c)
	defer input.Close()
	defer relayInput.Close()
	relayOutput, output := pair(c)
	defer relayOutput.Close()
	defer output.Close()
	c.Assert(relayOutput.SetWriteBuffer(1024), IsNil)
	reader, writer, err := os.Pipe()
	c.Assert(err, IsNil)
	defer reader.Close()
	defer writer.Close()
	fd := int(writer.Fd())
	message := wireMessage(binary.LittleEndian, make([]byte, 2*1024*1024))
	sent := make(chan error, 1)
	go func() {
		n, _, err := input.WriteMsgUnix(message, unix.UnixRights(fd), nil)
		if err == nil && n < len(message) {
			_, err = input.Write(message[n:])
		}
		sent <- err
	}()
	done := make(chan error, 1)
	go func() { done <- userbus.Relay(relayOutput, relayInput) }()
	// Observe the beginning of the send, then disconnect without draining
	// the message. This forces the relay's write to fail under backpressure.
	var data [1]byte
	var control [128]byte
	_, oobn, _, _, err := output.ReadMsgUnix(data[:], control[:])
	c.Assert(err, IsNil)
	messages, err := unix.ParseSocketControlMessage(control[:oobn])
	c.Assert(err, IsNil)
	c.Assert(messages, HasLen, 1)
	fds, err := unix.ParseUnixRights(&messages[0])
	c.Assert(err, IsNil)
	for _, fd := range fds {
		unix.Close(fd)
	}
	output.Close()
	c.Assert(<-sent, IsNil)
	writer.Close()
	c.Check(<-done, NotNil)
	c.Assert(reader.SetReadDeadline(time.Now().Add(time.Second)), IsNil)
	contents, err := io.ReadAll(reader)
	c.Check(err, IsNil)
	c.Check(contents, HasLen, 0)
}
