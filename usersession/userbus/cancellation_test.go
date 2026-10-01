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
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	. "gopkg.in/check.v1"
)

func (s *connectionSuite) TestCanceledCallClosesLateReplyDescriptors(c *C) {
	// TODO: enable unconditionally once the fix is included in our godbus
	// dependency: https://github.com/godbus/dbus/pull/443.
	if os.Getenv("SNAPD_TEST_GODBUS_FD_FIX") != "1" {
		c.Skip("godbus v5.2.2 leaks descriptors in canceled calls' late replies; set SNAPD_TEST_GODBUS_FD_FIX=1 to test the upstream fix")
	}
	uid := uint32(os.Getuid())
	server := s.startBus(c, uid)
	conn := s.connect(c, uid)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reader, writer, err := os.Pipe()
	c.Assert(err, IsNil)
	defer reader.Close()
	defer writer.Close()

	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	c.Assert(conn.AddMatchSignalContext(ctx,
		dbus.WithMatchSender(server.Names()[0]),
		dbus.WithMatchInterface("io.snapcraft.Test"),
		dbus.WithMatchMember("ReplySent")), IsNil)

	// Take over dispatch so the test can release the reply only after the
	// caller has observed cancellation, and send a barrier after the reply's
	// synchronous send completes. No blocked service goroutine is needed.
	requests := make(chan *dbus.Message, 8)
	server.Eavesdrop(requests)
	defer server.Eavesdrop(nil)
	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()
	call := conn.Object(server.Names()[0], "/test").GoWithContext(callCtx, "io.snapcraft.Test.GetFD", 0, nil)
	var request *dbus.Message
	for request == nil {
		select {
		case message, ok := <-requests:
			c.Assert(ok, Equals, true, Commentf("server disconnected before receiving the call"))
			if message.Type == dbus.TypeMethodCall && message.Headers[dbus.FieldMember].Value() == "GetFD" {
				request = message
			}
		case <-ctx.Done():
			c.Fatal("server did not receive the call")
		}
	}
	cancelCall()
	select {
	case result := <-call.Done:
		c.Assert(errors.Is(result.Err, context.Canceled), Equals, true)
	case <-ctx.Done():
		c.Fatal("call did not cancel")
	}

	fd := dbus.UnixFD(writer.Fd())
	reply := &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldDestination: request.Headers[dbus.FieldSender],
			dbus.FieldReplySerial: dbus.MakeVariant(request.Serial()),
			dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(fd)),
		},
		Body: []any{fd},
	}
	c.Assert(server.Send(reply, nil).Err, IsNil)
	c.Assert(writer.Close(), IsNil)
	c.Assert(server.Emit("/test", "io.snapcraft.Test.ReplySent"), IsNil)
	barrierReceived := false
	for !barrierReceived {
		select {
		case signal, ok := <-signals:
			c.Assert(ok, Equals, true, Commentf("client disconnected before receiving the barrier"))
			barrierReceived = signal.Name == "io.snapcraft.Test.ReplySent"
		case <-ctx.Done():
			c.Fatal("client did not receive the reply barrier")
		}
	}
	// Both messages came from the same server connection. Receiving the
	// barrier means the client's input worker has already processed the late
	// reply, and the bridge has closed its temporary descriptor copies.
	c.Log("received reply barrier after releasing the canceled call's FD reply")
	checkEOF := func(stage string) {
		c.Assert(reader.SetReadDeadline(time.Now().Add(time.Second)), IsNil)
		var data [1]byte
		n, err := reader.Read(data[:])
		c.Check(n, Equals, 0, Commentf("%s", stage))
		c.Check(err, Equals, io.EOF, Commentf("%s: discarded reply must not retain the pipe writer", stage))
	}
	checkEOF("before closing the connection")
	c.Assert(conn.Close(), IsNil)
	checkEOF("after closing the connection")
}
