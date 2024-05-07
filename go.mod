module github.com/snapcore/snapd

go 1.18

// maze.io/x/crypto/afis imported by github.com/snapcore/secboot/tpm2
replace maze.io/x/crypto => github.com/snapcore/maze.io-x-crypto v0.0.0-20190131090603-9b94c9afe066

require (
	github.com/bmatcuk/doublestar/v4 v4.6.1
	github.com/canonical/go-efilib v0.9.5
	github.com/canonical/go-sp800.90a-drbg v0.0.0-20210314144037-6eeb1040d6c3 // indirect
	github.com/canonical/go-tpm2 v1.3.0
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/godbus/dbus v0.0.0-20190726142602-4481cbc300e2
	github.com/gorilla/mux v1.8.0
	github.com/gvalkov/golang-evdev v0.0.0-20191114124502-287e62b94bcb
	github.com/jessevdk/go-flags v1.5.1-0.20210607101731-3927b71304df
	github.com/juju/ratelimit v1.0.1
	github.com/mvo5/goconfigparser v0.0.0-20231016112547-05bd887f05e1
	// if below two libseccomp-golang lines are updated, one must also update packaging/ubuntu-14.04/rules
	github.com/mvo5/libseccomp-golang v0.9.1-0.20180308152521-f4de83b52afb // old trusty builds only
	github.com/seccomp/libseccomp-golang v0.9.2-0.20220502024300-f57e1d55ea18
	github.com/snapcore/go-gettext v0.0.0-20191107141714-82bbea49e785
	github.com/snapcore/secboot v0.0.0-20240315160953-f8dedd0209e6
	golang.org/x/crypto v0.13.0
	golang.org/x/net v0.15.0 // indirect
	golang.org/x/sys v0.12.0
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c
	gopkg.in/macaroon.v1 v1.0.0-20150121114231-ab3940c6c165
	gopkg.in/retry.v1 v1.0.3
	gopkg.in/tomb.v2 v2.0.0-20161208151619-d5d1b5820637
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	go.etcd.io/bbolt v1.3.9
	golang.org/x/text v0.13.0
)

require (
	github.com/canonical/go-sp800.108-kdf v0.0.0-20210315104021-ead800bbf9a0 // indirect
	github.com/canonical/tcglog-parser v0.0.0-20230929123437-16b3d8d08691 // indirect
	github.com/kr/pretty v0.2.2-0.20200810074440-814ac30b4b18 // indirect
	github.com/kr/text v0.1.0 // indirect
	golang.org/x/term v0.12.0 // indirect
	maze.io/x/crypto v0.0.0-20190131090603-9b94c9afe066 // indirect
)

replace github.com/snapcore/secboot => github.com/chrisccoulson/secboot v0.0.0-20240404180734-cf489d604eb0
