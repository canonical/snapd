package hooks

import (
	"context"

	"github.com/canonical/mqtt.golang/autopaho"
	"github.com/canonical/mqtt.golang/paho"
)

func (h *TelemAgentHook) OnStarted() {
	ctx := context.Background()

	h.config.mqttConfig.OnPublishReceived = append(h.config.mqttConfig.OnPublishReceived, func(pr paho.PublishReceived) (bool, error) {
		if err := h.config.Server.Publish(pr.Packet.Topic, pr.Packet.Payload, pr.Packet.Retain, pr.Packet.QoS); err != nil {
			return false, err
		}

		h.Log.Info("Server received message from external broker, will resend")
		return true, nil
	})

	c, err := autopaho.NewConnection(ctx, h.config.mqttConfig) // starts process; will reconnect until context cancelled
	if err != nil {
		h.Log.Error("could not connect to remote broker: %v", err)
	}

	h.config.mqttClient = c
	// Wait for the connection to come up
	if err = h.config.mqttClient.AwaitConnection(ctx); err != nil {
		h.Log.Error("could not connect to remote broker: %v", err)
	}

}
