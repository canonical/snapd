// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2024 Canonical Ltd
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

package certstate

import (
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/tomb.v2"
)

type Certificate = certificate
type Certificates = certificates

const (
	Asn1TagVisibleString      = asn1TagVisibleString
	Asn1TagUniversalString    = asn1TagUniversalString
	PreviousGenerationTaskKey = previousGenerationTaskKey
	UndoFromGenerationTaskKey = undoFromGenerationTaskKey
)

var (
	IsBlocked                            = isBlocked
	ParseCertificates                    = parseCertificates
	ReadDigests                          = readDigests
	GenerateCACertificates               = generateCACertificates
	GarbageCollectCertificateGenerations = garbageCollectCertificateGenerations

	Asn1IsCanonicalizedStringType = asn1IsCanonicalizedStringType
	Asn1IsASCII                   = asn1IsASCII
	Asn1IsASCIISpace              = asn1IsASCIISpace
	AppendASN1Length              = appendASN1Length
	MarshalASN1Value              = marshalASN1Value
	Asn1StringToUTF8Bytes         = asn1StringToUTF8Bytes
	CanonicalizeASN1String        = canonicalizeASN1String
	CanonicalizeNameAttribute     = canonicalizeNameAttribute
	CanonicalSubjectNameDER       = canonicalSubjectNameDER
)

func MockRefreshCertificateDatabase(f func() error) func() {
	restore := testutil.Backup(&RefreshCertificateDatabase)
	RefreshCertificateDatabase = f
	return restore
}

func MockOsutilBootID(f func() (string, error)) func() {
	restore := testutil.Backup(&osutilBootID)
	osutilBootID = f
	return restore
}

func (m *CertManager) DoUpdateCertificateDatabase(t *state.Task, tb *tomb.Tomb) error {
	return m.doUpdateCertificateDatabase(t, tb)
}

func (m *CertManager) UndoUpdateCertificateDatabase(t *state.Task, tb *tomb.Tomb) error {
	return m.undoUpdateCertificateDatabase(t, tb)
}
