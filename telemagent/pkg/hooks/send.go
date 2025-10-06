package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/snapcore/snapd/telemagent/pkg/utils"

	"github.com/canonical/mqtt.golang/paho"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"github.com/snapcore/snapd/client"
)

func (h *TelemAgentHook) OnSubscribe(cl *mochi.Client, pk packets.Packet) packets.Packet {
	var err error
	var snapPublisher string
	var snapName string

	if snapPublisher, snapName, err = utils.GetSnapInfoFromConn(cl.Net.Conn.RemoteAddr().String()); err != nil {
		h.Log.Warn("Could not get snap publisher")
		return packets.Packet{Filters: packets.Subscriptions{{Filter: ErrorTopic}}}
	}

	for i := range pk.Filters {
		if (pk.Filters)[i].Filter[0] != '/' {
			var newTopic string

			if (pk.Filters)[i].Filter[0] != '$' {
				newTopic = fmt.Sprintf("/+/%s/%s", snapPublisher, (pk.Filters)[i].Filter)
			} else {
				h.Log.Error("Local namespace topics cannot start with $")
				(pk.Filters)[i].Filter = DeniedTopic
				continue
			}

			(pk.Filters)[i].Filter = newTopic

			msg := fmt.Sprintf("Topic converted to global namespace, subscribing now to %s", (pk.Filters)[i].Filter)
			h.Log.Info(msg)
		} else {
			if isValid := checkPublisher((pk.Filters)[i].Filter, snapName, snapPublisher, h.Log); !isValid {
				continue
			}
		}

		if _, err := h.config.mqttClient.Subscribe(context.Background(), &paho.Subscribe{
			Subscriptions: []paho.SubscribeOptions{
				{Topic: (pk.Filters)[i].Filter, QoS: 2},
			},
		}); err != nil {
			h.Log.Error(fmt.Sprintf("failed to subscribe (%s). This is likely to mean no messages will be received.", err))
		}
	}

	return pk
}

func (h *TelemAgentHook) OnACLCheck(cl *mochi.Client, topic string, write bool) bool {
	if write {
		return true
	}

	if topic == ErrorTopic || topic == DeniedTopic {
		h.Log.Info("Topic rejected")
		return false
	}

	return true
}

func (h *TelemAgentHook) OnPublish(cl *mochi.Client, pk packets.Packet) (packets.Packet, error) {
	var err error
	var snapPublisher string
	var snapName string

	if cl.Net.Inline {
		return pk, nil
	}

	if snapPublisher, snapName, err = utils.GetSnapInfoFromConn(cl.Net.Conn.RemoteAddr().String()); err != nil {
		h.Log.Warn("Could not get snap publisher")
		return packets.Packet{}, errors.New("failed to get snap publisher")
	}

	if (pk.TopicName)[0] == '/' {
		snapClient := client.New(nil)

		if isAllowed, err := isAllowedTopic(snapClient, pk.TopicName, snapName, snapPublisher, "pub"); err != nil || !isAllowed {
			h.Log.Warn("Topic is not allowed for publishing")
			pk.TopicName = DeniedTopic
		}
	} else {
		deviceId, err := utils.GetDeviceId()
		if err != nil {
			h.Log.Warn(err.Error())
		}

		if (pk.TopicName)[0] == '$' {
			h.Log.Error("Local namespace topic cannot start with $")
			(pk.TopicName) = DeniedTopic
			// error will be caught by interceptor
			return packets.Packet{}, errors.New("local namespace topic cannot start with $")
		}

		newTopic := fmt.Sprintf("/%s/%s/%s", deviceId, snapPublisher, pk.TopicName)

		msg := fmt.Sprintf("Converting topic %s to global namespace, prepending topic with snap name %s", pk.TopicName, snapPublisher)
		h.Log.Info(msg)

		pk.TopicName = newTopic
	}

	if _, err := h.config.mqttClient.Publish(context.Background(), &paho.Publish{
		QoS:     2,
		Topic:   pk.TopicName,
		Payload: pk.Payload,
		Retain:  pk.FixedHeader.Retain,
	}); err != nil {
		return packets.Packet{}, err
	}

	return packets.Packet{}, errors.New("client won't publish")
}

func (h *TelemAgentHook) OnPacketEncode(cl *mochi.Client, pk packets.Packet) packets.Packet {
	if pk.FixedHeader.Type == packets.Publish {
		levels := strings.Split(pk.TopicName, "/")
		levels = levels[1:]
		if len(levels) > 1 {
			var err error
			var snapPublisher string

			if snapPublisher, _, err = utils.GetSnapInfoFromConn(cl.Net.Conn.RemoteAddr().String()); err != nil {
				h.Log.Warn("Could not get snap publisher")
				return pk
			}

			deviceId, err := utils.GetDeviceId()
			if err != nil {
				h.Log.Warn(err.Error())
			}

			if deviceId == levels[0] && snapPublisher == levels[1] {
				levels = levels[2:]
			}

			newTopic := strings.Join(levels, "/")
			pk.TopicName = newTopic

			h.Log.Info("Removed global namespace.")
		} else {
			h.Log.Warn("Could not find global namespace, leaving topic as is.")
		}
		return pk
	}

	return pk
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
