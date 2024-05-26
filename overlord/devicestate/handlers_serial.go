// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/proxyconf"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

func baseURL() *url.URL {
	if snapdenv.UseStagingStore() {
		return mustParse("https://api.staging.snapcraft.io/")
	}
	return mustParse("https://api.snapcraft.io/")
}

func mustParse(s string) *url.URL {
	u := mylog.Check2(url.Parse(s))

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

	// we accept a stream with the serial assertion as well
	registrationCapabilities = []string{"serial-stream"}
)

func (m *DeviceManager) doGenerateDeviceKey(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	device := mylog.Check2(m.device())

	if device.KeyID != "" {
		// nothing to do
		return nil
	}

	st.Unlock()
	var keyPair *rsa.PrivateKey
	timings.Run(perfTimings, "generate-rsa-key", "generating device key pair", func(tm timings.Measurer) {
		keyPair = mylog.Check2(generateRSAKey(keyLength))
	})
	st.Lock()

	privKey := asserts.RSAPrivateKey(keyPair)
	mylog.Check(m.withKeypairMgr(func(keypairMgr asserts.KeypairManager) error {
		return keypairMgr.Put(privKey)
	}))

	device.KeyID = privKey.PublicKey().ID()
	mylog.Check(m.setDevice(device))

	t.SetStatus(state.DoneStatus)
	return nil
}

func newEnoughProxy(st *state.State, proxyURL *url.URL, client *http.Client) (bool, error) {
	st.Unlock()
	defer st.Lock()

	const prefix = "cannot check whether proxy store supports a custom serial vault"

	req := mylog.Check2(http.NewRequest("HEAD", proxyURL.String(), nil))

	req.Header.Set("User-Agent", snapdenv.UserAgent())
	resp := mylog.Check2(client.Do(req))

	// some sort of network or protocol error

	resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, fmt.Errorf(prefix+": Head request returned %s.", resp.Status)
	}
	verstr := resp.Header.Get("Snap-Store-Version")
	ver := mylog.Check2(strconv.Atoi(verstr))

	return ver >= 6, nil
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
		// talking directly to the custom device service
		cfg.serialRequestURL = base.ResolveReference(serialRef).String()
	} else {
		cfg.serialRequestURL = base.ResolveReference(devicesRef).String()
	}
}

// A registrationContext handles the contextual information needed
// for the initial registration or a re-registration.
type registrationContext interface {
	Device() (*auth.DeviceState, error)

	Model() *asserts.Model

	GadgetForSerialRequestConfig() string
	SerialRequestExtraHeaders() map[string]interface{}
	SerialRequestAncillaryAssertions() []asserts.Assertion

	FinishRegistration(serial *asserts.Serial) error

	ForRemodeling() bool
}

// initialRegistrationContext is a thin wrapper around DeviceManager
// implementing registrationContext for initial regitration
type initialRegistrationContext struct {
	deviceMgr *DeviceManager

	model *asserts.Model
}

func (rc *initialRegistrationContext) ForRemodeling() bool {
	return false
}

func (rc *initialRegistrationContext) Device() (*auth.DeviceState, error) {
	return rc.deviceMgr.device()
}

func (rc *initialRegistrationContext) Model() *asserts.Model {
	return rc.model
}

func (rc *initialRegistrationContext) GadgetForSerialRequestConfig() string {
	return rc.model.Gadget()
}

func (rc *initialRegistrationContext) SerialRequestExtraHeaders() map[string]interface{} {
	return nil
}

func (rc *initialRegistrationContext) SerialRequestAncillaryAssertions() []asserts.Assertion {
	return []asserts.Assertion{rc.model}
}

func (rc *initialRegistrationContext) FinishRegistration(serial *asserts.Serial) error {
	device := mylog.Check2(rc.deviceMgr.device())

	device.Serial = serial.Serial()
	mylog.Check(rc.deviceMgr.setDevice(device))

	rc.deviceMgr.markRegistered()

	// make sure we timely consider anything that was blocked on
	// registration
	rc.deviceMgr.state.EnsureBefore(0)

	return nil
}

// registrationCtx returns a registrationContext appropriate for the task and its change.
func (m *DeviceManager) registrationCtx(t *state.Task) (registrationContext, error) {
	remodCtx := mylog.Check2(remodelCtxFromTask(t))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if regCtx, ok := remodCtx.(registrationContext); ok {
		return regCtx, nil
	}
	model := mylog.Check2(m.Model())

	return &initialRegistrationContext{
		deviceMgr: m,
		model:     model,
	}, nil
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
		mylog.Check(dec.Decode(&srvErr))
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

func prepareSerialRequest(t *state.Task, regCtx registrationContext, privKey asserts.PrivateKey, device *auth.DeviceState, client *http.Client, cfg *serialRequestConfig) (string, error) {
	// limit tentatives starting from scratch before going to
	// slower full retries
	var nTentatives int
	mylog.Check(t.Get("pre-poll-tentatives", &nTentatives))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	nTentatives++
	t.Set("pre-poll-tentatives", nTentatives)

	st := t.State()
	st.Unlock()
	defer st.Lock()

	req := mylog.Check2(http.NewRequest("POST", cfg.requestIDURL, nil))

	req.Header.Set("User-Agent", snapdenv.UserAgent())
	cfg.applyHeaders(req)

	resp := mylog.Check2(client.Do(req))

	// If there is no network there is no need to count
	// this as a tentatives attempt. If we do it this
	// way the risk is that we tried a bunch of times
	// with no network and if we hit the server for real
	// and it replies with something we need to retry
	// we will not because nTentatives is way over the
	// limit.

	// Retry quickly if there is no network
	// (yet). This ensures that we try to get a serial
	// as soon as the user configured the network of the
	// device

	// If the cert is expired/not-valid yet that
	// most likely means that the devices has no
	// ntp-synced time yet. We will retry for up
	// to 2048s (timesyncd.conf(5) says the
	// maximum poll time is 2048s which is
	// 34min8s). With retry of 60s the below adds
	// up to 37.5m.

	// a non temporary net error fully errors out and triggers a retry
	// retries

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", retryBadStatus(t, nTentatives, "cannot retrieve request-id for making a request for a serial", resp)
	}

	dec := json.NewDecoder(resp.Body)
	var requestID requestIDResp
	mylog.Check(dec.Decode(&requestID))
	// assume broken i/o

	encodedPubKey := mylog.Check2(asserts.EncodePublicKey(privKey.PublicKey()))

	headers := map[string]interface{}{
		"brand-id":   device.Brand,
		"model":      device.Model,
		"request-id": requestID.RequestID,
		"device-key": string(encodedPubKey),
	}
	if cfg.proposedSerial != "" {
		headers["serial"] = cfg.proposedSerial
	}

	for k, v := range regCtx.SerialRequestExtraHeaders() {
		headers[k] = v
	}

	serialReq := mylog.Check2(asserts.SignWithoutAuthority(asserts.SerialRequestType, headers, cfg.body, privKey))

	buf := new(bytes.Buffer)
	encoder := asserts.NewEncoder(buf)
	mylog.Check(encoder.Encode(serialReq))

	for _, ancillaryAs := range regCtx.SerialRequestAncillaryAssertions() {
		mylog.Check(encoder.Encode(ancillaryAs))
	}

	return buf.String(), nil
}

var errPoll = errors.New("serial-request accepted, poll later")

func submitSerialRequest(t *state.Task, serialRequest string, client *http.Client, cfg *serialRequestConfig) (*asserts.Serial, *asserts.Batch, error) {
	st := t.State()
	st.Unlock()
	defer st.Lock()

	req := mylog.Check2(http.NewRequest("POST", cfg.serialRequestURL, bytes.NewBufferString(serialRequest)))

	req.Header.Set("User-Agent", snapdenv.UserAgent())
	req.Header.Set("Snap-Device-Capabilities", strings.Join(registrationCapabilities, " "))
	cfg.applyHeaders(req)
	req.Header.Set("Content-Type", asserts.MediaType)

	resp := mylog.Check2(client.Do(req))

	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 201:
	case 202:
		return nil, nil, errPoll
	default:
		return nil, nil, retryBadStatus(t, 0, "cannot deliver device serial request", resp)
	}

	var serial *asserts.Serial
	var batch *asserts.Batch
	// decode body with stream of assertions, of which one is the serial
	dec := asserts.NewDecoder(resp.Body)
	for {
		got, err := dec.Decode()
		if err == io.EOF {
			break
		}
		// assume broken i/o

		if got.Type() == asserts.SerialType {
			if serial != nil {
				return nil, nil, fmt.Errorf("cannot accept more than a single device serial assertion from the device service")
			}
			serial = got.(*asserts.Serial)
		} else {
			if batch == nil {
				batch = asserts.NewBatch(nil)
			}
			mylog.Check(batch.Add(got))

		}
		// TODO: consider a size limit?
	}

	if serial == nil {
		return nil, nil, fmt.Errorf("cannot proceed, received assertion stream from the device service missing device serial assertion")
	}

	return serial, batch, nil
}

var httputilNewHTTPClient = httputil.NewHTTPClient

var errStoreOffline = errors.New("snap store is marked offline")

func getSerial(t *state.Task, regCtx registrationContext, privKey asserts.PrivateKey, device *auth.DeviceState, tm timings.Measurer) (serial *asserts.Serial, ancillaryBatch *asserts.Batch, err error) {
	var serialSup serialSetup
	mylog.Check(t.Get("serial-setup", &serialSup))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, err
	}

	if serialSup.Serial != "" {
		// we got a serial, just haven't managed to save its info yet
		a := mylog.Check2(asserts.Decode([]byte(serialSup.Serial)))

		return a.(*asserts.Serial), nil, nil
	}

	st := t.State()

	shouldRequest := mylog.Check2(shouldRequestSerial(st, regCtx.GadgetForSerialRequestConfig()))

	if !shouldRequest {
		return nil, nil, errStoreOffline
	}

	proxyConf := proxyconf.New(st)
	client := httputilNewHTTPClient(&httputil.ClientOptions{
		Timeout:            30 * time.Second,
		MayLogBody:         true,
		Proxy:              proxyConf.Conf,
		ProxyConnectHeader: http.Header{"User-Agent": []string{snapdenv.UserAgent()}},
		ExtraSSLCerts: &httputil.ExtraSSLCertsFromDir{
			Dir: dirs.SnapdStoreSSLCertsDir,
		},
	})

	cfg := mylog.Check2(getSerialRequestConfig(t, regCtx, client))

	// NB: until we get at least an Accepted (202) we need to
	// retry from scratch creating a new request-id because the
	// previous one used could have expired

	if serialSup.SerialRequest == "" {
		var serialRequest string

		timings.Run(tm, "prepare-serial-request", "prepare device serial request", func(timings.Measurer) {
			serialRequest = mylog.Check2(prepareSerialRequest(t, regCtx, privKey, device, client, cfg))
		})
		// errors & retries

		serialSup.SerialRequest = serialRequest
	}

	timings.Run(tm, "submit-serial-request", "submit device serial request", func(timings.Measurer) {
		serial, ancillaryBatch = mylog.Check3(submitSerialRequest(t, serialSup.SerialRequest, client, cfg))
	})
	if err == errPoll {
		// we can/should reuse the serial-request
		t.Set("serial-setup", serialSup)
		return nil, nil, errPoll
	}
	// errors & retries

	keyID := privKey.PublicKey().ID()
	if serial.BrandID() != device.Brand || serial.Model() != device.Model || serial.DeviceKey().ID() != keyID {
		return nil, nil, fmt.Errorf("obtained serial assertion does not match provided device identity information (brand, model, key id): %s / %s / %s != %s / %s / %s", serial.BrandID(), serial.Model(), serial.DeviceKey().ID(), device.Brand, device.Model, keyID)
	}

	// cross check authority if different from brand-id
	if serial.BrandID() != serial.AuthorityID() {
		model := regCtx.Model()
		if !strutil.ListContains(model.SerialAuthority(), serial.AuthorityID()) {
			return nil, nil, fmt.Errorf("obtained serial assertion is signed by authority %q different from brand %q without model assertion with serial-authority set to to allow for them", serial.AuthorityID(), serial.BrandID())
		}
	}

	if ancillaryBatch == nil {
		serialSup.Serial = string(asserts.Encode(serial))
		t.Set("serial-setup", serialSup)
	}

	if repeatRequestSerial == "after-got-serial" {
		// For testing purposes, ensure a crash in this state works.
		return nil, nil, &state.Retry{}
	}

	return serial, ancillaryBatch, nil
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

func getSerialRequestConfig(t *state.Task, regCtx registrationContext, client *http.Client) (*serialRequestConfig, error) {
	var svcURL, proxyURL *url.URL

	st := t.State()
	tr := config.NewTransaction(st)
	if proxyStore := mylog.Check2(proxyStore(st, tr)); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	} else if proxyStore != nil {
		proxyURL = proxyStore.URL()
	}

	cfg := serialRequestConfig{}

	gadgetName := regCtx.GadgetForSerialRequestConfig()
	// gadget is optional on classic
	if gadgetName != "" {
		var gadgetSt snapstate.SnapState
		mylog.Check(snapstate.Get(st, gadgetName, &gadgetSt))

		var svcURI string
		mylog.Check(tr.GetMaybe(gadgetName, "device-service.url", &svcURI))

		if svcURI != "" {
			svcURL = mylog.Check2(url.Parse(svcURI))

			if !strings.HasSuffix(svcURL.Path, "/") {
				svcURL.Path += "/"
			}
		}
		mylog.Check(tr.GetMaybe(gadgetName, "device-service.headers", &cfg.headers))

		var bodyStr string
		mylog.Check(tr.GetMaybe(gadgetName, "registration.body", &bodyStr))

		cfg.body = []byte(bodyStr)
		mylog.Check(tr.GetMaybe(gadgetName, "registration.proposed-serial", &cfg.proposedSerial))

	}

	if proxyURL != nil && svcURL != nil {
		newEnough := mylog.Check2(newEnoughProxy(st, proxyURL, client))

		// Ignore the proxy on any error for
		// compatibility with previous versions of
		// snapd.
		//
		// TODO: provide a way for the users to specify
		// if they want to use the proxy store for their
		// device-service.url or not. This needs design.
		// (see LP:#2023166)

		if !newEnough {
			logger.Noticef("Proxy store does not support custom serial vault; ignoring the proxy")
			proxyURL = nil
		}
	}

	cfg.setURLs(proxyURL, svcURL)

	return &cfg, nil
}

func shouldRequestSerial(s *state.State, gadgetName string) (bool, error) {
	tr := config.NewTransaction(s)

	var storeAccess string
	mylog.Check(tr.GetMaybe("core", "store.access", &storeAccess))

	// if there isn't a gadget, just use store.access to determine if we should
	// request
	if gadgetName == "" {
		return storeAccess != "offline", nil
	}

	var deviceServiceAccess string
	mylog.Check(tr.GetMaybe(gadgetName, "device-service.access", &deviceServiceAccess))

	// if we have a gadget and device-service.access is set to offline, then we
	// will not request a serial
	if deviceServiceAccess == "offline" {
		return false, nil
	}

	var deviceServiceURL string
	mylog.Check(tr.GetMaybe(gadgetName, "device-service.url", &deviceServiceURL))

	// if we'd be using the fallback device-service.url (which is the store),
	// then use store.access to determine if we should request
	if deviceServiceURL == "" {
		return storeAccess != "offline", nil
	}

	return true, nil
}

func (m *DeviceManager) doRequestSerial(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	regCtx := mylog.Check2(m.registrationCtx(t))

	device := mylog.Check2(regCtx.Device())

	// NB: the keyPair is fixed for now
	privKey := mylog.Check2(m.keyPair())
	if errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: cannot find device key pair")
	}

	// make this idempotent, look if we have already a serial assertion
	// for privKey
	serials := mylog.Check2(assertstate.DB(st).FindMany(asserts.SerialType, map[string]string{
		"brand-id":            device.Brand,
		"model":               device.Model,
		"device-key-sha3-384": privKey.PublicKey().ID(),
	}))
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return err
	}

	finish := func(serial *asserts.Serial) error {
		mylog.
			// save serial if appropriate into the device save
			// assertion database
			Check(m.withSaveAssertDB(func(savedb *asserts.Database) error {
				db := assertstate.DB(st)
				retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
					return ref.Resolve(db.Find)
				}
				b := asserts.NewBatch(nil)
				mylog.Check(b.Fetch(savedb, retrieve, func(f asserts.Fetcher) error {
					mylog.Check(
						// save the associated model as well
						// as it might be required for cross-checks
						// of the serial
						f.Save(regCtx.Model()))

					return f.Save(serial)
				}))

				return b.CommitTo(savedb, nil)
			}))
		if err != nil && err != errNoSaveSupport {
			return fmt.Errorf("cannot save serial to device save assertion database: %v", err)
		}
		mylog.Check(regCtx.FinishRegistration(serial))

		t.SetStatus(state.DoneStatus)
		return nil
	}

	if len(serials) == 1 {
		// means we saved the assertion but didn't get to the end of the task
		return finish(serials[0].(*asserts.Serial))
	}
	if len(serials) > 1 {
		return fmt.Errorf("internal error: multiple serial assertions for the same device key")
	}

	var serial *asserts.Serial
	var ancillaryBatch *asserts.Batch
	timings.Run(perfTimings, "get-serial", "get device serial", func(tm timings.Measurer) {
		serial, ancillaryBatch = mylog.Check3(getSerial(t, regCtx, privKey, device, tm))
	})
	if err == errPoll {
		t.Logf("Will poll for device serial assertion in 60 seconds")
		return &state.Retry{After: retryInterval}
	}
	// errors & retries

	// TODO: the accept* helpers put the serial directly in the
	// system assertion database, that will not work
	// for 3rd-party signed serials in the case of a remodel
	// because the model is added only later. If needed, the best way
	// to fix this requires rethinking how remodel and new assertions
	// interact
	if ancillaryBatch == nil {
		mylog.Check(
			// the device service returned only the serial
			acceptSerialOnly(t, serial, perfTimings))
	} else {
		// the device service returned a stream of assertions
		timings.Run(perfTimings, "fetch-keys", "fetch signing key chain", func(timings.Measurer) {
			mylog.Check(acceptSerialPlusBatch(t, serial, ancillaryBatch))
		})
	}

	if repeatRequestSerial == "after-add-serial" {
		// For testing purposes, ensure a crash in this state works.
		return &state.Retry{}
	}

	return finish(serial)
}

func acceptSerialOnly(t *state.Task, serial *asserts.Serial, perfTimings *timings.Timings) error {
	st := t.State()

	var errAcctKey error
	// try to fetch the signing key chain of the serial
	timings.Run(perfTimings, "fetch-keys", "fetch signing key chain", func(timings.Measurer) {
		errAcctKey = mylog.Check2(fetchKeys(st, serial.SignKeyID()))
	})
	mylog.Check(

		// add the serial assertion to the system assertion db
		assertstate.Add(st, serial))

	// if we had failed to fetch the signing key, retry in a bit

	return nil
}

func acceptSerialPlusBatch(t *state.Task, serial *asserts.Serial, batch *asserts.Batch) error {
	st := t.State()
	mylog.Check(batch.Add(serial))

	return assertstate.AddBatch(st, batch, &asserts.CommitOptions{Precheck: true})
}

var repeatRequestSerial string // for tests

func fetchKeys(st *state.State, keyID string) (errAcctKey error, err error) {
	// TODO: right now any store should be good enough here but
	// that might change. As an alternative we do support
	// receiving a stream with any relevant assertions.
	sto := snapstate.Store(st, nil)
	db := assertstate.DB(st)

	retrieveError := false
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		st.Unlock()
		defer st.Lock()
		a := mylog.Check2(sto.Assertion(ref.Type, ref.PrimaryKey, nil))
		retrieveError = err != nil
		return a, err
	}

	save := func(a asserts.Assertion) error {
		mylog.Check(assertstate.Add(st, a))
		if err != nil && !asserts.IsUnaccceptedUpdate(err) {
			return err
		}
		return nil
	}

	f := asserts.NewFetcher(db, retrieve, save)

	keyRef := &asserts.Ref{
		Type:       asserts.AccountKeyType,
		PrimaryKey: []string{keyID},
	}
	mylog.Check(f.Fetch(keyRef))

	return nil, nil
}
