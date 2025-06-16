// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"log/slog"

	"github.com/canonical/mqtt.golang/packets"
)

// Handler is an interface for mProxy hooks.
type Handler interface {
	// Authorization on client `CONNECT`
	// Each of the params are passed by reference, so that it can be changed
	AuthConnect(ctx context.Context, WillFlag bool, WillTopic *string) error

	// Authorization on client `PUBLISH`
	// Topic is passed by reference, so that it can be modified
	AuthPublish(ctx context.Context, topic *string, payload *[]byte, userProperties *[]packets.User) error

	// Authorization on client `SUBSCRIBE`
	// Topics are passed by reference, so that they can be modified
	AuthSubscribe(ctx context.Context, subscriptions *[]packets.SubOptions, userProperties *[]packets.User) error

	// Authorization on client `UNSUBSCRIBE`
	// Topics are passed by reference, so that they can be modified
	AuthUnsubscribe(ctx context.Context, subscriptions *[]string, userProperties *[]packets.User) error

	// Reconvert topics on client going down
	// Topics are passed by reference, so that they can be modified
	DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error

	// After client successfully connected
	Connect(ctx context.Context) error

	// After client successfully published
	Publish(ctx context.Context, topic *string, payload *[]byte) error

	// After client successfully subscribed
	Subscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error

	// After client unsubscribed
	Unsubscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error

	// Disconnect on connection with client lost
	Disconnect(ctx context.Context) error
}

var errSessionMissing = errors.New("session is missing")

func LogAction(ctx context.Context, action string, topics *[]string, payload *[]byte, userProperties *[]packets.User, logger slog.Logger) error {
	s, ok := FromContext(ctx)
	args := []interface{}{}
	if !ok {
		args = append(args, slog.Any("error", errSessionMissing))
		logger.Error(action+"() failed to complete", args...)
		return errSessionMissing
	}
	args = append(args, slog.Group("session", slog.String("id", s.ID), slog.String("username", s.Username)))
	if s.Cert.Subject.CommonName != "" {
		args = append(args, slog.Group("cert", slog.String("cn", s.Cert.Subject.CommonName)))
	}
	if topics != nil {
		args = append(args, slog.Any("topics", *topics))
	}
	if payload != nil {
		args = append(args, slog.Any("payload", *payload))
	}
	if userProperties != nil {
		args = append(args, slog.Any("user_properties", *userProperties))
	}
	logger.Info(action+"() completed successfully", args...)

	return nil
}
