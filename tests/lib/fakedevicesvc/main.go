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
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var devPrivKey, _ = assertstest.ReadPrivKey(assertstest.DevKey)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "no listening address arg\n")
		os.Exit(1)
	}

	l := mylog.Check2(net.Listen("tcp", os.Args[1]))

	s := &http.Server{Handler: http.HandlerFunc(handle)}
	go s.Serve(l)

	ch := make(chan os.Signal, 2)
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
		io.WriteString(w, `{"request-id": "REQ-ID"}`)
	case "/serial":
		db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{}))
		mylog.Check(db.ImportKey(devPrivKey))

		defer r.Body.Close()

		dec := asserts.NewDecoder(r.Body)

		a := mylog.Check2(dec.Decode())

		serialReq, ok := a.(*asserts.SerialRequest)
		if !ok {
			badRequestError(w, "request is not a serial-request")
			return

		}

		a = mylog.Check2(dec.Decode())

		mod, ok := a.(*asserts.Model)
		if !ok {
			badRequestError(w, "expected model after serial-request")
			return

		}

		if mod.Model() != serialReq.Model() || mod.BrandID() != serialReq.BrandID() {
			badRequestError(w, "model and serial-request do not cross check")
			return
		}
		mylog.Check(asserts.SignatureCheck(serialReq, serialReq.DeviceKey()))

		serialStr := "7777"
		if r.Header.Get("X-Use-Proposed") == "yes" {
			// use proposed serial
			serialStr = serialReq.Serial()
		}

		serial := mylog.Check2(db.Sign(asserts.SerialType, map[string]interface{}{
			"authority-id":        "developer1",
			"brand-id":            "developer1",
			"model":               serialReq.Model(),
			"serial":              serialStr,
			"device-key":          serialReq.HeaderString("device-key"),
			"device-key-sha3-384": serialReq.SignKeyID(),
			"timestamp":           time.Now().Format(time.RFC3339),
		}, serialReq.Body(), devPrivKey.PublicKey().ID()))

		w.Header().Set("Content-Type", asserts.MediaType)
		w.WriteHeader(200)
		w.Write(asserts.Encode(serial))
	}
}
