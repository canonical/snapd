//go:build goexperiment.opensslcrypto

package sha3

import (
	"hash"
	_ "unsafe"
)

//go:linkname New384 crypto/internal/backend.NewSHA3_384
func New384() hash.Hash

//go:linkname Sum384 crypto/internal/backend.SHA3_384
func Sum384([]byte) [48]byte

func init() {
	crypto.RegisterHash(crypto.SHA3_384, New384)
}
