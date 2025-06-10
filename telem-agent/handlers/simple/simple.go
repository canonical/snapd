// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package simple

import (
	"context"
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
	return session.LogAction(ctx, "AuthConnect", nil, nil, nil, *h.logger)
}

// AuthPublish is called on device publish,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthPublish(ctx context.Context, topic *string, payload *[]byte, userProperties *[]packets.User) error {
	return session.LogAction(ctx, "AuthPublish", &[]string{*topic}, payload, userProperties, *h.logger)
}

// AuthSubscribe is called on device publish,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthSubscribe(ctx context.Context, subscriptions *[]packets.SubOptions, userProperties *[]packets.User) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "AuthSubscribe", &topics, nil, userProperties, *h.logger)
}

// AuthSubscribe is called on device unsubscribe,
// prior forwarding to the MQTT broker.
func (h *Handler) AuthUnsubscribe(ctx context.Context, subscriptions *[]string, userProperties *[]packets.User) error {
	var topics []string

	topics = append(topics, *subscriptions...)

	return session.LogAction(ctx, "AuthUnsubscribe", &topics, nil, userProperties, *h.logger)
}

// Reconvert topics on client going down.
// Topics are passed by reference, so that they can be modified.
func (h *Handler) DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error {
	return session.LogAction(ctx, "DownPublish", &[]string{*topic}, nil, userProperties, *h.logger)
}

// Connect - after client successfully connected.
func (h *Handler) Connect(ctx context.Context) error {
	return session.LogAction(ctx, "Connect", nil, nil, nil, *h.logger)
}

// Publish - after client successfully published.
func (h *Handler) Publish(ctx context.Context, topic *string, payload *[]byte) error {
	return session.LogAction(ctx, "Publish", &[]string{*topic}, payload, nil, *h.logger)
}

// Subscribe - after client successfully subscribed.
func (h *Handler) Subscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Subscribe", &topics, nil, nil, *h.logger)
}

// Unsubscribe - after client unsubscribed.
func (h *Handler) Unsubscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Unsubscribe", &topics, nil, nil, *h.logger)
}

// Disconnect on connection lost.
func (h *Handler) Disconnect(ctx context.Context) error {
	return session.LogAction(ctx, "Disconnect", nil, nil, nil, *h.logger)
}
