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

// Package devicestate implements the manager and state aspects responsible
// for the device identity and policies.
package devicestate

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

// DeviceManager is responsible for managing the device identity and device
// policies.
type DeviceManager struct {
	state  *state.State
	runner *state.TaskRunner
}

// Manager returns a new device manager.
func Manager(s *state.State) (*DeviceManager, error) {
	runner := state.NewTaskRunner(s)

	runner.AddHandler("generate-device-key", doGenerateDeviceKey, nil)
	runner.AddHandler("request-serial", doRequestSerial, nil)
	runner.AddHandler("download-serial", doDownloadSerial, nil)

	return &DeviceManager{state: s, runner: runner}, nil
}

func (m *DeviceManager) ensureOperational() error {
	m.state.Lock()
	defer m.state.Unlock()
	// XXX: auth.Device/SetDevice should probably move to devicestate
	// they are not quite just about auth and also we risk circular imports
	// (auth will need to mediate bits from devicestate soon)
	device, err := auth.Device(m.state)
	if err != nil {
		return err
	}

	if device.Serial != "" {
		// serial is set, we are all set
		return nil
	}

	if device.Brand == "" || device.Model == "" {
		// need first-boot, loading of model assertion info
		if release.OnClassic {
			// XXX: cheat for now to get us started somewhere
			device.Brand = "canonical"
			device.Model = "pc"
			err := auth.SetDevice(m.state, device)
			if err != nil {
				return err
			}
			m.state.Unlock()
			m.state.Lock()
		} else {
			// full first-boot stuff!
			// TODO: move first boot setup/invocation here!
			panic("need full first-boot to initialize brand and model of device")
		}
	}

	for _, chg := range m.state.Changes() {
		if chg.Kind() == "become-operational" && !chg.Status().Ready() {
			// change already in motion
			return nil
		}
	}

	// XXX: some of these will need to be split and use hooks
	// retries might need to embrace more than one "task" then,
	// need to be careful

	genKey := m.state.NewTask("generate-device-key", i18n.G("Generate device key"))
	serialReq := m.state.NewTask("request-serial", i18n.G("Deliver device serial request"))
	serialReq.WaitFor(genKey)
	retrieveSerial := m.state.NewTask("download-serial", i18n.G("Retrieve and install device serial"))
	retrieveSerial.WaitFor(serialReq)
	serialReq.Set("serial-setup-task", retrieveSerial.ID())

	chg := m.state.NewChange("become-operational", i18n.G("Setting up device identity"))
	chg.AddAll(state.NewTaskSet(genKey, serialReq, retrieveSerial))

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *DeviceManager) Ensure() error {
	err := m.ensureOperational()
	if err != nil {
		return err
	}
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *DeviceManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *DeviceManager) Stop() {
	m.runner.Stop()
}

var (
	keyLength = 4096
	// XXX: a 2nd different URL for nonce?
	// TODO: this will come as config from the gadget snap
	serialRequestURL = "https://serial.request" // XXX dummy value!
)

func doGenerateDeviceKey(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	// XXX: where to store device private key ultimately? use asserts support?
	var encoded string
	err := st.Get("device-keypair", &encoded)
	if err == nil {
		// nothing to do
		return nil
	}
	if err != state.ErrNoState {
		return err
	}
	keyPair, err := rsa.GenerateKey(rand.Reader, keyLength)
	if err != nil {
		return fmt.Errorf("cannot generate device key pair: %v", err)
	}

	encoded = base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(keyPair))
	st.Set("device-keypair", encoded)
	return nil
}

func keyPair(st *state.State) (*rsa.PrivateKey, error) {
	var encoded string
	err := st.Get("device-keypair", &encoded)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("internal error: cannot find device key pair")
	}
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot decode device key pair: %v", err)
	}
	pk, err := x509.ParsePKCS1PrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot decode device key pair: %v", err)
	}
	return pk, nil
}

type serialSetup struct {
	Serial        string `json:"serial"`
	SerialRequest string `json:"serial-request"`
}

type requestIDResp struct {
	RequestID string `json:"request-id"`
}

var errPoll = errors.New("serial-request accepted, poll later")

func submitSerialRequest(cli *http.Client, encodedSerialRequest []byte) (*asserts.Serial, error) {
	resp, err := cli.Post(serialRequestURL, asserts.MediaType, bytes.NewBuffer(encodedSerialRequest))
	if err != nil {
		return nil, &state.Retry{After: 60 * time.Second}
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, fmt.Errorf("unexpected status code %d trying to post serial request to %s)", resp.StatusCode, serialRequestURL)
	}

	switch resp.StatusCode {
	case 200, 201:
	case 202:
		return nil, errPoll
	default:
		return nil, &state.Retry{After: 60 * time.Second}
	}

	// decode body with serial assertion
	dec := asserts.NewDecoder(resp.Body)
	got, err := dec.Decode()
	if err != nil { // assume broken i/o
		return nil, &state.Retry{After: 60 * time.Second}
	}

	serial, ok := got.(*asserts.Serial)
	if !ok {
		return nil, fmt.Errorf("serial-request response did not return a serial assertion")
	}

	return serial, nil
}

func doRequestSerial(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	device, err := auth.Device(st)
	if err != nil {
		return err
	}

	var retrieveSerialID string
	err = t.Get("serial-setup-task", &retrieveSerialID)
	if err != nil {
		return err
	}
	retrieveSerial := st.Task(retrieveSerialID)
	if retrieveSerial == nil {
		return fmt.Errorf("internal error: cannot find retrieve serial task")
	}

	pk, err := keyPair(st)
	if err != nil {
		return err
	}

	cli := &http.Client{Timeout: 30 * time.Second}

	st.Unlock()
	resp, err := cli.Get(serialRequestURL)
	st.Lock()
	if err != nil {
		return &state.Retry{After: 60 * time.Second}
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("unexpected status code %d trying to get serial request nonce/ticket (from %s)", resp.StatusCode, serialRequestURL)
	}
	if resp.StatusCode != 200 {
		return &state.Retry{After: 60 * time.Second}
	}

	dec := json.NewDecoder(resp.Body)
	var requestID requestIDResp
	err = dec.Decode(&requestID)
	if err != nil { // assume broken i/o
		return &state.Retry{After: 60 * time.Second}
	}

	privKey := asserts.RSAPrivateKey(pk)
	encodedPubKey, err := asserts.EncodePublicKey(privKey.PublicKey())
	if err != nil {
		return fmt.Errorf("internal error: cannot encode device public key: %v", err)

	}

	serialReq, err := asserts.SignWithoutAuthority(asserts.SerialRequestType, map[string]interface{}{
		"brand-id":   device.Brand,
		"model":      device.Model,
		"request-id": requestID.RequestID,
		"device-key": string(encodedPubKey),
	}, nil, privKey) // XXX: fill body with some agreed hardware details
	if err != nil {
		return err
	}

	encodedSerialReq := asserts.Encode(serialReq)

	// NB: until we get at least an Accepted (202) we need to
	// retry from scratch creating a new nonce because the
	// previous one used could have expired

	st.Unlock()
	serial, err := submitSerialRequest(cli, encodedSerialReq)
	st.Lock()
	if err == errPoll {
		retrieveSerial.Set("serial-setup", serialSetup{
			SerialRequest: string(encodedSerialReq),
		})
		// TODO: delay next task?
		t.SetStatus(state.DoneStatus)
		return nil
	}
	if err != nil { // errors and retries
		return err
	}

	retrieveSerial.Set("serial-setup", serialSetup{
		Serial: string(asserts.Encode(serial)),
	})
	t.SetStatus(state.DoneStatus)
	return nil
}

func doDownloadSerial(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	device, err := auth.Device(st)
	if err != nil {
		return err
	}

	var serialSup serialSetup
	err = t.Get("serial-setup", &serialSup)
	if err != nil {
		return err
	}

	var serial *asserts.Serial

	if serialSup.SerialRequest != "" {
		// delivery of serial-request was asked to poll
		cli := &http.Client{Timeout: 30 * time.Second}
		var err error
		st.Unlock()
		serial, err = submitSerialRequest(cli, []byte(serialSup.SerialRequest))
		st.Lock()
		if err == errPoll {
			// TODO: what poll interval?
			return &state.Retry{After: 60 * time.Second}

		}
		if err != nil { // errors and retries
			return err
		}

	} else {
		// delivery of serial-request got directly a result
		a, err := asserts.Decode([]byte(serialSup.Serial))
		if err != nil {
			return fmt.Errorf("internal error: cannot decode retrieved serial assertion: %v", err)
		}
		var ok bool
		serial, ok = a.(*asserts.Serial)
		if !ok {
			return fmt.Errorf("internal error: retrieved serial assertion has wrong type")
		}
	}

	// TODO: (possibly refetch brand key and) verify serial assertion!
	// TODO: double check brand, model and key hash

	device.Serial = serial.Serial()
	auth.SetDevice(st, device)
	t.SetStatus(state.DoneStatus)
	return nil
}
