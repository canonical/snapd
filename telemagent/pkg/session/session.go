// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"crypto/x509"
)

// The sessionKey type is unexported to prevent collisions with context keys defined in
// other packages.
type sessionKey struct{}

// Session stores MQTT session data.
type Session struct {
	ID       string
	Username string
	Password []byte
	Cert     x509.Certificate
}

// snapKey and SnapPub are structs used to add the snap publisher and name to the context.
type (
	snapKey struct{}
	SnapPub struct {
		Publisher string
		Name      string
	}
)

// NewContext stores Session in context.Context values.
// It uses pointer to the session so it can be modified by handler.
func NewContext(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// FromContext retrieves Session from context.Context.
// Second value indicates if session is present in the context.
// and if it's safe to use it (it's not nil).=.
func FromContext(ctx context.Context) (*Session, bool) {
	if s, ok := ctx.Value(sessionKey{}).(*Session); ok && s != nil {
		return s, true
	}
	return nil, false
}

// FromContext retrieves Session from context.Context.
// Second value indicates if session is present in the context
// and if it's safe to use it (it's not nil).
func GetSnapFromContext(ctx context.Context) (string, string, bool) {
	if s, ok := ctx.Value(snapKey{}).(*SnapPub); ok {
		return s.Publisher, s.Name, true
	}
	return "", "", false
}

// NewContext stores Session in context.Context values.
// It uses pointer to the session so it can be modified by handler.
func AddSnapToContext(ctx context.Context, publisher string, name string) context.Context {
	return context.WithValue(ctx, snapKey{}, &SnapPub{Publisher: publisher, Name: name})
}
