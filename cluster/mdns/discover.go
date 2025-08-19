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

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/brutella/dnssd"
	dnssdlog "github.com/brutella/dnssd/log"
	"github.com/snapcore/snapd/logger"
)

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
	// Domain is the DNS domain used for discovery; defaults to "local" when empty.
	Domain string
	// Verbose enables verbose logging from the underlying dnssd library when true.
	Verbose bool
	// Buffer controls the size of the returned address channel; defaults to 1
	// to prevent blocking the internal mDNS loop.
	Buffer int
}

// MulticastDiscovery starts advertising the given service and emits the
// addresses observed for peers of the same service type. If no error is
// returned, the stop function should be called to wait on the goroutines that
// this function spawns.
func MulticastDiscovery(ctx context.Context, cfg Config) (discoveries <-chan string, stop func(), err error) {
	if cfg.Interface == "" {
		return nil, nil, errors.New("interface must be provided")
	}

	if cfg.Port <= 0 {
		return nil, nil, fmt.Errorf("invalid port %d", cfg.Port)
	}

	if cfg.ServiceName == "" {
		return nil, nil, errors.New("service name must be provided")
	}

	if cfg.ServiceType == "" {
		return nil, nil, errors.New("service type must be provided")
	}

	if cfg.Verbose {
		dnssdlog.Debug.Enable()
		dnssdlog.Info.Enable()
	}

	domain := cfg.Domain
	if domain == "" {
		domain = "local"
	}

	sv, err := dnssd.NewService(dnssd.Config{
		Name:   cfg.ServiceName,
		Type:   cfg.ServiceType,
		Domain: domain,
		Port:   cfg.Port,
		Ifaces: []string{cfg.Interface},
		IPs:    []net.IP{cfg.IP},
	})
	if err != nil {
		return nil, nil, err
	}

	rp, err := dnssd.NewResponder()
	if err != nil {
		return nil, nil, err
	}

	if _, err := rp.Add(sv); err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := rp.Respond(ctx)
		logger.Debugf("mdns responder exited: %v", err)
	}()

	size := cfg.Buffer
	if size <= 0 {
		size = 1
	}

	addresses := make(chan string, size)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(addresses)

		lookup := fmt.Sprintf("%s.%s.", cfg.ServiceType, domain)
		err := dnssd.LookupType(ctx, lookup, func(be dnssd.BrowseEntry) {
			for _, ip := range be.IPs {
				if len(ip) != net.IPv4len {
					continue
				}

				addr := net.JoinHostPort(ip.String(), strconv.Itoa(be.Port))
				select {
				case <-ctx.Done():
					return
				case addresses <- addr:
				}
			}

		}, func(be dnssd.BrowseEntry) {})
		logger.Debugf("mdns lookup exited: %v", err)
	}()

	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}

	return addresses, stop, nil
}
