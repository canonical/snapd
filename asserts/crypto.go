// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	_ "crypto/sha256" // be explicit about supporting SHA256
	_ "crypto/sha512" // be explicit about needing SHA512
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

const (
	maxEncodeLineLength = 76
)

func encodeFormatAndData(format string, data []byte) []byte {
	buf := new(bytes.Buffer)
	buf.Grow(len(format) + 1 + base64.StdEncoding.EncodedLen(len(data)))
	buf.WriteString(format)
	buf.WriteByte(' ')
	enc := base64.NewEncoder(base64.StdEncoding, buf)
	enc.Write(data)
	enc.Close()
	flat := buf.Bytes()
	flatSize := len(flat)

	buf = new(bytes.Buffer)
	buf.Grow(flatSize + flatSize/maxEncodeLineLength + 1)
	off := 0
	for {
		endOff := off + maxEncodeLineLength
		if endOff > flatSize {
			endOff = flatSize
		}
		buf.Write(flat[off:endOff])
		off = endOff
		if off >= flatSize {
			break
		}
		buf.WriteByte('\n')
	}

	return buf.Bytes()
}

type keyEncoder interface {
	keyFormat() string
	keyEncode(w io.Writer) error
}

func encodeKey(key keyEncoder, kind string) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := key.keyEncode(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to encode %s: %v", kind, err)
	}
	return encodeFormatAndData(key.keyFormat(), buf.Bytes()), nil
}

type openpgpSigner interface {
	sign(content []byte) (*packet.Signature, error)
}

func signContent(content []byte, privateKey PrivateKey) ([]byte, error) {
	signer, ok := privateKey.(openpgpSigner)
	if !ok {
		panic(fmt.Errorf("not an internally supported PrivateKey: %T", privateKey))
	}

	sig, err := signer.sign(content)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	err = sig.Serialize(buf)
	if err != nil {
		return nil, err
	}

	return encodeFormatAndData("openpgp", buf.Bytes()), nil
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

func decodeOpenpgp(formatAndBase64 []byte, kind string) (packet.Packet, error) {
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
		return nil, fmt.Errorf("could not decode %s data: %v", kind, err)
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

	h := opgSig.sig.Hash.New()
	h.Write(content)
	return pubKey.VerifySignature(h, opgSig.sig)
}

func decodeSignature(signature []byte) (Signature, error) {
	pkt, err := decodeOpenpgp(signature, "signature")
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

// PublicKey is the public part of a cryptographic private/public key pair.
type PublicKey interface {
	// Fingerprint returns the key fingerprint.
	Fingerprint() string
	// ID returns the id of the key as used to match signatures to their signing key.
	ID() string

	// verify verifies signature is valid for content using the key.
	verify(content []byte, sig Signature) error

	keyEncoder
}

type openpgpPubKey struct {
	pubKey *packet.PublicKey
	fp     string
}

func (opgPubKey *openpgpPubKey) Fingerprint() string {
	return opgPubKey.fp
}

func (opgPubKey *openpgpPubKey) ID() string {
	// the key id is defined as the 64 bits suffix of the 160 bits fingerprint
	return opgPubKey.fp[24:40]
}

func (opgPubKey *openpgpPubKey) verify(content []byte, sig Signature) error {
	return verifyContentSignature(content, sig, opgPubKey.pubKey)
}

func (opgPubKey *openpgpPubKey) keyFormat() string {
	return "openpgp"
}

func (opgPubKey openpgpPubKey) keyEncode(w io.Writer) error {
	return opgPubKey.pubKey.Serialize(w)
}

// OpenPGPPublicKey returns a database useable public key out of a opengpg packet.PulicKey.
func OpenPGPPublicKey(pubKey *packet.PublicKey) PublicKey {
	return &openpgpPubKey{pubKey: pubKey, fp: hex.EncodeToString(pubKey.Fingerprint[:])}
}

func decodePublicKey(pubKey []byte) (PublicKey, error) {
	pkt, err := decodeOpenpgp(pubKey, "public key")
	if err != nil {
		return nil, err
	}
	pubk, ok := pkt.(*packet.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected public key, got instead: %T", pkt)
	}
	return OpenPGPPublicKey(pubk), nil
}

// EncodePublicKey serializes a public key, typically for embedding in an assertion.
func EncodePublicKey(pubKey PublicKey) ([]byte, error) {
	return encodeKey(pubKey, "public key")
}

// PrivateKey is a cryptographic private/public key pair.
type PrivateKey interface {
	// PublicKey returns the public part of the pair.
	PublicKey() PublicKey

	keyEncoder
}

type openpgpPrivateKey struct {
	privk *packet.PrivateKey
}

func (opgPrivK openpgpPrivateKey) PublicKey() PublicKey {
	return OpenPGPPublicKey(&opgPrivK.privk.PublicKey)
}

func (opgPrivK openpgpPrivateKey) keyFormat() string {
	return "openpgp"
}

func (opgPrivK openpgpPrivateKey) keyEncode(w io.Writer) error {
	return opgPrivK.privk.Serialize(w)
}

var openpgpConfig = &packet.Config{
	DefaultHash: crypto.SHA512,
}

func (opgPrivK openpgpPrivateKey) sign(content []byte) (*packet.Signature, error) {
	privk := opgPrivK.privk
	sig := new(packet.Signature)
	sig.PubKeyAlgo = privk.PubKeyAlgo
	sig.Hash = openpgpConfig.Hash()
	sig.CreationTime = time.Now()
	sig.IssuerKeyId = &privk.KeyId

	h := openpgpConfig.Hash().New()
	h.Write(content)

	err := sig.Sign(h, privk, openpgpConfig)
	if err != nil {
		return nil, err
	}

	return sig, nil
}

func decodePrivateKey(privKey []byte) (PrivateKey, error) {
	pkt, err := decodeOpenpgp(privKey, "private key")
	if err != nil {
		return nil, err
	}
	privk, ok := pkt.(*packet.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected private key, got instead: %T", pkt)
	}
	return openpgpPrivateKey{privk}, nil
}

// OpenPGPPrivateKey returns a PrivateKey for database use out of a opengpg packet.PrivateKey.
func OpenPGPPrivateKey(privk *packet.PrivateKey) PrivateKey {
	return openpgpPrivateKey{privk}
}

// GenerateKey generates a private/public key pair.
func GenerateKey() (PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}
	return OpenPGPPrivateKey(packet.NewRSAPrivateKey(time.Now(), priv)), nil
}

func encodePrivateKey(privKey PrivateKey) ([]byte, error) {
	return encodeKey(privKey, "private key")
}

// externally held key pairs

type extPGPPrivateKey struct {
	pubKey PublicKey
	from   string
	doSign func(fingerprint string, content []byte) ([]byte, error)
}

func newExtPGPPrivateKey(exportedPubKeyStream io.Reader, from string, sign func(fingerprint string, content []byte) ([]byte, error)) (PrivateKey, error) {
	var pubKey *packet.PublicKey

	rd := packet.NewReader(exportedPubKeyStream)
	for {
		pkt, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read exported public key: %v", err)
		}
		cand, ok := pkt.(*packet.PublicKey)
		if ok {
			if cand.IsSubkey {
				continue
			}
			if pubKey != nil {
				return nil, fmt.Errorf("cannot select exported public key, found many")
			}
			pubKey = cand
		}
	}

	if pubKey == nil {
		return nil, fmt.Errorf("cannot read exported public key, found none (broken export)")

	}

	rsaPubKey, ok := pubKey.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not a RSA key")
	}

	bitLen := rsaPubKey.N.BitLen()
	if bitLen < 4096 {
		return nil, fmt.Errorf("need at least 4096 bits key, got %d", bitLen)
	}

	return &extPGPPrivateKey{
		pubKey: OpenPGPPublicKey(pubKey),
		from:   from,
		doSign: sign,
	}, nil
}

func (expk *extPGPPrivateKey) PublicKey() PublicKey {
	return expk.pubKey
}

func (expk *extPGPPrivateKey) keyEncode(w io.Writer) error {
	return fmt.Errorf("cannot access external private key to encode it")
}

func (expk *extPGPPrivateKey) keyFormat() string {
	return ""
}

func (expk *extPGPPrivateKey) sign(content []byte) (*packet.Signature, error) {
	out, err := expk.doSign(expk.pubKey.Fingerprint(), content)
	if err != nil {
		return nil, err
	}

	badSig := fmt.Sprintf("bad %s produced signature: ", expk.from)

	sigpkt, err := packet.Read(bytes.NewBuffer(out))
	if err != nil {
		return nil, fmt.Errorf(badSig+"%v", err)
	}

	sig, ok := sigpkt.(*packet.Signature)
	if !ok {
		return nil, fmt.Errorf(badSig+"got %T", sigpkt)
	}

	opgSig := openpgpSignature{sig}

	if sig.IssuerKeyId == nil {
		return nil, fmt.Errorf(badSig + "no key id in the signature")
	}

	sigKeyID := opgSig.KeyID()
	wantedID := expk.pubKey.ID()
	if sigKeyID != wantedID {
		return nil, fmt.Errorf(badSig+"wrong key id (expected %q): %s", wantedID, sigKeyID)
	}

	if sig.Hash != crypto.SHA512 {
		return nil, fmt.Errorf(badSig + "expected SHA512 digest")
	}

	err = expk.pubKey.verify(content, opgSig)
	if err != nil {
		return nil, fmt.Errorf(badSig+"it does not verify: %v", err)
	}

	return sig, nil
}
