SNAP_NAME = test-snapd-hello-classic
SNAP_VERSION = 0.1

# Ask gcc about the architecture name
arch := $(shell $(CC) -dumpmachine)

ifeq ($(arch),x86_64-linux-gnu)
snap_arch = amd64
dynamic_linker=ld-linux-x86-64.so.2
else ifeq ($(arch),i686-linux-gnu)
# NOTE: arch needs to be i386-linux-gnu, not i686-linux-gnu
arch := i386-linux-gnu
snap_arch = i386
dynamic_linker=ld-linux.so.2
else ifeq ($(arch),aarch64-linux-gnu)
snap_arch = arm64
dynamic_linker=ld-linux-aarch64.so.1
else ifeq ($(arch),arm-linux-gnueabihf)
snap_arch = armhf
dynamic_linker=ld-linux-armhf.so.3
else ifeq ($(arch),powerpc64le-linux-gnu)
# NOTE: The architecture name is ppc64el (E-L) but GCC uses powerpc64le (L-E)
snap_arch = ppc64el
dynamic_linker=ld64.so.2
else
$(error cannot translate architecture $(arch) to snap equivalent)
endif

# Name of the snap we're building
snap_file = $(SNAP_NAME)_$(SNAP_VERSION)_$(snap_arch).snap

define snap_yaml
name: $(SNAP_NAME)
version: $(SNAP_VERSION)
summary: A hello-world with classic confinement
architectures: [$(snap_arch)]
apps:
    $(SNAP_NAME):
        command: test-snapd-hello-classic.$(snap_arch).bin
confinement: classic
endef

.PHONY: all
all: $(snap_file)

# Name of the core snap to use
snap_core=core

# Don't search in default locations
LDFLAGS += -Wl,-z,nodefaultlib
LDFLAGS += -Wl,--enable-new-dtags
# Search in the core snap
LDFLAGS += -Wl,-rpath,/snap/$(snap_core)/current/lib/$(arch):/snap/$(snap_core)/current/usr/lib/$(arch)
# Use the dynamic linker from the core snap
LDFLAGS += -Wl,--dynamic-linker=/snap/$(snap_core)/current/lib/$(arch)/$(dynamic_linker)

test-snapd-hello-classic.$(snap_arch).bin: test-snapd-hello-classic.c
	$(CC) $(CFLAGS) $(LDFLAGS) -o $@ $<

$(snap_file): test-snapd-hello-classic.$(snap_arch).bin meta/snap.yaml
	mksquashfs . $@ -e $@ -noappend -no-xattrs -comp xz

meta: Makefile
	mkdir -p $@

export snap_yaml
meta/snap.yaml: Makefile | meta
	echo "$$snap_yaml" > $@

.ONESHELL:

.PHONY: clean
clean:
	rm -f test-snapd-hello-classic.*.bin
	rm -f meta/snap.yaml
	rm -f *.snap
	rm -f -d meta
