// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package translator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/pkg/session"
)

var _ session.Handler = (*Translator)(nil)

// Translator implements mqtt.Translator interface.
type Translator struct {
	logger    *slog.Logger
	topics    map[string]string
	revTopics map[string]string
}

// New creates new Event entity.
func New(logger *slog.Logger, topics, revTopics map[string]string) *Translator {
	return &Translator{
		logger:    logger,
		topics:    topics,
		revTopics: revTopics,
	}
}

// AuthConnect is called on device connection,
// prior forwarding to the MQTT broker.
func (tr *Translator) AuthConnect(ctx context.Context, WillFlag bool, WillTopic *string, Username *string, Password *[]byte)  error {
	return session.LogAction(ctx, "AuthConnect", nil, nil, nil, *tr.logger)
}

// AuthPublish is called on device publish,
// prior forwarding to the MQTT broker.
func (tr *Translator) AuthPublish(ctx context.Context, topic *string, payload *[]byte, userProperties *[]packets.User) error {
	newTopic, ok := tr.topics[*topic]
	if ok {
		msg := fmt.Sprintf("Topic %s translated to Topic %s. Publishing...", *topic, newTopic)
		*topic = newTopic
		tr.logger.Info(msg)
	} else {
		msg := fmt.Sprintf("Topic %s could not be translated and thus kept the same. Publishing...", *topic)
		tr.logger.Info(msg)
	}

	return session.LogAction(ctx, "AuthPublish", &[]string{*topic}, payload, userProperties, *tr.logger)
}

// AuthSubscribe is called on device publish,
// prior forwarding to the MQTT broker.
func (tr *Translator) AuthSubscribe(ctx context.Context, subscriptions *[]packets.SubOptions, userProperties *[]packets.User) error {
	for i, sub := range *subscriptions {
		newTopic, ok := tr.topics[sub.Topic]
		if ok {
			msg := fmt.Sprintf("Topic %s translated to Topic %s. Subscribing...", (*subscriptions)[i].Topic, newTopic)
			(*subscriptions)[i].Topic = newTopic
			tr.logger.Info(msg)
		} else {
			msg := fmt.Sprintf("Topic %s could not be translated and thus kept the same. Subscribing...", (*subscriptions)[i].Topic)
			tr.logger.Info(msg)
		}
	}

	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "AuthSubscribe", &topics, nil, userProperties, *tr.logger)
}

// AuthSubscribe is called on device unsubscribe,
// prior forwarding to the MQTT broker.
func (h *Translator) AuthUnsubscribe(ctx context.Context, subscriptions *[]string, userProperties *[]packets.User) error {
	var topics []string

	topics = append(topics, *subscriptions...)

	return session.LogAction(ctx, "AuthUnsubscribe", &topics, nil, userProperties, *h.logger)
}

// Reconvert topics on client going down.
// Topics are passed by reference, so that they can be modified.
func (tr *Translator) DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error {
	newTopic, ok := tr.revTopics[*topic]
	if ok {
		msg := fmt.Sprintf("Topic %s translated to Topic %s. Sending...", *topic, newTopic)
		*topic = newTopic
		tr.logger.Info(msg)
	} else {
		msg := fmt.Sprintf("Topic %s could not be translated and thus kept the same. Subscribing...", *topic)
		tr.logger.Info(msg)
	}

	return session.LogAction(ctx, "DownSubscribe", &[]string{*topic}, nil, nil, *tr.logger)
}

// Connect - after client successfully connected.
func (tr *Translator) Connect(ctx context.Context) error {
	return session.LogAction(ctx, "Connect", nil, nil, nil, *tr.logger)
}

// Publish - after client successfully published.
func (tr *Translator) Publish(ctx context.Context, topic *string, payload *[]byte) error {
	return session.LogAction(ctx, "Publish", &[]string{*topic}, payload, nil, *tr.logger)
}

// Subscribe - after client successfully subscribed.
func (h *Translator) Subscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Subscribe", &topics, nil, nil, *h.logger)
}

// Unsubscribe - after client unsubscribed.
func (h *Translator) Unsubscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Unsubscribe", &topics, nil, nil, *h.logger)
}

// Disconnect on connection lost.
func (tr *Translator) Disconnect(ctx context.Context) error {
	return session.LogAction(ctx, "Disconnect", nil, nil, nil, *tr.logger)
}
