// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var devPrivKey, _ = assertstest.ReadPrivKey(assertstest.DevKey)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "no listening address arg\n")
		os.Exit(1)
	}

	l, err := net.Listen("tcp", os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot listen: %v\n", err)
		os.Exit(1)
	}

	s := &http.Server{Handler: http.HandlerFunc(handle)}
	go s.Serve(l)

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch

	l.Close()
}

func internalError(w http.ResponseWriter, msg string, a ...interface{}) {
	http.Error(w, fmt.Sprintf(msg, a...), 500)
}

func badRequestError(w http.ResponseWriter, msg string, a ...interface{}) {
	http.Error(w, fmt.Sprintf(msg, a...), 400)
}

func handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/request-id":
		w.WriteHeader(200)
		io.WriteString(w, fmt.Sprintf(`{"request-id": "REQ-ID"}`))
	case "/serial":
		db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{})
		if err != nil {
			internalError(w, "cannot open signing db: %v", err)
			return
		}
		err = db.ImportKey(devPrivKey)
		if err != nil {
			internalError(w, "cannot import signing key: %v", err)
			return
		}

		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			internalError(w, "cannot read request: %v", err)
			return
		}

		a, err := asserts.Decode(b)
		if err != nil {
			badRequestError(w, "cannot decode request: %v", err)
			return
		}

		serialReq, ok := a.(*asserts.SerialRequest)
		if !ok {
			badRequestError(w, "request is not a serial-request")
			return

		}

		err = asserts.SignatureCheck(serialReq, serialReq.DeviceKey())
		if err != nil {
			badRequestError(w, "bad serial-request: %v", err)
			return
		}

		serialStr := "7777"
		if r.Header.Get("X-Use-Proposed") == "yes" {
			// use proposed serial
			serialStr = serialReq.Serial()
		}

		serial, err := db.Sign(asserts.SerialType, map[string]interface{}{
			"authority-id":        "developer1",
			"brand-id":            "developer1",
			"model":               serialReq.Model(),
			"serial":              serialStr,
			"device-key":          serialReq.HeaderString("device-key"),
			"device-key-sha3-384": serialReq.SignKeyID(),
			"timestamp":           time.Now().Format(time.RFC3339),
		}, serialReq.Body(), devPrivKey.PublicKey().ID())
		if err != nil {
			internalError(w, "cannot sign serial: %v", err)
			return
		}

		w.Header().Set("Content-Type", asserts.MediaType)
		w.WriteHeader(200)
		w.Write(asserts.Encode(serial))
	}
}
