// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package devicestate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func (m *DeviceManager) doMarkSeeded(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	st.Set("seed-time", time.Now())
	st.Set("seeded", true)
	// make sure we setup a fallback model/consider the next phase
	// (registration) timely
	st.EnsureBefore(0)
	return nil
}

func useStaging() bool {
	return osutil.GetenvBool("SNAPPY_USE_STAGING_STORE")
}

func baseURL() *url.URL {
	if useStaging() {
		return mustParse("https://api.staging.snapcraft.io/")
	}
	return mustParse("https://api.snapcraft.io/")
}

func mustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

var (
	keyLength     = 4096
	retryInterval = 60 * time.Second
	maxTentatives = 15
	baseStoreURL  = baseURL().ResolveReference(authRef)

	authRef    = mustParse("api/v1/snaps/auth/") // authRef must end in / for the following refs to work
	reqIdRef   = mustParse("request-id")
	serialRef  = mustParse("serial")
	devicesRef = mustParse("devices")
)

func newEnoughProxy(proxyURL *url.URL, client *http.Client) bool {
	const prefix = "Cannot check whether proxy store supports a custom serial vault"

	req, err := http.NewRequest("HEAD", proxyURL.String(), nil)
	if err != nil {
		// can't really happen unless proxyURL is somehow broken
		logger.Debugf(prefix+": %v", err)
		return false
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		// some sort of network or protocol error
		logger.Debugf(prefix+": %v", err)
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		logger.Debugf(prefix+": Head request returned %s.", resp.Status)
		return false
	}
	verstr := resp.Header.Get("Snap-Store-Version")
	ver, err := strconv.Atoi(verstr)
	if err != nil {
		logger.Debugf(prefix+": Bogus Snap-Store-Version header %q.", verstr)
		return false
	}
	return ver >= 6
}

func (cfg *serialRequestConfig) setURLs(proxyURL, svcURL *url.URL) {
	base := baseStoreURL
	if proxyURL != nil {
		if svcURL != nil {
			if cfg.headers == nil {
				cfg.headers = make(map[string]string, 1)
			}
			cfg.headers["X-Snap-Device-Service-URL"] = svcURL.String()
		}
		base = proxyURL.ResolveReference(authRef)
	} else if svcURL != nil {
		base = svcURL
	}

	cfg.requestIDURL = base.ResolveReference(reqIdRef).String()
	if svcURL != nil && proxyURL == nil {
		cfg.serialRequestURL = base.ResolveReference(serialRef).String()
	} else {
		cfg.serialRequestURL = base.ResolveReference(devicesRef).String()
	}
}

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

	st.Unlock()
	keyPair, err := generateRSAKey(keyLength)
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot generate device key pair: %v", err)
	}

	privKey := asserts.RSAPrivateKey(keyPair)
	err = m.keypairMgr.Put(privKey)
	if err != nil {
		return fmt.Errorf("cannot store device key pair: %v", err)
	}

	device.KeyID = privKey.PublicKey().ID()
	err = auth.SetDevice(st, device)
	if err != nil {
		return err
	}
	t.SetStatus(state.DoneStatus)
	return nil
}

type serialSetup struct {
	SerialRequest string `json:"serial-request"`
	Serial        string `json:"serial"`
}

type requestIDResp struct {
	RequestID string `json:"request-id"`
}

func retryErr(t *state.Task, nTentatives int, reason string, a ...interface{}) error {
	t.State().Lock()
	defer t.State().Unlock()
	if nTentatives >= maxTentatives {
		return fmt.Errorf(reason, a...)
	}
	t.Errorf(reason, a...)
	return &state.Retry{After: retryInterval}
}

type serverError struct {
	Message string         `json:"message"`
	Errors  []*serverError `json:"error_list"`
}

func retryBadStatus(t *state.Task, nTentatives int, reason string, resp *http.Response) error {
	if resp.StatusCode > 500 {
		// likely temporary
		return retryErr(t, nTentatives, "%s: unexpected status %d", reason, resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") == "application/json" {
		var srvErr serverError
		dec := json.NewDecoder(resp.Body)
		err := dec.Decode(&srvErr)
		if err == nil {
			msg := srvErr.Message
			if msg == "" && len(srvErr.Errors) > 0 {
				msg = srvErr.Errors[0].Message
			}
			if msg != "" {
				return fmt.Errorf("%s: %s", reason, msg)
			}
		}
	}
	return fmt.Errorf("%s: unexpected status %d", reason, resp.StatusCode)
}

func prepareSerialRequest(t *state.Task, privKey asserts.PrivateKey, device *auth.DeviceState, client *http.Client, cfg *serialRequestConfig) (string, error) {
	// limit tentatives starting from scratch before going to
	// slower full retries
	var nTentatives int
	err := t.Get("pre-poll-tentatives", &nTentatives)
	if err != nil && err != state.ErrNoState {
		return "", err
	}
	nTentatives++
	t.Set("pre-poll-tentatives", nTentatives)

	st := t.State()
	st.Unlock()
	defer st.Lock()

	req, err := http.NewRequest("POST", cfg.requestIDURL, nil)
	if err != nil {
		return "", fmt.Errorf("internal error: cannot create request-id request %q", cfg.requestIDURL)
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	cfg.applyHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && !netErr.Temporary() {
			// a non temporary net error, like a DNS no
			// host, error out and do full retries
			return "", fmt.Errorf("cannot retrieve request-id for making a request for a serial: %v", err)
		}
		return "", retryErr(t, nTentatives, "cannot retrieve request-id for making a request for a serial: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", retryBadStatus(t, nTentatives, "cannot retrieve request-id for making a request for a serial", resp)
	}

	dec := json.NewDecoder(resp.Body)
	var requestID requestIDResp
	err = dec.Decode(&requestID)
	if err != nil { // assume broken i/o
		return "", retryErr(t, nTentatives, "cannot read response with request-id for making a request for a serial: %v", err)
	}

	encodedPubKey, err := asserts.EncodePublicKey(privKey.PublicKey())
	if err != nil {
		return "", fmt.Errorf("internal error: cannot encode device public key: %v", err)

	}

	headers := map[string]interface{}{
		"brand-id":   device.Brand,
		"model":      device.Model,
		"request-id": requestID.RequestID,
		"device-key": string(encodedPubKey),
	}
	if cfg.proposedSerial != "" {
		headers["serial"] = cfg.proposedSerial
	}

	serialReq, err := asserts.SignWithoutAuthority(asserts.SerialRequestType, headers, cfg.body, privKey)
	if err != nil {
		return "", err
	}

	return string(asserts.Encode(serialReq)), nil
}

var errPoll = errors.New("serial-request accepted, poll later")

func submitSerialRequest(t *state.Task, serialRequest string, client *http.Client, cfg *serialRequestConfig) (*asserts.Serial, error) {
	st := t.State()
	st.Unlock()
	defer st.Lock()

	req, err := http.NewRequest("POST", cfg.serialRequestURL, bytes.NewBufferString(serialRequest))
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot create serial-request request %q", cfg.serialRequestURL)
	}
	req.Header.Set("User-Agent", httputil.UserAgent())
	cfg.applyHeaders(req)
	req.Header.Set("Content-Type", asserts.MediaType)

	resp, err := client.Do(req)
	if err != nil {
		return nil, retryErr(t, 0, "cannot deliver device serial request: %v", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 201:
	case 202:
		return nil, errPoll
	default:
		return nil, retryBadStatus(t, 0, "cannot deliver device serial request", resp)
	}

	// TODO: support a stream of assertions instead of just the serial

	// decode body with serial assertion
	dec := asserts.NewDecoder(resp.Body)
	got, err := dec.Decode()
	if err != nil { // assume broken i/o
		return nil, retryErr(t, 0, "cannot read response to request for a serial: %v", err)
	}

	serial, ok := got.(*asserts.Serial)
	if !ok {
		return nil, fmt.Errorf("cannot use device serial assertion of type %q", got.Type().Name)
	}

	return serial, nil
}

func getSerial(t *state.Task, privKey asserts.PrivateKey, device *auth.DeviceState) (*asserts.Serial, error) {
	var serialSup serialSetup
	err := t.Get("serial-setup", &serialSup)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if serialSup.Serial != "" {
		// we got a serial, just haven't managed to save its info yet
		a, err := asserts.Decode([]byte(serialSup.Serial))
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot decode previously saved serial: %v", err)
		}
		return a.(*asserts.Serial), nil
	}

	client := httputil.NewHTTPClient(&httputil.ClientOpts{
		Timeout:    30 * time.Second,
		MayLogBody: true,
	})

	cfg, err := getSerialRequestConfig(t, client)
	if err != nil {
		return nil, err
	}

	// NB: until we get at least an Accepted (202) we need to
	// retry from scratch creating a new request-id because the
	// previous one used could have expired

	if serialSup.SerialRequest == "" {
		serialRequest, err := prepareSerialRequest(t, privKey, device, client, cfg)
		if err != nil { // errors & retries
			return nil, err
		}

		serialSup.SerialRequest = serialRequest
	}

	serial, err := submitSerialRequest(t, serialSup.SerialRequest, client, cfg)
	if err == errPoll {
		// we can/should reuse the serial-request
		t.Set("serial-setup", serialSup)
		return nil, errPoll
	}
	if err != nil { // errors & retries
		return nil, err
	}

	keyID := privKey.PublicKey().ID()
	if serial.BrandID() != device.Brand || serial.Model() != device.Model || serial.DeviceKey().ID() != keyID {
		return nil, fmt.Errorf("obtained serial assertion does not match provided device identity information (brand, model, key id): %s / %s / %s != %s / %s / %s", serial.BrandID(), serial.Model(), serial.DeviceKey().ID(), device.Brand, device.Model, keyID)
	}

	serialSup.Serial = string(asserts.Encode(serial))
	t.Set("serial-setup", serialSup)

	if repeatRequestSerial == "after-got-serial" {
		// For testing purposes, ensure a crash in this state works.
		return nil, &state.Retry{}
	}

	return serial, nil
}

type serialRequestConfig struct {
	requestIDURL     string
	serialRequestURL string
	headers          map[string]string
	proposedSerial   string
	body             []byte
}

func (cfg *serialRequestConfig) applyHeaders(req *http.Request) {
	for k, v := range cfg.headers {
		req.Header.Set(k, v)
	}
}

func getSerialRequestConfig(t *state.Task, client *http.Client) (*serialRequestConfig, error) {
	var svcURL, proxyURL *url.URL

	st := t.State()
	tr := config.NewTransaction(st)
	if proxyStore, err := proxyStore(st, tr); err != nil && err != state.ErrNoState {
		return nil, err
	} else if proxyStore != nil {
		proxyURL = proxyStore.URL()
	}

	// gadget is optional on classic
	model, err := Model(st)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	cfg := serialRequestConfig{}

	if model != nil && model.Gadget() != "" {
		// model specifies a gadget
		gadgetInfo, err := snapstate.GadgetInfo(st)
		if err != nil {
			return nil, fmt.Errorf("cannot find gadget snap and its name: %v", err)
		}
		gadgetName := gadgetInfo.InstanceName()

		var svcURI string
		err = tr.GetMaybe(gadgetName, "device-service.url", &svcURI)
		if err != nil {
			return nil, err
		}

		if svcURI != "" {
			svcURL, err = url.Parse(svcURI)
			if err != nil {
				return nil, fmt.Errorf("cannot parse device registration base URL %q: %v", svcURI, err)
			}
			if !strings.HasSuffix(svcURL.Path, "/") {
				svcURL.Path += "/"
			}
		}

		err = tr.GetMaybe(gadgetName, "device-service.headers", &cfg.headers)
		if err != nil {
			return nil, err
		}

		var bodyStr string
		err = tr.GetMaybe(gadgetName, "registration.body", &bodyStr)
		if err != nil {
			return nil, err
		}

		cfg.body = []byte(bodyStr)

		err = tr.GetMaybe(gadgetName, "registration.proposed-serial", &cfg.proposedSerial)
		if err != nil {
			return nil, err
		}
	}

	if proxyURL != nil && svcURL != nil && !newEnoughProxy(proxyURL, client) {
		logger.Noticef("Proxy store does not support custom serial vault; ignoring the proxy")
		proxyURL = nil
	}

	cfg.setURLs(proxyURL, svcURL)

	return &cfg, nil
}

func (m *DeviceManager) finishRegistration(t *state.Task, device *auth.DeviceState, serial *asserts.Serial) error {
	device.Serial = serial.Serial()
	err := auth.SetDevice(t.State(), device)
	if err != nil {
		return err
	}
	m.markRegistered()
	t.SetStatus(state.DoneStatus)
	return nil
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
	if err == state.ErrNoState {
		return fmt.Errorf("internal error: cannot find device key pair")
	}
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
	if err != nil && !asserts.IsNotFound(err) {
		return err
	}

	if len(serials) == 1 {
		// means we saved the assertion but didn't get to the end of the task
		return m.finishRegistration(t, device, serials[0].(*asserts.Serial))
	}
	if len(serials) > 1 {
		return fmt.Errorf("internal error: multiple serial assertions for the same device key")
	}

	serial, err := getSerial(t, privKey, device)
	if err == errPoll {
		t.Logf("Will poll for device serial assertion in 60 seconds")
		return &state.Retry{After: retryInterval}
	}
	if err != nil { // errors & retries
		return err
	}

	// try to fetch the signing key chain of the serial
	errAcctKey, err := fetchKeys(st, serial.SignKeyID())
	if err != nil {
		return err
	}

	// add the serial assertion to the system assertion db
	err = assertstate.Add(st, serial)
	if err != nil {
		// if we had failed to fetch the signing key, retry in a bit
		if errAcctKey != nil {
			t.Errorf("cannot fetch signing key for the serial: %v", errAcctKey)
			return &state.Retry{After: retryInterval}
		}
		return err
	}

	if repeatRequestSerial == "after-add-serial" {
		// For testing purposes, ensure a crash in this state works.
		return &state.Retry{}
	}

	return m.finishRegistration(t, device, serial)
}

var repeatRequestSerial string // for tests

func fetchKeys(st *state.State, keyID string) (errAcctKey error, err error) {
	sto := snapstate.Store(st)
	db := assertstate.DB(st)
	for {
		_, err := db.FindPredefined(asserts.AccountKeyType, map[string]string{
			"public-key-sha3-384": keyID,
		})
		if err == nil {
			return nil, nil
		}
		if !asserts.IsNotFound(err) {
			return nil, err
		}
		st.Unlock()
		a, errAcctKey := sto.Assertion(asserts.AccountKeyType, []string{keyID}, nil)
		st.Lock()
		if errAcctKey != nil {
			return errAcctKey, nil
		}
		err = assertstate.Add(st, a)
		if err != nil {
			if !asserts.IsUnaccceptedUpdate(err) {
				return nil, err
			}
		}
		keyID = a.SignKeyID()
	}
}
