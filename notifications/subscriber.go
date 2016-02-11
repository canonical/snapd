// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package notifications

import (
	"encoding/json"

	"github.com/gorilla/websocket"
)

// A Subscriber is interested in receiving notifications
type Subscriber struct {
	uuid     string
	conn     messageWriter
	types    []string
	resource string
}

// Subscribers is a collection of subscribers
type Subscribers map[string]*Subscriber

type messageWriter interface {
	WriteMessage(messageType int, data []byte) error
}

// Notify receives a notification and if the subscriber is interested in it it
// is encoded as JSON and written to the websocket.
func (s *Subscriber) Notify(n *Notification) error {
	if !s.canAccept(n) {
		return nil
	}

	b, err := json.Marshal(n)
	if err != nil {
		return err
	}

	return s.conn.WriteMessage(websocket.TextMessage, b)
}

func (s *Subscriber) canAccept(n *Notification) bool {
	if s.resource != "" {
		return s.resource == n.Resource
	}

	if len(s.types) > 0 {
		for _, t := range s.types {
			if t == n.Type {
				return true
			}
		}

		return false
	}

	return true
}
