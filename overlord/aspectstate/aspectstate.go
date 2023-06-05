// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

package aspectstate

import (
	"errors"
	"sync"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord/aspectstate/aspecttest"
	"github.com/snapcore/snapd/overlord/state"
)

// BaseHijacker implements the aspects.DataBag so derivations of it can implement
// only the methods that are intended for usage.
type BaseHijacker struct{}

func (BaseHijacker) Get(string, interface{}) error {
	return errors.New("Get() method is not implemented")
}

func (BaseHijacker) Set(string, interface{}) error {
	return errors.New("Set() method is not implemented")
}

func (BaseHijacker) Data() ([]byte, error) {
	// hijacker data isn't written to state but return something to pass the schema
	return []byte("{}"), nil
}

var hijackedAspects = &hijackedStore{
	hijackedAspects: make(map[[3]string]aspects.DataBag),
}

type hijackedStore struct {
	hijackedAspects map[[3]string]aspects.DataBag
	mu              sync.RWMutex
}

func (s *hijackedStore) hijack(account, bundleName, aspect string, hijacker aspects.DataBag) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hijackedAspects[[3]string{account, bundleName, aspect}] = hijacker
}

func (s *hijackedStore) get(account, bundleName, aspect string) aspects.DataBag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hijackedAspects[[3]string{account, bundleName, aspect}]
}

func (s *hijackedStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k := range s.hijackedAspects {
		delete(s.hijackedAspects, k)
	}
}

// Hijack register a hijacker that will be called when a get/set is requested
// for the provided input combination. The hijacker must implement the DataBag
// interface its Get/Set methods are called transparently.
//
// TODO: permit hijacking only get/sets instead of both?
func Hijack(account, bundleName, aspect string, hijacker aspects.DataBag) {
	hijackedAspects.hijack(account, bundleName, aspect, hijacker)
}

// Set finds the aspect identified by the account, bundleName and aspect and sets
// the specified field to the supplied value.
func Set(st *state.State, account, bundleName, aspect, field string, value interface{}) error {
	st.Lock()
	defer st.Unlock()

	databag := hijackedAspects.get(account, bundleName, aspect)
	hijacked := databag != nil

	if !hijacked {
		var err error
		databag, err = getDatabag(st, account, bundleName)
		if err != nil {
			if !errors.Is(err, state.ErrNoState) {
				return err
			}

			databag = aspects.NewJSONDataBag()
		}
	}

	accPatterns, err := aspecttest.GetAspectAssertion(account, bundleName)
	if err != nil {
		if errors.Is(err, &aspecttest.NotFound{}) {
			return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect, BaseErr: err}
		}
		return err
	}

	aspectBundle, err := aspects.NewAspectBundle(bundleName, accPatterns, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect}
	}

	if err := asp.Set(databag, field, value); err != nil {
		return err
	}

	if !hijacked {
		// change updateDatabags to take a generic bag (implement json marshaler?)
		jsonDb := databag.(aspects.JSONDataBag)
		if err := updateDatabags(st, account, bundleName, jsonDb); err != nil {
			return err
		}
	}

	return nil
}

// Get finds the aspect identified by the account, bundleName and aspect and
// returns the specified field's value through the "value" output parameter.
func Get(st *state.State, account, bundleName, aspect, field string, value interface{}) error {
	st.Lock()
	defer st.Unlock()

	databag := hijackedAspects.get(account, bundleName, aspect)
	hijacked := databag != nil

	if !hijacked {
		var err error
		databag, err = getDatabag(st, account, bundleName)
		if err != nil {
			if errors.Is(err, state.ErrNoState) {
				return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect}
			}

			return err
		}
	}

	accPatterns, err := aspecttest.GetAspectAssertion(account, bundleName)
	if err != nil {
		if errors.Is(err, &aspecttest.NotFound{}) {
			return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect, BaseErr: err}
		}
		return err
	}

	aspectBundle, err := aspects.NewAspectBundle(bundleName, accPatterns, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect}
	}

	if err := asp.Get(databag, field, value); err != nil {
		return err
	}

	return nil
}

func updateDatabags(st *state.State, account, bundleName string, databag aspects.JSONDataBag) error {
	var databags map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return err
		}

		databags = map[string]map[string]aspects.JSONDataBag{
			account: {bundleName: aspects.NewJSONDataBag()},
		}
	}

	databags[account][bundleName] = databag
	st.Set("aspect-databags", databags)
	return nil
}

func getDatabag(st *state.State, account, bundleName string) (aspects.JSONDataBag, error) {
	var databags map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		return nil, err
	}
	return databags[account][bundleName], nil
}
