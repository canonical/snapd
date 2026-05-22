// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package asserts

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"io"

	"golang.org/x/crypto/openpgp/packet"
	check "gopkg.in/check.v1"
)

type extKeypairMgrImplSuite struct{}

var _ = check.Suite(&extKeypairMgrImplSuite{})

var fakeExtKeypairMgrConfig = extKeypairMgrConfig{signingWith: "fake", keyStore: "fake"}

type fakeExtKeypairMgrBackendBase struct {
	signingMethod   extKeypairMgrSigning
	loadByName      map[string]*extKeypairMgrLoadedKey
	visitKeys       []*extKeypairMgrLoadedKey
	loadCalls       []string
	visitCalls      int
	visitConsidered [][]string
	rsaSignHandles  []string
	pgpSignHandles  []string
	pgpSignResult   map[string][]byte
	privByHandle    map[string]*rsa.PrivateKey
}

func (s *fakeExtKeypairMgrBackendBase) CheckFeatures() (extKeypairMgrSigning, error) {
	return s.signingMethod, nil
}

func (s *fakeExtKeypairMgrBackendBase) Visit(consider func(loaded *extKeypairMgrLoadedKey) error) error {
	s.visitCalls++
	considered := make([]string, 0, len(s.visitKeys))
	for _, loaded := range s.visitKeys {
		considered = append(considered, loaded.name)
		if err := consider(loaded); err != nil {
			s.visitConsidered = append(s.visitConsidered, considered)
			return err
		}
	}
	s.visitConsidered = append(s.visitConsidered, considered)
	return nil
}

func (s *fakeExtKeypairMgrBackendBase) RSAPKCSSign(keyHandle string, prepared []byte) ([]byte, error) {
	s.rsaSignHandles = append(s.rsaSignHandles, keyHandle)
	return rsa.SignPKCS1v15(rand.Reader, s.privByHandle[keyHandle], 0, prepared)
}

func (s *fakeExtKeypairMgrBackendBase) Sign(keyHandle string, content []byte) ([]byte, error) {
	s.pgpSignHandles = append(s.pgpSignHandles, keyHandle)
	if sig := s.pgpSignResult[keyHandle]; sig != nil {
		return sig, nil
	}
	packetSig, err := openpgpPrivateKey{privk: packet.NewRSAPrivateKey(v1FixedTimestamp, s.privByHandle[keyHandle])}.sign(content)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(nil)
	err = packetSig.Serialize(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type fakeExtKeypairMgrBackend struct {
	fakeExtKeypairMgrBackendBase
}

func (s *fakeExtKeypairMgrBackend) LoadByName(name string) (*extKeypairMgrLoadedKey, error) {
	s.loadCalls = append(s.loadCalls, name)
	loaded := s.loadByName[name]
	if loaded == nil {
		return nil, &keyNotFoundError{msg: "missing key"}
	}
	return loaded, nil
}

type fakeExtKeypairMgrBackendWithoutByNameLookup struct {
	fakeExtKeypairMgrBackendBase
}

func (s *extKeypairMgrImplSuite) newLoadedKey(c *check.C, name string, keyHandle string) (*rsa.PrivateKey, *extKeypairMgrLoadedKey) {
	privKey, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, check.IsNil)
	return privKey, &extKeypairMgrLoadedKey{
		name:      name,
		keyHandle: keyHandle,
		pubKey:    RSAPublicKey(&privKey.PublicKey),
	}
}

func (s *extKeypairMgrImplSuite) TestLoadByNameCachesExportAndPrivateKey(c *check.C) {
	privKey, loaded := s.newLoadedKey(c, "default", "handle-default")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName: map[string]*extKeypairMgrLoadedKey{
				"default": loaded,
			},
			privByHandle: map[string]*rsa.PrivateKey{
				"handle-default": privKey,
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	key1, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	key2, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	exported, err := impl.Export("default")
	c.Assert(err, check.IsNil)
	expectedExport, err := EncodePublicKey(loaded.pubKey)
	c.Assert(err, check.IsNil)

	c.Check(key1, check.Equals, key2)
	c.Check(backend.loadCalls, check.DeepEquals, []string{"default"})
	c.Check(backend.visitCalls, check.Equals, 0)
	c.Check(exported, check.DeepEquals, expectedExport)
}

func (s *extKeypairMgrImplSuite) TestGetStopsAfterFirstMatchingVisitedKey(c *check.C) {
	privKey1, loaded1 := s.newLoadedKey(c, "default", "handle-default")
	privKey2, loaded2 := s.newLoadedKey(c, "models", "handle-models")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName:    map[string]*extKeypairMgrLoadedKey{},
			visitKeys:     []*extKeypairMgrLoadedKey{loaded1, loaded2},
			privByHandle:  map[string]*rsa.PrivateKey{"handle-default": privKey1, "handle-models": privKey2},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	key1, err := impl.Get(loaded1.pubKey.ID())
	c.Assert(err, check.IsNil)

	c.Check(key1.PublicKey().ID(), check.Equals, loaded1.pubKey.ID())
	c.Check(backend.visitCalls, check.Equals, 1)
	c.Check(backend.visitConsidered, check.DeepEquals, [][]string{{"default"}})
	_, found := impl.cache[loaded2.pubKey.ID()]
	c.Check(found, check.Equals, false)
}

func (s *extKeypairMgrImplSuite) TestGetStopsAtMatchingVisitedKeyAndCachesVisitedPrefix(c *check.C) {
	privKey1, loaded1 := s.newLoadedKey(c, "default", "handle-default")
	privKey2, loaded2 := s.newLoadedKey(c, "models", "handle-models")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName:    map[string]*extKeypairMgrLoadedKey{},
			visitKeys:     []*extKeypairMgrLoadedKey{loaded1, loaded2},
			privByHandle:  map[string]*rsa.PrivateKey{"handle-default": privKey1, "handle-models": privKey2},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	key2, err := impl.Get(loaded2.pubKey.ID())
	c.Assert(err, check.IsNil)
	key1, err := impl.Get(loaded1.pubKey.ID())
	c.Assert(err, check.IsNil)

	c.Check(key2.PublicKey().ID(), check.Equals, loaded2.pubKey.ID())
	c.Check(key1.PublicKey().ID(), check.Equals, loaded1.pubKey.ID())
	c.Check(backend.visitCalls, check.Equals, 1)
	c.Check(backend.visitConsidered, check.DeepEquals, [][]string{{"default", "models"}})
	c.Check(impl.cache[loaded1.pubKey.ID()], check.NotNil)
	c.Check(impl.cache[loaded2.pubKey.ID()], check.NotNil)

	list, err := impl.List()
	c.Assert(err, check.IsNil)
	c.Check(backend.visitCalls, check.Equals, 2)
	c.Check(backend.visitConsidered, check.DeepEquals, [][]string{{"default", "models"}, {"default", "models"}})
	c.Check(list, check.DeepEquals, []ExternalKeyInfo{{Name: "default", ID: loaded1.pubKey.ID()}, {Name: "models", ID: loaded2.pubKey.ID()}})
}

func (s *extKeypairMgrImplSuite) TestGetRevisitsWhenRequestedKeyWasNotInCachedPrefix(c *check.C) {
	privKey1, loaded1 := s.newLoadedKey(c, "default", "handle-default")
	privKey2, loaded2 := s.newLoadedKey(c, "models", "handle-models")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName:    map[string]*extKeypairMgrLoadedKey{},
			visitKeys:     []*extKeypairMgrLoadedKey{loaded1, loaded2},
			privByHandle:  map[string]*rsa.PrivateKey{"handle-default": privKey1, "handle-models": privKey2},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.Get(loaded1.pubKey.ID())
	c.Assert(err, check.IsNil)
	_, err = impl.Get(loaded2.pubKey.ID())
	c.Assert(err, check.IsNil)

	c.Check(backend.visitCalls, check.Equals, 2)
	c.Check(backend.visitConsidered, check.DeepEquals, [][]string{{"default"}, {"default", "models"}})
}

func (s *extKeypairMgrImplSuite) TestGetByNameUsesByNameLookupFastPath(c *check.C) {
	privKey, loaded := s.newLoadedKey(c, "default", "handle-default")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName: map[string]*extKeypairMgrLoadedKey{
				"default": loaded,
			},
			visitKeys: []*extKeypairMgrLoadedKey{loaded},
			privByHandle: map[string]*rsa.PrivateKey{
				"handle-default": privKey,
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.GetByName("default")
	c.Assert(err, check.IsNil)

	c.Check(backend.loadCalls, check.DeepEquals, []string{"default"})
	c.Check(backend.visitCalls, check.Equals, 0)
}

func (s *extKeypairMgrImplSuite) TestGetByNamePropagatesNotFoundWithByNameLookup(c *check.C) {
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName:    map[string]*extKeypairMgrLoadedKey{},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.GetByName("missing")
	c.Assert(err, check.ErrorMatches, `missing key`)
	c.Check(IsKeyNotFound(err), check.Equals, true)
	c.Check(backend.loadCalls, check.DeepEquals, []string{"missing"})
	c.Check(backend.visitCalls, check.Equals, 0)
}

func (s *extKeypairMgrImplSuite) TestGetByNameFallsBackToVisitWithoutByNameLookup(c *check.C) {
	privKey, loaded := s.newLoadedKey(c, "default", "handle-default")
	backend := &fakeExtKeypairMgrBackendWithoutByNameLookup{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			visitKeys:     []*extKeypairMgrLoadedKey{loaded},
			privByHandle: map[string]*rsa.PrivateKey{
				"handle-default": privKey,
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	priv, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	c.Check(priv.PublicKey().ID(), check.Equals, loaded.pubKey.ID())
	c.Check(backend.visitCalls, check.Equals, 1)
}

func (s *extKeypairMgrImplSuite) TestGetByNameFallbackCachesVisitedEntry(c *check.C) {
	privKey, loaded := s.newLoadedKey(c, "default", "handle-default")
	backend := &fakeExtKeypairMgrBackendWithoutByNameLookup{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			visitKeys:     []*extKeypairMgrLoadedKey{loaded},
			privByHandle: map[string]*rsa.PrivateKey{
				"handle-default": privKey,
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	key1, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	exported, err := impl.Export("default")
	c.Assert(err, check.IsNil)
	key2, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	expectedExport, err := EncodePublicKey(loaded.pubKey)
	c.Assert(err, check.IsNil)

	c.Check(key1, check.Equals, key2)
	c.Check(exported, check.DeepEquals, expectedExport)
	c.Check(backend.visitCalls, check.Equals, 1)
}

func (s *extKeypairMgrImplSuite) TestGetByNameFallbackUsesKeyStoreError(c *check.C) {
	backend := &fakeExtKeypairMgrBackendWithoutByNameLookup{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.GetByName("missing")
	c.Assert(err, check.ErrorMatches, `cannot find key pair in fake`)
	c.Check(IsKeyNotFound(err), check.Equals, true)
	c.Check(backend.visitCalls, check.Equals, 1)
}

func (s *extKeypairMgrImplSuite) TestExportPropagatesNotFoundWithByNameLookup(c *check.C) {
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName:    map[string]*extKeypairMgrLoadedKey{},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.Export("missing")
	c.Assert(err, check.ErrorMatches, `missing key`)
	c.Check(IsKeyNotFound(err), check.Equals, true)
	c.Check(backend.loadCalls, check.DeepEquals, []string{"missing"})
	c.Check(backend.visitCalls, check.Equals, 0)
}

func (s *extKeypairMgrImplSuite) TestExportFallbackUsesKeyStoreError(c *check.C) {
	backend := &fakeExtKeypairMgrBackendWithoutByNameLookup{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.Export("missing")
	c.Assert(err, check.ErrorMatches, `cannot find key pair in fake`)
	c.Check(IsKeyNotFound(err), check.Equals, true)
	c.Check(backend.visitCalls, check.Equals, 1)
}

type fakeNonRSAPublicKey struct {
	id string
}

func (pk *fakeNonRSAPublicKey) ID() string                                         { return pk.id }
func (pk *fakeNonRSAPublicKey) verify(content []byte, sig *packet.Signature) error { return nil }
func (pk *fakeNonRSAPublicKey) cryptoPublicKey() crypto.PublicKey                  { return ed25519.PublicKey{} }
func (pk *fakeNonRSAPublicKey) keyEncode(w io.Writer) error                        { return nil }

func (s *extKeypairMgrImplSuite) TestCacheLoadedKeyInvalidPublicKeyErrorIsNotRepetitive(c *check.C) {
	impl, err := newExtKeypairMgrImpl(&fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
		},
	}, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.cacheLoadedKey(&extKeypairMgrLoadedKey{
		name:      "default",
		keyHandle: "handle-default",
		pubKey:    &fakeNonRSAPublicKey{id: "ZmFrZQ"},
	})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Matches, `loaded key "default" has invalid public key: internal error: expected RSA public key, got instead: .*`)
	c.Check(err.Error(), check.Not(check.Matches), `internal error: loaded key .*: internal error: .*`)
}

func (s *extKeypairMgrImplSuite) TestGetMissingUsesKeyStoreError(c *check.C) {
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName:    map[string]*extKeypairMgrLoadedKey{},
			privByHandle:  map[string]*rsa.PrivateKey{},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	_, err = impl.Get("missing-id")
	c.Assert(err, check.ErrorMatches, `cannot find key pair in fake`)
	c.Check(IsKeyNotFound(err), check.Equals, true)
	c.Check(backend.visitCalls, check.Equals, 1)
}

func (s *extKeypairMgrImplSuite) TestRSAPKCSSigningUsesKeyHandle(c *check.C) {
	privKey, loaded := s.newLoadedKey(c, "default", "rsa-handle")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningRSAPKCS,
			loadByName: map[string]*extKeypairMgrLoadedKey{
				"default": loaded,
			},
			privByHandle: map[string]*rsa.PrivateKey{
				"rsa-handle": privKey,
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	priv, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	sig, err := RawSignWithKey([]byte("hello"), priv)
	c.Assert(err, check.IsNil)
	err = RawVerifyWithKey([]byte("hello"), sig, priv.PublicKey())
	c.Assert(err, check.IsNil)
	c.Check(backend.rsaSignHandles, check.DeepEquals, []string{"rsa-handle"})
}

func (s *extKeypairMgrImplSuite) TestOpenPGPSigningUsesKeyHandle(c *check.C) {
	privKey, loaded := s.newLoadedKey(c, "default", "pgp-handle")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningOpenPGP,
			loadByName: map[string]*extKeypairMgrLoadedKey{
				"default": loaded,
			},
			privByHandle: map[string]*rsa.PrivateKey{
				"pgp-handle": privKey,
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	priv, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	sig, err := RawSignWithKey([]byte("hello"), priv)
	c.Assert(err, check.IsNil)
	err = RawVerifyWithKey([]byte("hello"), sig, priv.PublicKey())
	c.Assert(err, check.IsNil)
	c.Check(backend.pgpSignHandles, check.DeepEquals, []string{"pgp-handle"})
}

func (s *extKeypairMgrImplSuite) TestOpenPGPSigningInvalidPacketUsesSigningWithInError(c *check.C) {
	_, loaded := s.newLoadedKey(c, "default", "pgp-handle")
	backend := &fakeExtKeypairMgrBackend{
		fakeExtKeypairMgrBackendBase: fakeExtKeypairMgrBackendBase{
			signingMethod: extKeypairMgrSigningOpenPGP,
			loadByName: map[string]*extKeypairMgrLoadedKey{
				"default": loaded,
			},
			pgpSignResult: map[string][]byte{
				"pgp-handle": []byte("broken"),
			},
		},
	}

	impl, err := newExtKeypairMgrImpl(backend, fakeExtKeypairMgrConfig)
	c.Assert(err, check.IsNil)

	priv, err := impl.GetByName("default")
	c.Assert(err, check.IsNil)
	_, err = RawSignWithKey([]byte("hello"), priv)
	c.Assert(err, check.ErrorMatches, `bad fake produced signature: .*`)
	c.Check(backend.pgpSignHandles, check.DeepEquals, []string{"pgp-handle"})
}
