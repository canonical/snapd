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
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/godbus/dbus/v5"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/usersession/userbus"
)

func greeting(magic string, value uint32) []byte {
	data := make([]byte, 12)
	copy(data, magic)
	binary.LittleEndian.PutUint32(data[8:], value)
	return data
}

func (s *relaySuite) TestInvalidGreeting(c *C) {
	for _, data := range [][]byte{
		greeting("SNAPBUS2", 1000),
		greeting("SNAPBUS1", 1001),
		greeting("SNAPERR1", 4097),
		[]byte("SNAP"),
	} {
		client, server := pair(c)
		_, err := server.Write(data)
		c.Assert(err, IsNil)
		server.CloseWrite()
		conn, err := userbus.ConnectBridge(client, 1000)
		c.Check(conn, IsNil)
		c.Check(err, NotNil)
		client.Close()
		server.Close()
	}
}

func (s *relaySuite) TestFDNegotiationRequired(c *C) {
	client, server := pair(c)
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		if _, err := server.Write(greeting("SNAPBUS1", 1000)); err != nil {
			done <- err
			return
		}
		reader := bufio.NewReader(server)
		if _, err := reader.ReadByte(); err != nil {
			done <- err
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- err
				return
			}
			var reply string
			switch strings.TrimSpace(line) {
			case "AUTH":
				reply = "REJECTED EXTERNAL\r\n"
			case "AUTH EXTERNAL":
				reply = "DATA\r\n"
			case "DATA":
				reply = "OK 0123456789abcdef0123456789abcdef\r\n"
			case "NEGOTIATE_UNIX_FD":
				reply = "ERROR unsupported\r\n"
			case "BEGIN":
				call, err := dbus.DecodeMessage(reader)
				if err != nil {
					done <- err
					return
				}
				message := &dbus.Message{Type: dbus.TypeMethodReply, Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldReplySerial: dbus.MakeVariant(call.Serial()),
					dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(":1.1")),
				}, Body: []any{":1.1"}}
				done <- message.EncodeTo(server, binary.LittleEndian)
				return
			default:
				done <- fmt.Errorf("unexpected authentication line %q", line)
				return
			}
			if _, err := io.WriteString(server, reply); err != nil {
				done <- err
				return
			}
		}
	}()
	conn, err := userbus.ConnectBridge(client, 1000)
	c.Check(conn, IsNil)
	c.Check(err, ErrorMatches, "user bus does not support file descriptor passing")
	c.Check(<-done, IsNil)
}
