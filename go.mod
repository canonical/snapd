module github.com/snapcore/snapd

go 1.13

require (
	github.com/canonical/go-efilib v0.0.0-20210909101908-41435fa545d4 // indirect
	github.com/canonical/go-sp800.90a-drbg v0.0.0-20210314144037-6eeb1040d6c3 // indirect
	github.com/canonical/go-tpm2 v0.0.0-20210827151749-f80ff5afff61
	github.com/canonical/tcglog-parser v0.0.0-20210824131805-69fa1e9f0ad2 // indirect
	github.com/canonical/ubuntu-image v0.0.0-20220722194516-317b74d3a56f // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/godbus/dbus v0.0.0-20190726142602-4481cbc300e2
	github.com/gorilla/mux v1.8.0
	github.com/gvalkov/golang-evdev v0.0.0-20191114124502-287e62b94bcb
	github.com/jessevdk/go-flags v1.5.0
	github.com/juju/ratelimit v1.0.1
	github.com/kr/pretty v0.2.2-0.20200810074440-814ac30b4b18 // indirect
	github.com/mvo5/goconfigparser v0.0.0-20201015074339-50f22f44deb5
	// if below two libseccomp-golang lines are updated, one must also update packaging/ubuntu-14.04/rules
	github.com/mvo5/libseccomp-golang v0.9.1-0.20180308152521-f4de83b52afb // old trusty builds only
	github.com/seccomp/libseccomp-golang v0.9.2-0.20220502024300-f57e1d55ea18
	github.com/snapcore/bolt v1.3.2-0.20210908134111-63c8bfcf7af8
	github.com/snapcore/go-gettext v0.0.0-20201130093759-38740d1bd3d2
	github.com/snapcore/secboot v0.0.0-20211018143212-802bb19ca263
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97
	golang.org/x/sys v0.0.0-20210908233432-aa78b53d3365
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c
	gopkg.in/macaroon.v1 v1.0.0
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
	gopkg.in/retry.v1 v1.0.3
	gopkg.in/tomb.v2 v2.0.0-20161208151619-d5d1b5820637
	gopkg.in/tylerb/graceful.v1 v1.2.15
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	maze.io/x/crypto v0.0.0-20190131090603-9b94c9afe066 // indirect
)
