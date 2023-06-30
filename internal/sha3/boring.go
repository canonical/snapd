//go:build boringcrypto

package sha3

import "crypto/sha3"

var New384 = sha3.New384
var Sum384 = sha3.Sum384
