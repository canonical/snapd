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

package mdns

import "net"

// Config holds the parameters required to advertise and discover peers using
// multicast DNS.
type Config struct {
	// Interface is the network interface used for advertising and discovery.
	Interface string
	// IP is the address announced for the local service.
	IP net.IP
	// Port is the port advertised for the local service.
	Port int
	// ServiceName is the instance name exposed over mDNS.
	ServiceName string
	// ServiceType is the DNS-SD service type (for example "_snapd._https").
	ServiceType string
	// Buffer controls the size of the returned address channel; defaults to 1
	// to prevent blocking the internal mDNS loop.
	Buffer int
}
