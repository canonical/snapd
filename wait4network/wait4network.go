// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

var (
	errUsage = fmt.Errorf("Usage: %s URL MD5 TIMEOUT INTERVAL; for example: http://start.ubuntu.com/connectivity-check.html 4589f42e1546aa47ca181e5d949d310b 1s 5s", os.Args[0])
)

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) != 5 {
		die(errUsage)
	}

	targetURL := os.Args[1]
	targetMD5 := os.Args[2]
	targetTimeout, err := time.ParseDuration(os.Args[3])
	if err != nil {
		die(err)
	}
	interval, err := time.ParseDuration(os.Args[4])
	if err != nil {
		die(err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.DialTimeout(network, addr, targetTimeout)
			},
		},
	}

	ticker := time.NewTicker(interval)

	for {
		if resp, err := client.Get(targetURL); err == nil {
			h := md5.New()
			io.Copy(h, resp.Body)
			if fmt.Sprintf("%x", h.Sum(nil)) == targetMD5 {
				os.Exit(0)
			}
		}
		<-ticker.C
	}
}
