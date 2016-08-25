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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

// DeviceManager is responsible for managing the device identity and device
// policies.
type DeviceManager struct {
	state      *state.State
	keypairMgr asserts.KeypairManager
	runner     *state.TaskRunner
}

// Manager returns a new device manager.
func Manager(s *state.State) (*DeviceManager, error) {
	runner := state.NewTaskRunner(s)

	keypairMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	if err != nil {
		return nil, err

	}

	m := &DeviceManager{state: s, keypairMgr: keypairMgr, runner: runner}

	runner.AddHandler("generate-device-key", m.doGenerateDeviceKey, nil)
	runner.AddHandler("request-serial", m.doRequestSerial, nil)

	return m, nil
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
			// TODO: are we going to have model assertions on classic or need will need to cheat here?
			return nil
		}
		// cannot proceed yet, once first boot is done these will be set
		// and we can pick up from there
		return nil
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
	requestSerial := m.state.NewTask("request-serial", i18n.G("Request device serial"))
	requestSerial.WaitFor(genKey)

	chg := m.state.NewChange("become-operational", i18n.G("Initialize device"))
	chg.AddAll(state.NewTaskSet(genKey, requestSerial))

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

func (m *DeviceManager) doGenerateDeviceKey(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	device, err := auth.Device(st)
	if err != nil {
		return err
	}

	if device.KeyID != "" {
		// nothing to do
		return nil
	}

	keyPair, err := rsa.GenerateKey(rand.Reader, keyLength)
	if err != nil {
		return fmt.Errorf("cannot generate device key pair: %v", err)
	}

	privKey := asserts.RSAPrivateKey(keyPair)
	err = m.keypairMgr.Put(privKey)
	if err != nil {
		return fmt.Errorf("cannot store device key pair: %v", err)
	}

	device.KeyID = privKey.PublicKey().ID()
	auth.SetDevice(st, device)
	t.SetStatus(state.DoneStatus)
	return nil
}

func (m *DeviceManager) keyPair() (asserts.PrivateKey, error) {
	device, err := auth.Device(m.state)
	if err != nil {
		return nil, err
	}

	if device.KeyID == "" {
		return nil, fmt.Errorf("internal error: cannot find device key pair")
	}

	privKey, err := m.keypairMgr.Get(device.KeyID)
	if err != nil {
		return nil, fmt.Errorf("cannot read device key pair: %v", err)
	}
	return privKey, nil
}

type serialSetup struct {
	SerialRequest string `json:"serial-request"`
}

type requestIDResp struct {
	RequestID string `json:"request-id"`
}

func prepareSerialRequest(t *state.Task, privKey asserts.PrivateKey, device *auth.DeviceState, client *http.Client) (string, error) {
	// TODO: see to simplify locking here
	st := t.State()
	st.Unlock()
	defer st.Lock()
	resp, err := client.Get(serialRequestURL)
	if err != nil {
		st.Lock()
		t.Errorf("cannot retrieve request-id for making a request for a serial: %v", err)
		st.Unlock()
		return "", &state.Retry{After: 60 * time.Second}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		st.Lock()
		t.Errorf("cannot retrieve request-id for making a request for a serial: unexpected status %d", resp.StatusCode)
		st.Unlock()
		return "", &state.Retry{After: 60 * time.Second}
	}

	dec := json.NewDecoder(resp.Body)
	var requestID requestIDResp
	err = dec.Decode(&requestID)
	if err != nil { // assume broken i/o
		st.Lock()
		t.Errorf("cannot read response with request-id for making a request for a serial: %v", err)
		st.Unlock()
		return "", &state.Retry{After: 60 * time.Second}
	}

	encodedPubKey, err := asserts.EncodePublicKey(privKey.PublicKey())
	if err != nil {
		return "", fmt.Errorf("internal error: cannot encode device public key: %v", err)

	}

	serialReq, err := asserts.SignWithoutAuthority(asserts.SerialRequestType, map[string]interface{}{
		"brand-id":   device.Brand,
		"model":      device.Model,
		"request-id": requestID.RequestID,
		"device-key": string(encodedPubKey),
	}, nil, privKey) // XXX: fill body with some agreed hardware details
	if err != nil {
		return "", err
	}

	return string(asserts.Encode(serialReq)), nil
}

var errPoll = errors.New("serial-request accepted, poll later")

func submitSerialRequest(t *state.Task, serialRequest string, client *http.Client) (*asserts.Serial, error) {
	// TODO: see to simplify locking here
	st := t.State()
	st.Unlock()
	defer st.Lock()
	resp, err := client.Post(serialRequestURL, asserts.MediaType, bytes.NewBufferString(serialRequest))
	if err != nil {
		st.Lock()
		t.Errorf("cannot deliver device serial request: %v", err)
		st.Unlock()
		return nil, &state.Retry{After: 60 * time.Second}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 201:
	case 202:
		return nil, errPoll
	default:
		st.Lock()
		t.Errorf("cannot deliver device serial request: unexpected status %d", resp.StatusCode)
		st.Unlock()
		return nil, &state.Retry{After: 60 * time.Second}
	}

	// decode body with serial assertion
	dec := asserts.NewDecoder(resp.Body)
	got, err := dec.Decode()
	if err != nil { // assume broken i/o
		st.Lock()
		t.Errorf("cannot read response to request for a serial: %v", err)
		st.Unlock()
		return nil, &state.Retry{After: 60 * time.Second}
	}

	serial, ok := got.(*asserts.Serial)
	if !ok {
		return nil, fmt.Errorf("cannot use device serial assertion of type %q", got.Type().Name)
	}

	return serial, nil
}

func (m *DeviceManager) doRequestSerial(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	device, err := auth.Device(st)
	if err != nil {
		return err
	}

	privKey, err := m.keyPair()
	if err != nil {
		return err
	}

	// make this idempotent, look if we have already a serial assertion
	// for privKey
	serials, err := assertstate.DB(st).FindMany(asserts.SerialType, map[string]string{
		"brand-id":            device.Brand,
		"model":               device.Model,
		"device-key-sha3-384": privKey.PublicKey().ID(),
	})

	if len(serials) == 1 {
		// means we saved the assertion but didn't get to the end of the task
		device.Serial = serials[0].(*asserts.Serial).Serial()
		auth.SetDevice(st, device)
		t.SetStatus(state.DoneStatus)
		return nil
	}
	if len(serials) > 1 {
		return fmt.Errorf("internal error: multiple serial assertions for the same device key")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	var serialSup serialSetup
	err = t.Get("serial-setup", &serialSup)
	if err != nil && err != state.ErrNoState {
		return err
	}

	// NB: until we get at least an Accepted (202) we need to
	// retry from scratch creating a new request-id because the
	// previous one used could have expired

	if serialSup.SerialRequest == "" {
		serialRequest, err := prepareSerialRequest(t, privKey, device, client)
		if err != nil { // errors & retries
			return err
		}

		serialSup.SerialRequest = serialRequest
	}

	serial, err := submitSerialRequest(t, serialSup.SerialRequest, client)
	if err == errPoll {
		// we can/should reuse the serial-request
		t.Set("serial-setup", serialSup)
		t.Logf("Will poll for device serial assertion in 60 seconds")
		return &state.Retry{After: 60 * time.Second}
	}
	if err != nil { // errors & retries
		return err
	}

	// TODO: (possibly refetch brand key and)
	// TODO: double check brand, model and key hash

	// add the serial assertion to the system assertion db
	err = assertstate.Add(st, serial)
	if err != nil {
		return err
	}

	device.Serial = serial.Serial()
	auth.SetDevice(st, device)
	t.SetStatus(state.DoneStatus)
	return nil
}
