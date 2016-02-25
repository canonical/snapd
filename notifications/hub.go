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
	"sync"
)

// A Hub allows subscribers to receive notifications.
type Hub struct {
	sync.Mutex
	subscribers Subscribers
}

// NewHub returns an initialised hub
func NewHub() *Hub {
	return &Hub{
		subscribers: make(Subscribers),
	}
}

// SubscriberCount returns the number of subscribers
func (h *Hub) SubscriberCount() int {
	h.Lock()
	defer h.Unlock()

	return len(h.subscribers)
}

// Subscribe registers a subscriber to receive notifications.
func (h *Hub) Subscribe(s *Subscriber) {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.subscribers[s.uuid]; !ok {
		h.subscribers[s.uuid] = s
	}
}

// Unsubscribe unregisters a subscriber from notifications.
func (h *Hub) Unsubscribe(s *Subscriber) {
	h.Lock()
	defer h.Unlock()

	h.doUnsubscribe(s)
}

func (h *Hub) doUnsubscribe(s *Subscriber) {
	s.conn.Close()
	delete(h.subscribers, s.uuid)
}

// Publish broadcasts a notification to subscribers.
func (h *Hub) Publish(n *Notification) {
	h.Lock()
	defer h.Unlock()

	for _, s := range h.subscribers {
		err := s.Notify(n)
		if err != nil {
			h.doUnsubscribe(s)
		}
	}
}
