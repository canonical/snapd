# Quick intro to hacking on snap-confine

Hey, welcome to the nice, low-level world of snap-confine

## Building the code locally

To get started from a pristine tree you want to do this:

```
./mkversion.sh
cd cmd/
autoreconf -i -f
./configure --prefix=/usr --libexecdir=/usr/lib/snapd --with-nvidia-ubuntu
```

This will drop makefiles and let you build stuff. You may find the `make hack`
target, available in `cmd/snap-confine` handy, it installs the locally built
version on your system and reloads the apparmor profile.

## Submitting patches

Please run `make fmt` before sending your patches.
