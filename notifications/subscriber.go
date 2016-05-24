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
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/snapcore/snapd/strutil"
)

// A Subscriber is interested in receiving notifications
type Subscriber struct {
	uuid     string
	conn     websocketConnection
	types    []string
	resource string
}

// Subscribers is a collection of subscribers
type Subscribers map[string]*Subscriber

type websocketConnection interface {
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// NewSubscriber returns a new subscriber containing the given websocket
// connection and type/resource filters set from the query string params in the
// supplied http request
func NewSubscriber(c websocketConnection, r *http.Request) *Subscriber {
	s := &Subscriber{
		uuid: strutil.MakeRandomString(16),
		conn: c,
	}

	q := r.URL.Query()
	if len(q["types"]) > 0 {
		s.types = strings.Split(q["types"][0], ",")
	}
	if len(q["resource"]) > 0 {
		s.resource = q["resource"][0]
	}

	return s
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
		// notification has full resource path while we have the uuid portion
		return strings.HasSuffix(n.Resource, s.resource)
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
