// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package dbustest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/godbus/dbus"
)

// testDBusClientName is the unique name of the connected client.
const testDBusClientName = ":test"

// DBusHandlerFunc is the type of handler function for interacting with test DBus.
//
// The handler is called for each message that arrives to the bus from the test
// client. The handler can respond by returning zero or more messages.
// Typically one message is returned (method response or error). Additional
// messages can be returned to represent signals emitted during message
// handling.
//
// The handler is not called for internal messages related to DBus itself.
type DBusHandlerFunc func(msg *dbus.Message, n int) ([]*dbus.Message, error)

// testDBusStream provides io.ReadWriteCloser for dbus.NewConn.
//
// Internally the stream uses separate input and output buffers.  Input buffer
// is written to by DBus. Output buffer is written to internally, in response
// to internal interactions with DBus or in response to message handler
// function, provided by test code, returning DBus messages to send.
//
// Before authentication is completed buffers are used to operate a
// line-oriented, text protocol. After authentication the buffers exchange DBus
// messages exclusively.
//
// The authentication phase is handled internally and is not exposed to tests.
//
// Anything written to the output buffer is passed to DBus for processing.
// Anything read from input buffer is decoded and either handled internally or
// delegated to the handler function.
//
// DBus implementation we use requires Read to block while data is unavailable
// so a condition variable is used to provide blocking behavior.
type testDBusStream struct {
	handler DBusHandlerFunc

	m        sync.Mutex
	readable sync.Cond

	outputBuf, inputBuf bytes.Buffer
	closed              bool
	authDone            bool
	n                   int
}

func (s *testDBusStream) decodeRequest() {
	// s.m is locked
	if !s.authDone {
		// Before authentication is done process the text protocol anticipating
		// the TEST authentication used by NewDBusTestConn call below.
		msg := s.inputBuf.String()
		s.inputBuf.Reset()
		switch msg {
		case "\x00":
			// initial NUL byte, ignore
		case "AUTH\r\n":
			s.outputBuf.WriteString("REJECTED TEST\r\n")
		case "AUTH TEST TEST\r\n":
			s.outputBuf.WriteString("OK test://\r\n")
		case "CANCEL\r\n":
			s.outputBuf.WriteString("REJECTED\r\n")
		case "BEGIN\r\n":
			s.authDone = true
		default:
			panic(fmt.Errorf("unrecognized authentication message %q", msg))
		}
		s.readable.Signal()
		return
	}

	// After authentication the buffer must contain marshaled DBus messages.
	msgIn, err := dbus.DecodeMessage(&s.inputBuf)
	if err != nil {
		panic(fmt.Errorf("cannot decode incoming message: %v", err))
	}
	switch s.n {
	case 0:
		// The very first message we receive is a Hello message sent from
		// NewDBusTestConn below. This message, along with the NameAcquired
		// signal we send below, allow DBus to associate the client with the
		// responses sent by test.
		s.sendMsg(&dbus.Message{
			Type: dbus.TypeMethodReply,
			Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldDestination: dbus.MakeVariant(testDBusClientName),
				dbus.FieldSender:      dbus.MakeVariant("org.freedesktop.DBus"),
				dbus.FieldReplySerial: dbus.MakeVariant(msgIn.Serial()),
				dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf("")),
			},
			Body: []interface{}{":test"},
		})
		s.sendMsg(&dbus.Message{
			Type: dbus.TypeSignal,
			Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/DBus")),
				dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.DBus"),
				dbus.FieldMember:      dbus.MakeVariant("NameAcquired"),
				dbus.FieldDestination: dbus.MakeVariant(testDBusClientName),
				dbus.FieldSender:      dbus.MakeVariant("org.freedesktop.DBus"),
				dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf("")),
			},
			Body: []interface{}{testDBusClientName},
		})
	default:
		msgOutList, err := s.handler(msgIn, s.n-1)
		if err != nil {
			panic("cannot handle message: " + err.Error())
		}
		for _, msgOut := range msgOutList {
			// Test code does not need to provide the address of the sender.
			if _, ok := msgOut.Headers[dbus.FieldSender]; !ok {
				msgOut.Headers[dbus.FieldSender] = dbus.MakeVariant(testDBusClientName)
			}
			s.sendMsg(msgOut)
		}
	}
	s.n++
}

func (s *testDBusStream) sendMsg(msg *dbus.Message) {
	// TODO: handle big endian if we ever get big endian machines again.
	if err := msg.EncodeTo(&s.outputBuf, binary.LittleEndian); err != nil {
		panic(fmt.Errorf("cannot encode outgoing message: %v", err))
	}
	// s.m is locked
	s.readable.Signal()
}

func (s *testDBusStream) Read(p []byte) (n int, err error) {
	s.m.Lock()
	defer s.m.Unlock()

	// When the buffer is empty block until more data arrives. DBus
	// continuously blocks on reading and premature empty read is treated as an
	// EOF, terminating the message flow.
	if s.outputBuf.Len() == 0 {
		s.readable.Wait()
	}

	if s.closed {
		return 0, fmt.Errorf("stream is closed")
	}
	return s.outputBuf.Read(p)
}

func (s *testDBusStream) Write(p []byte) (n int, err error) {
	s.m.Lock()
	defer s.m.Unlock()

	if s.closed {
		return 0, fmt.Errorf("stream is closed")
	}

	n, err = s.inputBuf.Write(p)
	s.decodeRequest()
	return n, err
}

func (s *testDBusStream) Close() error {
	s.m.Lock()
	defer s.m.Unlock()
	s.closed = true
	s.readable.Signal()
	return nil
}

func newTestDBusStream(handler DBusHandlerFunc) *testDBusStream {
	s := &testDBusStream{handler: handler}
	s.readable.L = &s.m
	return s
}

// testAuth implements DBus authentication protocol used during testing.
type testAuth struct{}

func (a *testAuth) FirstData() (name, resp []byte, status dbus.AuthStatus) {
	return []byte("TEST"), []byte("TEST"), dbus.AuthOk
}

func (a *testAuth) HandleData(data []byte) (resp []byte, status dbus.AuthStatus) {
	return []byte(""), dbus.AuthOk
}

// Connection returns a DBus connection for writing unit tests.
//
// The handler function is called for each message sent to the bus. It can
// return any number of messages to send in response. The counter aids in
// testing a sequence of messages that is expected.
func Connection(handler DBusHandlerFunc) (*dbus.Conn, error) {
	conn, err := dbus.NewConn(newTestDBusStream(handler))
	if err != nil {
		return nil, err
	}
	if err = conn.Auth([]dbus.Auth{&testAuth{}}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err = conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
