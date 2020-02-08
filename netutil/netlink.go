// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package netutil

import (
	"fmt"
	"net"
	"syscall"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	unetlink "github.com/snapcore/snapd/osutil/udev/netlink"
)

const (
	// see /usr/include/linux/rtnetlink.h
	RTMGRP_IPV4_ROUTE = 0x40
	RTMGRP_IPV6_ROUTE = 0x400

	RTM_NEWROUTE = 24
	RTM_DELROUTE = 25
)

// openNetlinkFd is used in tests to mock a netlink socket
var openNetlinkFd = openNetlinkFdImpl

func openNetlinkFdImpl() (fd int, err error) {
	fd, err = syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE)
	if err != nil {
		return -1, err
	}
	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: RTMGRP_IPV4_ROUTE | RTMGRP_IPV6_ROUTE,
	}
	if err := syscall.Bind(fd, addr); err != nil {
		return -1, err
	}
	return fd, nil
}

// RoutesMonitor monitors the network information and provides callbacks
// when a default gateway is added or removed
type RoutesMonitor struct {
	netlinkFd int

	tomb tomb.Tomb

	netlinkErrors chan error
	netlinkQuitCh chan struct{}

	stop           func()
	readableOrStop func() (bool, error)

	defaultGwAdded   func(string)
	defaultGwRemoved func(string)
}

func NewRoutesMonitor(defaultGwAdded, defaultGwRemoved func(string)) *RoutesMonitor {
	m := &RoutesMonitor{
		netlinkFd: -1,

		netlinkErrors: make(chan error, 1),

		defaultGwAdded:   defaultGwAdded,
		defaultGwRemoved: defaultGwRemoved,
	}
	return m
}

func (m *RoutesMonitor) Connect() error {
	if m.netlinkFd != -1 {
		return fmt.Errorf("cannot connect: already connected")
	}

	fd, err := openNetlinkFd()
	if err != nil {
		return err
	}
	m.netlinkFd = fd
	return nil
}

func (m *RoutesMonitor) Stop() {
	close(m.netlinkQuitCh)
	m.stop()
	select {
	case <-m.tomb.Dead():
	case <-time.After(5 * time.Second):
		logger.Noticef("RoutesMonitor stopping timed out")
		return
	}
	m.tomb.Wait()
}

func isDefaultGw(mm *syscall.NetlinkMessage) (bool, net.IP) {
	nras, err := syscall.ParseNetlinkRouteAttr(mm)
	if err != nil {
		// XXX: log error?
		return false, nil
	}
	for _, nra := range nras {
		// XXX: we could also check for Type:RTA_TABLE and
		//      Value:RT_TABLE_MAIN
		switch nra.Attr.Type {
		case syscall.RTA_GATEWAY:
			return true, net.IP(nra.Value)
		}
	}
	return false, nil
}

func (m *RoutesMonitor) Run() error {
	if m.netlinkFd == -1 {
		return fmt.Errorf("cannot monitor: not connected")
	}

	m.netlinkQuitCh = make(chan struct{})
	// TODO: with go 1.11+ it should be possible to just switch to setting
	// fd to non-blocking and then wrapping the socket via os.NewFile and
	// use Closeq to force a read to stop.
	// c.f. https://github.com/golang/go/commit/ea5825b0b64e1a017a76eac0ad734e11ff557c8e
	readableOrStop, stop, err := unetlink.RawSockStopper(m.netlinkFd)
	if err != nil {
		return fmt.Errorf("internal error: cannot setup stopper: %v", err)
	}
	m.stop = stop
	m.readableOrStop = readableOrStop

	m.tomb.Go(func() error {
		m.monitor()
		return nil
	})
	return nil
}

func (m *RoutesMonitor) monitor() {
	buf := make([]byte, syscall.Getpagesize())
	for {
		_, err := m.readableOrStop()
		if err != nil {
			logger.Noticef("internal error: RoutesMonitor: %v", err)
			return
		}

		select {
		case <-m.netlinkQuitCh:
			syscall.Close(m.netlinkFd)
			m.netlinkFd = -1
			close(m.netlinkErrors)
			return
		default:
			n, err := syscall.Read(m.netlinkFd, buf)
			if errno, ok := err.(syscall.Errno); ok && errno.Temporary() {
				continue
			}
			if err != nil {
				m.netlinkErrors <- err
				close(m.netlinkErrors)
				return
			}
			if n < syscall.NLMSG_HDRLEN {
				// XXX: log error?
				continue
			}
			rawMsg := buf[:n]
			msgs, err := syscall.ParseNetlinkMessage(rawMsg)
			if err != nil {
				// XXX: log error?
				continue
			}
			for _, mm := range msgs {
				switch mm.Header.Type {
				case RTM_NEWROUTE:
					isDefaultGw, gw := isDefaultGw(&mm)
					if isDefaultGw && m.defaultGwAdded != nil {
						m.defaultGwAdded(gw.String())
					}
				case RTM_DELROUTE:
					isDefaultGw, gw := isDefaultGw(&mm)
					if isDefaultGw && m.defaultGwRemoved != nil {
						m.defaultGwRemoved(gw.String())
					}

				}
			}
		}
	}
}
