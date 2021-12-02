// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"io"
	"time"

	"github.com/snapcore/snapd/asserts/internal"
)

// expose test-only things here

var NumAssertionType = len(typeRegistry)

// v1FixedTimestamp exposed for tests
var V1FixedTimestamp = v1FixedTimestamp

// assembleAndSign exposed for tests
var AssembleAndSignInTest = assembleAndSign

// decodePrivateKey exposed for tests
var DecodePrivateKeyInTest = decodePrivateKey

// NewDecoderStressed makes a Decoder with a stressed setup with the given buffer and maximum sizes.
func NewDecoderStressed(r io.Reader, bufSize, maxHeadersSize, maxBodySize, maxSigSize int) *Decoder {
	return (&Decoder{
		rd:                 r,
		initialBufSize:     bufSize,
		maxHeadersSize:     maxHeadersSize,
		maxSigSize:         maxSigSize,
		defaultMaxBodySize: maxBodySize,
	}).initBuffer()
}

func BootstrapAccountForTest(authorityID string) *Account {
	return &Account{
		assertionBase: assertionBase{
			headers: map[string]interface{}{
				"type":         "account",
				"authority-id": authorityID,
				"account-id":   authorityID,
				"validation":   "verified",
			},
		},
		timestamp: time.Now().UTC(),
	}
}

func MakeAccountKeyForTest(authorityID string, openPGPPubKey PublicKey, since time.Time, validYears int) *AccountKey {
	return &AccountKey{
		assertionBase: assertionBase{
			headers: map[string]interface{}{
				"type":                "account-key",
				"authority-id":        authorityID,
				"account-id":          authorityID,
				"public-key-sha3-384": openPGPPubKey.ID(),
			},
		},
		since:  since.UTC(),
		until:  since.UTC().AddDate(validYears, 0, 0),
		pubKey: openPGPPubKey,
	}
}

func BootstrapAccountKeyForTest(authorityID string, pubKey PublicKey) *AccountKey {
	return MakeAccountKeyForTest(authorityID, pubKey, time.Time{}, 9999)
}

func ExpiredAccountKeyForTest(authorityID string, pubKey PublicKey) *AccountKey {
	return MakeAccountKeyForTest(authorityID, pubKey, time.Time{}, 1)
}

func MockTimeNow(t time.Time) (restore func()) {
	oldTimeNow := timeNow
	timeNow = func() time.Time {
		return t
	}
	return func() {
		timeNow = oldTimeNow
	}
}

// define dummy assertion types to use in the tests

type TestOnly struct {
	assertionBase
}

func assembleTestOnly(assert assertionBase) (Assertion, error) {
	// for testing error cases
	if _, err := checkIntWithDefault(assert.headers, "count", 0); err != nil {
		return nil, err
	}
	return &TestOnly{assert}, nil
}

var TestOnlyType = &AssertionType{"test-only", []string{"primary-key"}, assembleTestOnly, 0}

type TestOnly2 struct {
	assertionBase
}

func assembleTestOnly2(assert assertionBase) (Assertion, error) {
	return &TestOnly2{assert}, nil
}

var TestOnly2Type = &AssertionType{"test-only-2", []string{"pk1", "pk2"}, assembleTestOnly2, 0}

// TestOnlyDecl is a test-only assertion that mimics snap-declaration
// relations with other assertions.
type TestOnlyDecl struct {
	assertionBase
}

func (dcl *TestOnlyDecl) ID() string {
	return dcl.HeaderString("id")
}

func (dcl *TestOnlyDecl) DevID() string {
	return dcl.HeaderString("dev-id")
}

func (dcl *TestOnlyDecl) Prerequisites() []*Ref {
	return []*Ref{
		{Type: AccountType, PrimaryKey: []string{dcl.DevID()}},
	}
}

func assembleTestOnlyDecl(assert assertionBase) (Assertion, error) {
	return &TestOnlyDecl{assert}, nil
}

var TestOnlyDeclType = &AssertionType{"test-only-decl", []string{"id"}, assembleTestOnlyDecl, 0}

// TestOnlyRev is a test-only assertion that mimics snap-revision
// relations with other assertions.
type TestOnlyRev struct {
	assertionBase
}

func (rev *TestOnlyRev) H() string {
	return rev.HeaderString("h")
}

func (rev *TestOnlyRev) ID() string {
	return rev.HeaderString("id")
}

func (rev *TestOnlyRev) DevID() string {
	return rev.HeaderString("dev-id")
}

func (rev *TestOnlyRev) Prerequisites() []*Ref {
	return []*Ref{
		{Type: TestOnlyDeclType, PrimaryKey: []string{rev.ID()}},
		{Type: AccountType, PrimaryKey: []string{rev.DevID()}},
	}
}

func assembleTestOnlyRev(assert assertionBase) (Assertion, error) {
	return &TestOnlyRev{assert}, nil
}

var TestOnlyRevType = &AssertionType{"test-only-rev", []string{"h"}, assembleTestOnlyRev, 0}

// TestOnlySeq is a test-only assertion that is sequence-forming.
type TestOnlySeq struct {
	assertionBase
	seq int
}

func (seq *TestOnlySeq) N() string {
	return seq.HeaderString("n")
}

func (seq *TestOnlySeq) Sequence() int {
	return seq.seq
}

func assembleTestOnlySeq(assert assertionBase) (Assertion, error) {
	seq, err := checkSequence(assert.headers, "sequence")
	if err != nil {
		return nil, err
	}
	return &TestOnlySeq{
		assertionBase: assert,
		seq:           seq,
	}, nil
}

var TestOnlySeqType = &AssertionType{"test-only-seq", []string{"n", "sequence"}, assembleTestOnlySeq, sequenceForming}

type TestOnlyNoAuthority struct {
	assertionBase
}

func assembleTestOnlyNoAuthority(assert assertionBase) (Assertion, error) {
	if _, err := checkNotEmptyString(assert.headers, "hdr"); err != nil {
		return nil, err
	}
	return &TestOnlyNoAuthority{assert}, nil
}

var TestOnlyNoAuthorityType = &AssertionType{"test-only-no-authority", nil, assembleTestOnlyNoAuthority, noAuthority}

type TestOnlyNoAuthorityPK struct {
	assertionBase
}

func assembleTestOnlyNoAuthorityPK(assert assertionBase) (Assertion, error) {
	return &TestOnlyNoAuthorityPK{assert}, nil
}

var TestOnlyNoAuthorityPKType = &AssertionType{"test-only-no-authority-pk", []string{"pk"}, assembleTestOnlyNoAuthorityPK, noAuthority}

func init() {
	typeRegistry[TestOnlyType.Name] = TestOnlyType
	maxSupportedFormat[TestOnlyType.Name] = 1
	typeRegistry[TestOnly2Type.Name] = TestOnly2Type
	typeRegistry[TestOnlyNoAuthorityType.Name] = TestOnlyNoAuthorityType
	typeRegistry[TestOnlyNoAuthorityPKType.Name] = TestOnlyNoAuthorityPKType
	formatAnalyzer[TestOnlyType] = func(headers map[string]interface{}, _ []byte) (int, error) {
		if _, ok := headers["format-1-feature"]; ok {
			return 1, nil
		}
		return 0, nil
	}
	typeRegistry[TestOnlyDeclType.Name] = TestOnlyDeclType
	typeRegistry[TestOnlyRevType.Name] = TestOnlyRevType
	typeRegistry[TestOnlySeqType.Name] = TestOnlySeqType
	maxSupportedFormat[TestOnlySeqType.Name] = 2
}

// AccountKeyIsKeyValidAt exposes isKeyValidAt on AccountKey for tests
func AccountKeyIsKeyValidAt(ak *AccountKey, when time.Time) bool {
	return ak.isKeyValidAt(when)
}

// AccountKeyIsKeyValidAssumingCurTimeWithin exposes isKeyValidAssumingCurTimeWithin on AccountKey for tests
func AccountKeyIsKeyValidAssumingCurTimeWithin(ak *AccountKey, earliest, latest time.Time) bool {
	return ak.isKeyValidAssumingCurTimeWithin(earliest, latest)
}

type GPGRunner func(input []byte, args ...string) ([]byte, error)

func MockRunGPG(mock func(prev GPGRunner, input []byte, args ...string) ([]byte, error)) (restore func()) {
	prevRunGPG := runGPG
	runGPG = func(input []byte, args ...string) ([]byte, error) {
		return mock(prevRunGPG, input, args...)
	}
	return func() {
		runGPG = prevRunGPG
	}
}

func GPGBatchYes() (restore func()) {
	gpgBatchYes = true
	return func() {
		gpgBatchYes = false
	}
}

// Headers helpers to test
var (
	ParseHeaders = parseHeaders
	AppendEntry  = appendEntry
)

// ParametersForGenerate exposes parametersForGenerate for tests.
func (gkm *GPGKeypairManager) ParametersForGenerate(passphrase string, name string) string {
	return gkm.parametersForGenerate(passphrase, name)
}

// ifacedecls tests
var (
	CompileAttributeConstraints = compileAttributeConstraints
	CompileNameConstraints      = compileNameConstraints
	CompilePlugRule             = compilePlugRule
	CompileSlotRule             = compileSlotRule
)

type featureExposer interface {
	feature(flabel string) bool
}

func RuleFeature(rule featureExposer, flabel string) bool {
	return rule.feature(flabel)
}

func (b *Batch) DoPrecheck(db *Database) error {
	return b.precheck(db)
}

// pool tests

func MakePoolGrouping(elems ...uint16) Grouping {
	return Grouping(internal.Serialize(elems))
}
