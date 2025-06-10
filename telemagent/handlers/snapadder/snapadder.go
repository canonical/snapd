// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package snapadder

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/snapcore/snapd/telemagent/interceptors/permissioncontroller"
	"github.com/snapcore/snapd/telemagent/internal/utils"
	"github.com/snapcore/snapd/telemagent/pkg/session"
	"github.com/snapcore/snapd/client"
)

var _ session.Handler = (*SnapAdder)(nil)

// SnapAdder implements mqtt.SnapAdder interface.
type SnapAdder struct {
	logger *slog.Logger
}

// New creates new Event entity.
func New(logger *slog.Logger) *SnapAdder {
	return &SnapAdder{
		logger: logger,
	}
}

// AuthConnect is called on device connection,
// prior forwarding to the MQTT broker.
func (tr *SnapAdder) AuthConnect(ctx context.Context, WillFlag bool, WillTopic *string) error {
	var snapPublisher string
	var ok bool

	if snapPublisher, _, ok = session.GetSnapFromContext(ctx); !ok {
		tr.logger.Warn("Could not get snap publisher")
	}

	deviceId, err := utils.GetDeviceId()
	if err != nil {
		tr.logger.Warn(err.Error())
	}

	if WillFlag {
		newTopic := fmt.Sprintf("/%s/%s/%s", deviceId, snapPublisher, *WillTopic)
		*WillTopic = newTopic

		msg := fmt.Sprintf("Will topic converted to global namespace, will send now to %s", *WillTopic)
		tr.logger.Info(msg)
	}

	return session.LogAction(ctx, "AuthConnect", nil, nil, nil, *tr.logger)
}

// AuthPublish is called on device publish,
// prior to forwarding to the MQTT broker.
func (tr *SnapAdder) AuthPublish(ctx context.Context, topic *string, payload *[]byte, userProperties *[]packets.User) error {
	if snapPublisher, snapName, ok := session.GetSnapFromContext(ctx); ok {
		if (*topic)[0] == '/' {
			snapClient := client.New(nil)

			if isAllowed, err := isAllowedTopic(snapClient, *topic, snapName, snapPublisher, "pub"); err != nil || !isAllowed {
				tr.logger.Warn("Topic is not allowed for publishing")
				*topic = permissioncontroller.DeniedTopic
			}
		} else {
			deviceId, err := utils.GetDeviceId()
			if err != nil {
				tr.logger.Warn(err.Error())
			}

			if (*topic)[0] == '$' {
				tr.logger.Error("Local namespace topic cannot start with $")
				(*topic) = permissioncontroller.DeniedTopic
				// error will be caught by interceptor
				return nil
			}

			newTopic := fmt.Sprintf("/%s/%s/%s", deviceId, snapPublisher, *topic)

			msg := fmt.Sprintf("Converting topic %s to global namespace, prepending topic with snap name %s", *topic, snapPublisher)
			tr.logger.Info(msg)

			*topic = newTopic
		}
	} else {
		tr.logger.Warn("Could not find pid, leaving topic as is")
	}
	return session.LogAction(ctx, "AuthPublish", &[]string{*topic}, payload, userProperties, *tr.logger)
}

// AuthSubscribe is called on device subscribe,
// prior to forwarding to the MQTT broker.
func (tr *SnapAdder) AuthSubscribe(ctx context.Context, subscriptions *[]packets.SubOptions, userProperties *[]packets.User) error {
	var ok bool
	var snapPublisher string
	var snapName string
	var newSubscriptions []packets.SubOptions
	var topics []string

	if snapPublisher, snapName, ok = session.GetSnapFromContext(ctx); !ok {
		tr.logger.Warn("Could not get snap publisher")
		*subscriptions = []packets.SubOptions{{Topic: permissioncontroller.DeniedTopic}}
		return session.LogAction(ctx, "AuthSubscribe", &topics, nil, userProperties, *tr.logger)
	}

	for i := range *subscriptions {
		if (*subscriptions)[i].Topic[0] != '/' {
			var newTopic string

			if (*subscriptions)[i].Topic[0] != '$' {
				newTopic = fmt.Sprintf("/+/%s/%s", snapPublisher, (*subscriptions)[i].Topic)
			} else {
				tr.logger.Error("Local namespace topics cannot start with $")
				(*subscriptions)[i].Topic = permissioncontroller.DeniedTopic
				continue
			}

			(*subscriptions)[i].Topic = newTopic

			msg := fmt.Sprintf("Topic converted to global namespace, subscribing now to %s", (*subscriptions)[i].Topic)
			tr.logger.Info(msg)
		} else {
			if isValid := checkPublisher((*subscriptions)[i].Topic, snapName, snapPublisher, tr.logger); !isValid {
				continue
			}
		}
		topics = append(topics, (*subscriptions)[i].Topic)
		newSubscriptions = append(newSubscriptions, (*subscriptions)[i])
	}

	if len(newSubscriptions) == 0 {
		newSubscriptions = append(newSubscriptions, packets.SubOptions{Topic: permissioncontroller.DeniedTopic})
	}
	*subscriptions = newSubscriptions
	return session.LogAction(ctx, "AuthSubscribe", &topics, nil, userProperties, *tr.logger)
}

// AuthUnsubscribe is called on device unsubscribe,
// prior to forwarding to the MQTT broker.
func (tr *SnapAdder) AuthUnsubscribe(ctx context.Context, subscriptions *[]string, userProperties *[]packets.User) error {
	var ok bool
	var snapPublisher string
	var snapName string
	var topics []string

	if snapPublisher, snapName, ok = session.GetSnapFromContext(ctx); !ok {
		tr.logger.Warn("Could not get snap publisher")
		*subscriptions = []string{permissioncontroller.DeniedTopic}
		return session.LogAction(ctx, "AuthSubscribe", &topics, nil, userProperties, *tr.logger)
	}

	for i := range *subscriptions {
		if (*subscriptions)[i][0] != '/' {
			var newTopic string

			if (*subscriptions)[i][0] != '$' {
				newTopic = fmt.Sprintf("/+/%s/%s", snapPublisher, (*subscriptions)[i])
			} else {
				tr.logger.Error("Local namespace topics cannot start with $")
				(*subscriptions)[i] = permissioncontroller.DeniedTopic
				continue
			}

			(*subscriptions)[i] = newTopic

			msg := fmt.Sprintf("Topic converted to global namespace, subscribing now to %s", (*subscriptions)[i])
			tr.logger.Info(msg)
		} else {
			if isValid := checkPublisher((*subscriptions)[i], snapName, snapPublisher, tr.logger); !isValid {
				continue
			}
		}
		topics = append(topics, (*subscriptions)[i])
	}

	if len(topics) == 0 {
		topics = append(topics, permissioncontroller.DeniedTopic)
	}
	*subscriptions = topics

	return session.LogAction(ctx, "AuthUnsubscribe", &topics, nil, userProperties, *tr.logger)
}

// Reconvert topics on client going down.
// Topics are passed by reference, so that they can be modified.
func (tr *SnapAdder) DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error {
	levels := strings.Split(*topic, "/")
	levels = levels[1:]
	if len(levels) > 1 {
		var snapPublisher string
		var ok bool

		if snapPublisher, _, ok = session.GetSnapFromContext(ctx); !ok {
			tr.logger.Warn("Could not get snap publisher")
		}

		deviceId, err := utils.GetDeviceId()
		if err != nil {
			tr.logger.Warn(err.Error())
		}

		if deviceId == levels[0] && snapPublisher == levels[1] {
			levels = levels[2:]
		}

		newTopic := strings.Join(levels, "/")
		*topic = newTopic

		tr.logger.Info("Removed global namespace.")
	} else {
		tr.logger.Warn("Could not find global namespace, leaving topic as is.")
	}
	return session.LogAction(ctx, "DownPublish", &[]string{*topic}, nil, nil, *tr.logger)
}

// Connect - after client successfully connected.
func (tr *SnapAdder) Connect(ctx context.Context) error {
	return session.LogAction(ctx, "Connect", nil, nil, nil, *tr.logger)
}

// Publish - after client successfully published.
func (tr *SnapAdder) Publish(ctx context.Context, topic *string, payload *[]byte) error {
	return session.LogAction(ctx, "Publish", &[]string{*topic}, payload, nil, *tr.logger)
}

// Subscribe - after client successfully subscribed.
func (h *SnapAdder) Subscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}
	return session.LogAction(ctx, "Subscribe", &topics, nil, nil, *h.logger)
}

// Unsubscribe - after client unsubscribed.
func (h *SnapAdder) Unsubscribe(ctx context.Context, subscriptions *[]packets.SubOptions) error {
	var topics []string

	for _, x := range *subscriptions {
		topics = append(topics, x.Topic)
	}

	return session.LogAction(ctx, "Unsubscribe", &topics, nil, nil, *h.logger)
}

// Disconnect on connection lost.
func (tr *SnapAdder) Disconnect(ctx context.Context) error {
	return session.LogAction(ctx, "Disconnect", nil, nil, nil, *tr.logger)
}

func isAllowedTopic(snapClient *client.Client, topic, snapName, snapPublisher, action string) (bool, error) {
	levels := strings.Split(topic, "/")[1:]
	if len(levels) < 2 {
		return false, fmt.Errorf("invalid topic: no snap publisher")
	}

	if levels[1] == snapPublisher {
		return true, nil
	}

	if action == "pub" {
		return false, nil
	}



	return false, nil
}

func isContainedIn(accepted, candidate string) bool {
	acceptedLevels := strings.Split(accepted, "/")
	candidateLevels := strings.Split(candidate, "/")

	minLen := min(len(acceptedLevels), len(candidateLevels))

	if len(acceptedLevels) != len(candidateLevels) && acceptedLevels[len(acceptedLevels)-1] != "#" {
		return false
	}

	for i := 0; i < minLen; i++ {
		if acceptedLevels[i] == "#" {
			return true
		}

		if acceptedLevels[i] == "+" {
			continue
		}

		if acceptedLevels[i] != candidateLevels[i] {
			return false
		}
	}

	return true
}

func checkPublisher(topic, snapName, snapPublisher string, logger *slog.Logger) bool {
	levels := strings.Split(topic, "/")[1:]
	if len(levels) < 2 {
		logger.Warn("Topic is global, but does not have snap publisher and device id.")
		return false
	}
	snapPublisherId := levels[1]

	snapClient := client.New(nil)
	headers := make(map[string]string)
	headers["username"] = snapPublisherId
	results, err := snapClient.Known("account", headers, &client.KnownOptions{Remote: true})

	if err == nil && len(results) > 0 {
		logger.Info("Publisher username is valid and registered at the store.")
		return false
	}

	headers = make(map[string]string)
	headers["account-id"] = snapPublisherId
	results, err = snapClient.Known("account", headers, &client.KnownOptions{Remote: true})
	if err != nil || len(results) == 0 {
		logger.Warn("Could not check identity of publisher")
		// continue
	} else {
		logger.Info("Publisher account-id is valid and registered at the store.")
	}

	if isAllowed, err := isAllowedTopic(snapClient, topic, snapName, snapPublisher, "sub"); err != nil || !isAllowed {
		logger.Warn("Topic is not allowed for subscription")
		return false
	}
	return true
}
