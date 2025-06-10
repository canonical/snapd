// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package testhandler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/canonical/telem-agent/pkg/session"
)

var _ session.Handler = (*Handler)(nil)

// Handler implements mqtt.Handler interface.
type Handler struct {
	logger *slog.Logger
}

// New creates new Event entity.
func New(logger *slog.Logger) *Handler {
	return &Handler{
		logger: logger,
	}
}

// AuthConnect is called on device connection,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthConnect(ctx context.Context, WillFlag bool, WillTopic *string) error {
	if ctx == context.Background() {
		return nil
	}
	return fmt.Errorf("error for testing")
}

// AuthPublish is called on device publish,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthPublish(ctx context.Context, topic *string, payload *[]byte, userProperties *[]packets.User) error {
	return fmt.Errorf("error for testing")
}

// AuthSubscribe is called on device publish,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthSubscribe(ctx context.Context, subscriptions *[]packets.SubOptions, userProperties *[]packets.User) error {
	return fmt.Errorf("error for testing")
}

// AuthSubscribe is called on device publish,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthUnsubscribe(ctx context.Context, subscriptions *[]string, userProperties *[]packets.User) error {
	return fmt.Errorf("error for testing")
}

// Reconvert topics on client going down.
// Topics are passed by reference, so that they can be modified.
func (h *Handler) DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error {
	return fmt.Errorf("error for testing")
}

// Connect - after client successfully connected.
func (h *Handler) Connect(ctx context.Context) error {
	return fmt.Errorf("error for testing")
}

// Publish - after client successfully published.
func (h *Handler) Publish(ctx context.Context, topic *string, payload *[]byte) error {
	return fmt.Errorf("error for testing")
}

// Subscribe - after client successfully subscribed.
func (h *Handler) Subscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	return fmt.Errorf("error for testing")
}

// Unsubscribe - after client unsubscribed.
func (h *Handler) Unsubscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	return fmt.Errorf("error for testing")
}

// Disconnect on connection lost.
func (h *Handler) Disconnect(ctx context.Context) error {
	return fmt.Errorf("error for testing")
}
