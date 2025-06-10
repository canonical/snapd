// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package permissioncontroller

import (
	"context"
	"log/slog"

	"github.com/canonical/mqtt.golang/packets"
	"github.com/canonical/telem-agent/pkg/session"
)

const DeniedTopic = "DENIED"

var _ session.Interceptor = (*PermissionController)(nil)

// PermissionController implements mqtt.Interceptor interface.
type PermissionController struct {
	logger *slog.Logger
}

// New creates new Event entity.
func New(logger *slog.Logger) *PermissionController {
	return &PermissionController{
		logger: logger,
	}
}

func (pc *PermissionController) Intercept(ctx context.Context, pkt *packets.ControlPacket, dir session.Direction) (packets.ControlPacket, error) {
	if dir == session.Down {
		session.LogAction(ctx, "Intercept", nil, nil, nil, *pc.logger)
		return *pkt, nil
	}

	switch pkt.PacketType() {
	case "PUBLISH":
		pubPacket := pkt.Content.(*packets.Publish)
		if pubPacket.Topic != DeniedTopic {
			session.LogAction(ctx, "Intercept", nil, nil, nil, *pc.logger)
			return *pkt, nil
		}

		pc.logger.Warn("Topic is not allowed for publishing and will not be forwarded to the broker")
		switch pubPacket.QoS {
		case 0:
			return packets.ControlPacket{}, session.ErrDeniedTopic
		case 1:
			notAuthorizedResponsePacket := packets.NewControlPacket(packets.PUBACK)
			notAuthorizedResponsePacket.Content.(*packets.Puback).ReasonCode = packets.PubackNotAuthorized
			notAuthorizedResponsePacket.Content.(*packets.Puback).PacketID = pkt.PacketID()
			return *notAuthorizedResponsePacket, session.ErrDeniedTopic
		case 2:
			notAuthorizedResponsePacket := packets.NewControlPacket(packets.PUBREC)
			notAuthorizedResponsePacket.Content.(*packets.Pubrec).ReasonCode = packets.PubrecNotAuthorized
			notAuthorizedResponsePacket.Content.(*packets.Pubrec).PacketID = pkt.PacketID()
			return *notAuthorizedResponsePacket, session.ErrDeniedTopic
		}
	case "SUBSCRIBE":
		subPacket := pkt.Content.(*packets.Subscribe)
		notAuthorizedResponsePacket := packets.NewControlPacket(packets.SUBACK)
		for _, sub := range subPacket.Subscriptions {
			if sub.Topic != DeniedTopic {
				session.LogAction(ctx, "Intercept", nil, nil, nil, *pc.logger)
				return *pkt, nil
			}

			pc.logger.Warn("Topic is not allowed for publishing and will not be forwarded to the broker")

			notAuthorizedResponsePacket.Content.(*packets.Suback).Reasons = append(notAuthorizedResponsePacket.Content.(*packets.Suback).Reasons, packets.SubackNotauthorized)
			notAuthorizedResponsePacket.Content.(*packets.Suback).PacketID = pkt.PacketID()
		}
		return *notAuthorizedResponsePacket, session.ErrDeniedTopic
	default:
		return *pkt, nil
	}
	return *pkt, nil
}
