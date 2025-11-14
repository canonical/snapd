// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package assemblestate

import (
	"context"
	"fmt"
	"net"

	"github.com/snapcore/snapd/cluster/mdns"
)

func MulticastDiscovery(
	ctx context.Context,
	iface string,
	address string,
	port int,
	rdt DeviceToken,
) (<-chan string, func(), error) {
	cfg := mdns.Config{
		Interface:   iface,
		IP:          net.ParseIP(address),
		Port:        port,
		ServiceName: fmt.Sprintf("snapd-%s", rdt),
		ServiceType: "_snapd._https",
		Buffer:      128,
	}

	return mdns.MulticastDiscovery(ctx, cfg)
}
