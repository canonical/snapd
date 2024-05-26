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
	"sync/atomic"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
)

// testDBusClientName is the unique name of the connected client.
const testDBusClientName = ":test"

// DBusHandlerFunc is the type of handler function for interacting with test DBus.
//
// The handler is called for each message that arrives to the bus from the test
// client. The handler can respond by returning zero or more messages. Typically
// one message is returned (method response or error). Additional messages can
// be returned to represent signals emitted during message handling. The counter
// n aids in testing a sequence of messages that is expected.
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

	outputBuf bytes.Buffer

	output       chan []byte
	closeRequest chan struct{}

	closed   atomic.Value
	authDone bool
	n        int
}

func (s *testDBusStream) decodeRequest(req []byte) {
	buf := bytes.NewBuffer(req)
	if !s.authDone {
		// Before authentication is done process the text protocol anticipating
		// the TEST authentication used by NewDBusTestConn call below.
		msg := buf.String()
		switch msg {
		case "\x00":
			// initial NUL byte, ignore
		case "AUTH\r\n":
			s.output <- []byte("REJECTED TEST\r\n")
			// XXX: this "case" can get removed once we moved to a newer version of go-dbus
		case "AUTH TEST TEST\r\n":
			s.output <- []byte("OK test://\r\n")
		case "AUTH TEST\r\n":
			s.output <- []byte("OK test://\r\n")
		case "CANCEL\r\n":
			s.output <- []byte("REJECTED\r\n")
		case "BEGIN\r\n":
			s.authDone = true
		default:
			panic(fmt.Errorf("unrecognized authentication message %q", msg))
		}
		return
	}

	// After authentication the buffer must contain marshaled DBus messages.
	msgIn := mylog.Check2(dbus.DecodeMessage(buf))

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
		msgOutList := mylog.Check2(s.handler(msgIn, s.n-1))

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
	var buf bytes.Buffer
	mylog.Check(msg.EncodeTo(&buf, binary.LittleEndian))

	s.output <- buf.Bytes()
}

func (s *testDBusStream) Read(p []byte) (n int, err error) {
	for {
		// When the buffer is empty block until more data arrives. DBus
		// continuously blocks on reading and premature empty read is treated as an
		// EOF, terminating the message flow.
		if s.closed.Load().(bool) {
			return 0, fmt.Errorf("stream is closed")
		}
		if s.outputBuf.Len() > 0 {
			return s.outputBuf.Read(p)
		}
		select {
		case data := <-s.output:
			// just accumulate the data in the output buffer
			s.outputBuf.Write(data)
		case <-s.closeRequest:
			s.closed.Store(true)
		}
	}
}

func (s *testDBusStream) Write(p []byte) (n int, err error) {
	for {
		select {
		case <-s.closeRequest:
			s.closed.Store(true)
		default:
			if s.closed.Load().(bool) {
				return 0, fmt.Errorf("stream is closed")
			}
			s.decodeRequest(p)
			return len(p), nil
		}
	}
}

func (s *testDBusStream) Close() error {
	s.closeRequest <- struct{}{}
	return nil
}

func (s *testDBusStream) InjectMessage(msg *dbus.Message) {
	s.sendMsg(msg)
}

func newTestDBusStream(handler DBusHandlerFunc) *testDBusStream {
	s := &testDBusStream{
		handler:      handler,
		output:       make(chan []byte, 1),
		closeRequest: make(chan struct{}, 1),
	}
	s.closed.Store(false)
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

type InjectMessageFunc func(msg *dbus.Message)

// InjectableConnection returns a DBus connection for writing unit tests and a
// function that can be used to inject messages that will be received by the
// test client.
func InjectableConnection(handler DBusHandlerFunc) (*dbus.Conn, InjectMessageFunc, error) {
	testDBusStream := newTestDBusStream(handler)
	conn := mylog.Check2(dbus.NewConn(testDBusStream))
	mylog.Check(conn.Auth([]dbus.Auth{&testAuth{}}))
	mylog.Check(conn.Hello())

	return conn, testDBusStream.InjectMessage, nil
}

// Connection returns a DBus connection for writing unit tests.
//
// The handler function is called for each message sent to the bus. It can
// return any number of messages to send in response.
func Connection(handler DBusHandlerFunc) (*dbus.Conn, error) {
	conn, _ := mylog.Check3(InjectableConnection(handler))
	return conn, err
}
