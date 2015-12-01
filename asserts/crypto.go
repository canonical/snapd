// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"crypto/rand"
	"crypto/rsa"
	_ "crypto/sha256" // be explicit about needing SHA256
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

// TODO: eventually this should be the only non-test file using/importing directly from golang.org/x/crypto

func generatePrivateKey() (*packet.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return packet.NewRSAPrivateKey(time.Now(), priv), nil
}

var openpgpConfig = &packet.Config{
	DefaultHash: crypto.SHA256,
}

func signContent(content []byte, privKey *packet.PrivateKey) ([]byte, error) {
	sig := new(packet.Signature)
	sig.PubKeyAlgo = privKey.PubKeyAlgo
	sig.Hash = openpgpConfig.Hash()
	sig.CreationTime = time.Now()
	sig.IssuerKeyId = &privKey.KeyId

	h := openpgpConfig.Hash().New()
	h.Write(content)

	err := sig.Sign(h, privKey, openpgpConfig)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBufferString("openpgp ")
	// TODO: split the base64 blob over many lines for readability of the resulting file
	enc := base64.NewEncoder(base64.StdEncoding, buf)
	err = sig.Serialize(enc)
	if err != nil {
		return nil, err
	}
	enc.Close()

	return buf.Bytes(), nil
}

func splitFormatAndBase64Decode(formatAndBase64 []byte) (string, []byte, error) {
	parts := bytes.SplitN(formatAndBase64, []byte(" "), 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("expected format and base64 data separated by space")
	}
	buf := make([]byte, base64.StdEncoding.DecodedLen(len(parts[1])))
	n, err := base64.StdEncoding.Decode(buf, parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("could not decode base64 data: %v", err)
	}
	return string(parts[0]), buf[:n], nil
}

func parseOpenpgp(formatAndBase64 []byte, kind string) (packet.Packet, error) {
	if len(formatAndBase64) == 0 {
		return nil, fmt.Errorf("empty %s", kind)
	}
	format, data, err := splitFormatAndBase64Decode(formatAndBase64)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", kind, err)
	}
	if format != "openpgp" {
		return nil, fmt.Errorf("unsupported %s format: %q", kind, format)
	}
	pkt, err := packet.Read(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not parse %s data: %v", kind, err)
	}
	return pkt, nil
}

// Signature is a cryptographic signature.
type Signature interface {
	// KeyID() returns a suffix of the signing key fingerprint
	KeyID() string
}

type openpgpSignature struct {
	sig *packet.Signature
}

func (opgSig openpgpSignature) KeyID() string {
	return fmt.Sprintf("%016x", *opgSig.sig.IssuerKeyId)
}

func verifyContentSignature(content []byte, sig Signature, pubKey *packet.PublicKey) error {
	opgSig, ok := sig.(openpgpSignature)
	if !ok {
		panic(fmt.Errorf("not an internally supported Signature: %T", sig))
	}

	h := openpgpConfig.Hash().New()
	h.Write(content)
	return pubKey.VerifySignature(h, opgSig.sig)
}

func parseSignature(signature []byte) (Signature, error) {
	pkt, err := parseOpenpgp(signature, "signature")
	if err != nil {
		return nil, err
	}
	sig, ok := pkt.(*packet.Signature)
	if !ok {
		return nil, fmt.Errorf("expected signature, got instead: %T", pkt)
	}
	if sig.IssuerKeyId == nil {
		return nil, fmt.Errorf("expected issuer key id in signature")
	}
	return openpgpSignature{sig}, nil
}

type openpgpPubKey struct {
	pubKey *packet.PublicKey
	fp     string
}

func (opgPubKey *openpgpPubKey) IsValidAt(time time.Time) bool {
	return true
}

func (opgPubKey *openpgpPubKey) Fingerprint() string {
	return opgPubKey.fp
}

func (opgPubKey *openpgpPubKey) Verify(content []byte, sig Signature) error {
	return verifyContentSignature(content, sig, opgPubKey.pubKey)
}

// WrapPublicKey returns a database useable public key out of a opengpg packet.PulicKey.
func WrapPublicKey(pubKey *packet.PublicKey) PublicKey {
	return &openpgpPubKey{pubKey: pubKey, fp: hex.EncodeToString(pubKey.Fingerprint[:])}
}

func parsePublicKey(pubKey []byte) (PublicKey, error) {
	pkt, err := parseOpenpgp(pubKey, "public key")
	if err != nil {
		return nil, err
	}
	pubk, ok := pkt.(*packet.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected public key, got instead: %T", pkt)
	}
	return WrapPublicKey(pubk), nil
}
