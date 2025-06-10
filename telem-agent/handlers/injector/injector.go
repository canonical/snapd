// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package injector

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/canonical/telem-agent/pkg/session"
)

var _ session.Handler = (*Injector)(nil)

// Translator implements mqtt.Translator interface.
type Injector struct {
	logger       *slog.Logger
	addedPayload string
}

// New creates new Event entity.
func New(logger *slog.Logger, addedPayload string) *Injector {
	return &Injector{
		logger:       logger,
		addedPayload: addedPayload,
	}
}

// AuthConnect is called on device connection,
// prior forwarding to the MQTT broker.
func (inj *Injector) AuthConnect(ctx context.Context, WillFlag bool, WillTopic *string) error {
	return session.LogAction(ctx, "AuthConnect", nil, nil, nil, *inj.logger)
}

// AuthPublish is called on device publish,
// prior to forwarding to the MQTT broker.
func (inj *Injector) AuthPublish(ctx context.Context, topic *string, payload *[]byte, userProperties *[]packets.User) error {
	hostName, err := os.Hostname()
	if err != nil {
		return err
	}
	addedInfo := fmt.Sprintf(", Device: %s", hostName)
	newPayloadAsString := string((*payload)) + " " + inj.addedPayload + addedInfo
	newPayloadAsByteStream := []byte(newPayloadAsString)
	*payload = newPayloadAsByteStream

	msg := fmt.Sprintf("Payload now is %s", string((*payload)))
	inj.logger.Info(msg)

	return session.LogAction(ctx, "AuthPublish", &[]string{*topic}, payload, userProperties, *inj.logger)
}

// AuthSubscribe is called on device publish,
// prior forwarding to the MQTT broker.
func (h *Injector) AuthSubscribe(ctx context.Context, subscriptions *[]packets.SubOptions, userProperties *[]packets.User) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "AuthSubscribe", &topics, nil, userProperties, *h.logger)
}

// AuthSubscribe is called on device unsubscribe,
// prior forwarding to the MQTT broker.
func (h *Injector) AuthUnsubscribe(ctx context.Context, subscriptions *[]string, userProperties *[]packets.User) error {
	var topics []string

	topics = append(topics, *subscriptions...)

	return session.LogAction(ctx, "AuthUnsubscribe", &topics, nil, userProperties, *h.logger)
}

// Reconvert topics on client going down.
// Topics are passed by reference, so that they can be modified.
func (inj *Injector) DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error {
	return session.LogAction(ctx, "DownPublish", &[]string{*topic}, nil, userProperties, *inj.logger)
}

// Connect - after client successfully connected.
func (inj *Injector) Connect(ctx context.Context) error {
	return session.LogAction(ctx, "Connect", nil, nil, nil, *inj.logger)
}

// Publish - after client successfully published.
func (inj *Injector) Publish(ctx context.Context, topic *string, payload *[]byte) error {
	return session.LogAction(ctx, "Publish", &[]string{*topic}, payload, nil, *inj.logger)
}

// Subscribe - after client successfully subscribed.
func (h *Injector) Subscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Subscribe", &topics, nil, nil, *h.logger)
}

// Unsubscribe - after client unsubscribed.
func (h *Injector) Unsubscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Unsubscribe", &topics, nil, nil, *h.logger)
}

// Disconnect on connection lost.
func (inj *Injector) Disconnect(ctx context.Context) error {
	return session.LogAction(ctx, "Disconnect", nil, nil, nil, *inj.logger)
}
