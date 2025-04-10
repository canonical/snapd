package optee

import (
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	fdeTAPresent = fdeTAPresentImpl
	decryptKey   = decryptKeyImpl
	encryptKey   = encryptKeyImpl
	lockTA       = lockTAImpl
)

func FDETAPresent() bool {
	return fdeTAPresent()
}

func DecryptKey(input []byte, handle []byte) ([]byte, error) {
	return decryptKey(input, handle)
}

func EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	return encryptKey(input)
}

func LockTA() error {
	return lockTA()
}

func MockFDETAPresent(f func() bool) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee functions in tests")
	return testutil.Mock(&fdeTAPresent, f)
}

func MockDecryptKey(f func(input []byte, handle []byte) ([]byte, error)) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee functions in tests")
	return testutil.Mock(&decryptKey, f)
}

func MockEncryptKey(f func(input []byte) (handle []byte, sealed []byte, err error)) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee functions in tests")
	return testutil.Mock(&encryptKey, f)
}

func MockLockTA(f func() error) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee functions in tests")
	return testutil.Mock(&lockTA, f)
}
