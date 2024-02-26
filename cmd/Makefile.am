
EXTRA_DIST = VERSION snap-confine/PORTING
CLEANFILES =
TESTS =
libexec_PROGRAMS =
dist_man_MANS =
noinst_PROGRAMS =
noinst_LIBRARIES =

AM_CFLAGS = $(CHECK_CFLAGS)

if USE_INTERNAL_BPF_HEADERS
VENDOR_BPF_HEADERS_CFLAGS = -I$(srcdir)/libsnap-confine-private/bpf/vendor
endif

subdirs = \
		  libsnap-confine-private \
		  snap-confine \
		  snap-device-helper \
		  snap-discard-ns \
		  snap-gdb-shim \
		  snap-update-ns \
		  snapd-env-generator \
		  snapd-generator \
		  system-shutdown

# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-unit-tests

# Force particular coding style on all source and header files.
.PHONY: check-syntax-c
check-syntax-c:
	echo "WARNING: check-syntax-c produces different results for different version of indent"
	echo "Your version of indent: `indent --version`"
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch])) ; do \
	       out="$$d/`basename $$f.out`"; \
	       echo "Checking $$f ... "; \
	       HOME=$(srcdir) indent "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

.PHONY: check-unit-tests
if WITH_UNIT_TESTS
check-unit-tests: snap-confine/unit-tests system-shutdown/unit-tests libsnap-confine-private/unit-tests snap-device-helper/unit-tests
	$(HAVE_VALGRIND) ./libsnap-confine-private/unit-tests
	$(HAVE_VALGRIND) ./snap-confine/unit-tests
	$(HAVE_VALGRIND) ./system-shutdown/unit-tests
	$(HAVE_VALGRIND) ./snap-device-helper/unit-tests
else
check-unit-tests:
	echo "unit tests are disabled (rebuild with --enable-unit-tests)"
endif

# NOTE: clang-format is using project-wide .clang-format file.
.PHONY: fmt
fmt:: $(foreach dir,$(subdirs),$(wildcard $(srcdir)/$(dir)/*.[ch]))
	clang-format -i $^

# The hack target helps developers work on snap-confine on their live system by
# installing a fresh copy of snap confine and the appropriate apparmor profile.
.PHONY: hack
hack: snap-confine/snap-confine-debug snap-confine/snap-confine.apparmor snap-update-ns/snap-update-ns snap-seccomp/snap-seccomp snap-discard-ns/snap-discard-ns snap-device-helper/snap-device-helper snapd-apparmor/snapd-apparmor
	sudo install -D -m 4755 snap-confine/snap-confine-debug $(DESTDIR)$(libexecdir)/snap-confine
	if [ -d /etc/apparmor.d ]; then sudo install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine.real; fi
	sudo install -d -m 755 $(DESTDIR)/var/lib/snapd/apparmor/snap-confine/
	if [ "$$(command -v apparmor_parser)" != "" ]; then sudo apparmor_parser -r snap-confine/snap-confine.apparmor; fi
	sudo install -m 755 snap-update-ns/snap-update-ns $(DESTDIR)$(libexecdir)/snap-update-ns
	sudo install -m 755 snap-discard-ns/snap-discard-ns $(DESTDIR)$(libexecdir)/snap-discard-ns
	sudo install -m 755 snap-seccomp/snap-seccomp $(DESTDIR)$(libexecdir)/snap-seccomp
	sudo install -m 755 snap-device-helper/snap-device-helper $(DESTDIR)$(libexecdir)/snap-device-helper
	sudo install -m 755 snapd-apparmor/snapd-apparmor $(DESTDIR)$(libexecdir)/snapd-apparmor
	if [ "$$(command -v restorecon)" != "" ]; then sudo restorecon -R -v $(DESTDIR)$(libexecdir)/; fi

# for the hack target also:
snap-update-ns/snap-update-ns: snap-update-ns/*.go snap-update-ns/*.[ch]
	cd snap-update-ns && GOPATH=$(or $(GOPATH),$(realpath $(srcdir)/../../../../..)) go build \
		-ldflags='-extldflags=-static -linkmode=external' -v
snap-seccomp/snap-seccomp: snap-seccomp/*.go
	cd snap-seccomp && GOPATH=$(or $(GOPATH),$(realpath $(srcdir)/../../../../..)) go build -v
snapd-apparmor/snapd-apparmor: snapd-apparmor/*.go
	cd snapd-apparmor && GOPATH=$(or $(GOPATH),$(realpath $(srcdir)/../../../../..)) go build -v

##
## libsnap-confine-private.a
##

noinst_LIBRARIES += libsnap-confine-private.a

libsnap_confine_private_a_SOURCES = \
	libsnap-confine-private/apparmor-support.c \
	libsnap-confine-private/apparmor-support.h \
	libsnap-confine-private/cgroup-freezer-support.c \
	libsnap-confine-private/cgroup-freezer-support.h \
	libsnap-confine-private/cgroup-support.c \
	libsnap-confine-private/cgroup-support.h \
	libsnap-confine-private/device-cgroup-support.c \
	libsnap-confine-private/device-cgroup-support.h \
	libsnap-confine-private/classic.c \
	libsnap-confine-private/classic.h \
	libsnap-confine-private/cleanup-funcs.c \
	libsnap-confine-private/cleanup-funcs.h \
	libsnap-confine-private/error.c \
	libsnap-confine-private/error.h \
	libsnap-confine-private/fault-injection.c \
	libsnap-confine-private/fault-injection.h \
	libsnap-confine-private/feature.c \
	libsnap-confine-private/feature.h \
	libsnap-confine-private/infofile.c \
	libsnap-confine-private/locking.c \
	libsnap-confine-private/locking.h \
	libsnap-confine-private/mount-opt.c \
	libsnap-confine-private/mount-opt.h \
	libsnap-confine-private/mountinfo.c \
	libsnap-confine-private/mountinfo.h \
	libsnap-confine-private/panic.c \
	libsnap-confine-private/panic.h \
	libsnap-confine-private/privs.c \
	libsnap-confine-private/privs.h \
	libsnap-confine-private/secure-getenv.c \
	libsnap-confine-private/secure-getenv.h \
	libsnap-confine-private/snap.c \
	libsnap-confine-private/snap.h \
	libsnap-confine-private/string-utils.c \
	libsnap-confine-private/string-utils.h \
	libsnap-confine-private/tool.c \
	libsnap-confine-private/tool.h \
	libsnap-confine-private/utils.c \
	libsnap-confine-private/utils.h
if ENABLE_BPF
libsnap_confine_private_a_SOURCES += \
	libsnap-confine-private/bpf-support.c \
	libsnap-confine-private/bpf-support.h
endif
libsnap_confine_private_a_CFLAGS = $(AM_CFLAGS) $(VENDOR_BPF_HEADERS_CFLAGS)

noinst_LIBRARIES += libsnap-confine-private-debug.a
libsnap_confine_private_debug_a_SOURCES = $(libsnap_confine_private_a_SOURCES)
libsnap_confine_private_debug_a_CFLAGS = $(AM_CFLAGS) $(VENDOR_BPF_HEADERS_CFLAGS) -DSNAP_CONFINE_DEBUG_BUILD=1

if WITH_UNIT_TESTS
noinst_PROGRAMS += libsnap-confine-private/unit-tests
libsnap_confine_private_unit_tests_SOURCES = \
	libsnap-confine-private/cgroup-support-test.c \
	libsnap-confine-private/classic-test.c \
	libsnap-confine-private/cleanup-funcs-test.c \
	libsnap-confine-private/error-test.c \
	libsnap-confine-private/fault-injection-test.c \
	libsnap-confine-private/feature-test.c \
	libsnap-confine-private/infofile-test.c \
	libsnap-confine-private/locking-test.c \
	libsnap-confine-private/mount-opt-test.c \
	libsnap-confine-private/mountinfo-test.c \
	libsnap-confine-private/panic-test.c \
	libsnap-confine-private/privs-test.c \
	libsnap-confine-private/secure-getenv-test.c \
	libsnap-confine-private/snap-test.c \
	libsnap-confine-private/string-utils-test.c \
	libsnap-confine-private/test-utils-test.c \
	libsnap-confine-private/test-utils.c \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	libsnap-confine-private/utils-test.c

libsnap_confine_private_unit_tests_CFLAGS = $(AM_CFLAGS) $(VENDOR_BPF_HEADERS_CFLAGS) $(GLIB_CFLAGS)
libsnap_confine_private_unit_tests_LDADD = $(GLIB_LIBS)
libsnap_confine_private_unit_tests_CFLAGS += -D_ENABLE_FAULT_INJECTION
libsnap_confine_private_unit_tests_STATIC =

if STATIC_LIBCAP
libsnap_confine_private_unit_tests_STATIC += -lcap
else
libsnap_confine_private_unit_tests_LDADD += -lcap
endif  # STATIC_LIBCAP

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
libsnap-confine-private/unit-tests$(EXEEXT): $(libsnap_confine_private_unit_tests_OBJECTS) $(libsnap_confine_private_unit_tests_DEPENDENCIES) $(EXTRA_libsnap_confine_private_unit_tests_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f libsnap-confine-private/unit-tests$(EXEEXT)
	$(AM_V_CCLD)$(libsnap_confine_private_unit_tests_LINK) $(libsnap_confine_private_unit_tests_OBJECTS) $(libsnap_confine_private_unit_tests_LDADD) $(LIBS)

libsnap-confine-private/unit-tests$(EXEEXT): LIBS += -Wl,-Bstatic $(libsnap_confine_private_unit_tests_STATIC) -Wl,-Bdynamic
endif  # WITH_UNIT_TESTS

##
## decode-mount-opts
##

noinst_PROGRAMS += decode-mount-opts/decode-mount-opts

decode_mount_opts_decode_mount_opts_SOURCES = \
	decode-mount-opts/decode-mount-opts.c
decode_mount_opts_decode_mount_opts_LDADD = libsnap-confine-private.a
decode_mount_opts_decode_mount_opts_STATIC =

if STATIC_LIBCAP
decode_mount_opts_decode_mount_opts_STATIC += -lcap
else
decode_mount_opts_decode_mount_opts_LDADD += -lcap
endif  # STATIC_LIBCAP

# XXX: this makes automake generate decode_mount_opts_decode_mount_opts_LINK
decode_mount_opts_decode_mount_opts_CFLAGS = -D_fake

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
decode-mount-opts/decode-mount-opts$(EXEEXT): $(decode_mount_opts_decode_mount_opts_OBJECTS) $(decode_mount_opts_decode_mount_opts_DEPENDENCIES) $(EXTRA_decode_mount_opts_decode_mount_opts_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f decode-mount-opts/decode-mount-opts$(EXEEXT)
	$(AM_V_CCLD)$(decode_mount_opts_decode_mount_opts_LINK) $(decode_mount_opts_decode_mount_opts_OBJECTS) $(decode_mount_opts_decode_mount_opts_LDADD) $(LIBS)

decode-mount-opts/decode-mount-opts$(EXEEXT): LIBS += -Wl,-Bstatic $(decode_mount_opts_decode_mount_opts_STATIC) -Wl,-Bdynamic

##
## snap-confine
##

libexec_PROGRAMS += snap-confine/snap-confine
if HAVE_RST2MAN
dist_man_MANS += snap-confine/snap-confine.8
CLEANFILES += snap-confine/snap-confine.8
endif
EXTRA_DIST += snap-confine/snap-confine.rst
EXTRA_DIST += snap-confine/snap-confine.apparmor.in

snap_confine_snap_confine_SOURCES = \
	snap-confine/cookie-support.c \
	snap-confine/cookie-support.h \
	snap-confine/mount-support-nvidia.c \
	snap-confine/mount-support-nvidia.h \
	snap-confine/mount-support.c \
	snap-confine/mount-support.h \
	snap-confine/ns-support.c \
	snap-confine/ns-support.h \
	snap-confine/seccomp-support-ext.c \
	snap-confine/seccomp-support-ext.h \
	snap-confine/seccomp-support.c \
	snap-confine/seccomp-support.h \
	snap-confine/snap-confine-args.c \
	snap-confine/snap-confine-args.h \
	snap-confine/snap-confine-invocation.c \
	snap-confine/snap-confine-invocation.h \
	snap-confine/snap-confine.c \
	snap-confine/udev-support.c \
	snap-confine/udev-support.h \
	snap-confine/user-support.c \
	snap-confine/user-support.h

snap_confine_snap_confine_CFLAGS = $(AM_CFLAGS) -DLIBEXECDIR=\"$(libexecdir)\" -DNATIVE_LIBDIR=\"$(libdir)\"
snap_confine_snap_confine_LDFLAGS = $(AM_LDFLAGS)
snap_confine_snap_confine_LDADD = libsnap-confine-private.a
snap_confine_snap_confine_CFLAGS += $(LIBUDEV_CFLAGS)
snap_confine_snap_confine_LDADD += $(snap_confine_snap_confine_extra_libs)
# _STATIC is where we collect statically linked in libraries
snap_confine_snap_confine_STATIC =
# use a separate variable instead of snap_confine_snap_confine_LDADD to collect
# all external libraries, this way it can be reused in
# snap_confine_snap_confine_debug_LDADD withouth applying any text
# transformations
snap_confine_snap_confine_extra_libs = $(LIBUDEV_LIBS) -ldl

if STATIC_LIBCAP
snap_confine_snap_confine_STATIC += -lcap
else
snap_confine_snap_confine_extra_libs += -lcap
endif  # STATIC_LIBCAP

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/snap-confine$(EXEEXT): $(snap_confine_snap_confine_OBJECTS) $(snap_confine_snap_confine_DEPENDENCIES) $(EXTRA_snap_confine_snap_confine_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f snap-confine/snap-confine$(EXEEXT)
	$(AM_V_CCLD)$(snap_confine_snap_confine_LINK) $(snap_confine_snap_confine_OBJECTS) $(snap_confine_snap_confine_LDADD) $(LIBS)

snap-confine/snap-confine$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_confine_snap_confine_STATIC) -Wl,-Bdynamic -pthread

# This is here to help fix rpmlint hardening issue.
# https://en.opensuse.org/openSUSE:Packaging_checks#non-position-independent-executable
snap_confine_snap_confine_CFLAGS += $(SUID_CFLAGS)
snap_confine_snap_confine_LDFLAGS += $(SUID_LDFLAGS)

if APPARMOR
snap_confine_snap_confine_CFLAGS += $(APPARMOR_CFLAGS)
if STATIC_LIBAPPARMOR
snap_confine_snap_confine_STATIC += $(shell $(PKG_CONFIG) --static --libs libapparmor)
else
snap_confine_snap_confine_extra_libs += $(APPARMOR_LIBS)
endif  # STATIC_LIBAPPARMOR
endif  # APPARMOR

if SELINUX
snap_confine_snap_confine_SOURCES += \
	snap-confine/selinux-support.c \
	snap-confine/selinux-support.h
snap_confine_snap_confine_CFLAGS += $(SELINUX_CFLAGS)
if STATIC_LIBSELINUX
snap_confine_snap_confine_STATIC += $(shell $(PKG_CONFIG) --static --libs libselinux)
else
snap_confine_snap_confine_extra_libs += $(SELINUX_LIBS)
endif  # STATIC_LIBSELINUX
endif  # SELINUX

# an extra build that has additional debugging enabled at compile time

noinst_PROGRAMS += snap-confine/snap-confine-debug
snap_confine_snap_confine_debug_SOURCES = $(snap_confine_snap_confine_SOURCES)
snap_confine_snap_confine_debug_CFLAGS = $(snap_confine_snap_confine_CFLAGS)
snap_confine_snap_confine_debug_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
snap_confine_snap_confine_debug_LDADD = libsnap-confine-private-debug.a $(snap_confine_snap_confine_extra_libs)
snap_confine_snap_confine_debug_CFLAGS += -DSNAP_CONFINE_DEBUG_BUILD=1
snap_confine_snap_confine_debug_STATIC = $(snap_confine_snap_confine_STATIC)

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/snap-confine-debug$(EXEEXT): $(snap_confine_snap_confine_debug_OBJECTS) $(snap_confine_snap_confine_debug_DEPENDENCIES) $(EXTRA_snap_confine_snap_confine_debug_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f snap-confine/snap-confine-debug$(EXEEXT)
	$(AM_V_CCLD)$(snap_confine_snap_confine_debug_LINK) $(snap_confine_snap_confine_debug_OBJECTS) $(snap_confine_snap_confine_debug_LDADD) $(LIBS)

snap-confine/snap-confine-debug$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_confine_snap_confine_debug_STATIC) -Wl,-Bdynamic -pthread

if WITH_UNIT_TESTS
noinst_PROGRAMS += snap-confine/unit-tests
snap_confine_unit_tests_SOURCES = \
	libsnap-confine-private/test-utils.c \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	snap-confine/cookie-support-test.c \
	snap-confine/mount-support-test.c \
	snap-confine/ns-support-test.c \
	snap-confine/snap-confine-args-test.c \
	snap-confine/snap-confine-invocation-test.c
snap_confine_unit_tests_CFLAGS = $(snap_confine_snap_confine_CFLAGS) $(GLIB_CFLAGS)
snap_confine_unit_tests_LDADD = $(snap_confine_snap_confine_LDADD) $(GLIB_LIBS)
snap_confine_unit_tests_LDFLAGS = $(snap_confine_snap_confine_LDFLAGS)
snap_confine_unit_tests_STATIC = $(snap_confine_snap_confine_STATIC)

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-confine/unit-tests$(EXEEXT): $(snap_confine_unit_tests_OBJECTS) $(snap_confine_unit_tests_DEPENDENCIES) $(EXTRA_snap_confine_unit_tests_DEPENDENCIES) libsnap-confine-private/$(am__dirstamp)
	@rm -f snap-confine/unit-tests$(EXEEXT)
	$(AM_V_CCLD)$(snap_confine_unit_tests_LINK) $(snap_confine_unit_tests_OBJECTS) $(snap_confine_unit_tests_LDADD) $(LIBS)

snap-confine/unit-tests$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_confine_unit_tests_STATIC) -Wl,-Bdynamic -pthread
endif  # WITH_UNIT_TESTS

if HAVE_RST2MAN
%.8: %.rst
	$(HAVE_RST2MAN) $^ > $@
endif

snap-confine/snap-confine.apparmor: snap-confine/snap-confine.apparmor.in Makefile
	sed -e 's,[@]LIBEXECDIR[@],$(libexecdir),g' -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),g' <$< >$@

# Install the apparmor profile
#
# NOTE: the funky make functions here just convert /foo/bar/froz into
# foo.bar.froz The inner subst replaces slashes with dots and the outer
# patsubst strips the leading dot
install-data-local:: snap-confine/snap-confine.apparmor
if APPARMOR
	install -d -m 755 $(DESTDIR)/etc/apparmor.d/
	install -m 644 snap-confine/snap-confine.apparmor $(DESTDIR)/etc/apparmor.d/$(patsubst .%,%,$(subst /,.,$(libexecdir))).snap-confine
endif
	install -d -m 755 $(DESTDIR)/var/lib/snapd/apparmor/snap-confine/

# NOTE: The 'void' directory *has to* be chmod 111
install-data-local::
	install -d -m 111 $(DESTDIR)/var/lib/snapd/void

install-exec-hook::
# Ensure that snap-confine is u+s (setuid)
	chmod 4755 $(DESTDIR)$(libexecdir)/snap-confine

##
## snap-mgmt
##

libexec_SCRIPTS = snap-mgmt/snap-mgmt
CLEANFILES += snap-mgmt/$(am__dirstamp) snap-mgmt/snap-mgmt

snap-mgmt/$(am__dirstamp):
	mkdir -p $$(dirname $@)
	touch $@

snap-mgmt/snap-mgmt: snap-mgmt/snap-mgmt.sh.in Makefile snap-mgmt/$(am__dirstamp)
	sed -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@

if SELINUX
##
## snap-mgmt-selinux
##

libexec_SCRIPTS += snap-mgmt/snap-mgmt-selinux
CLEANFILES += snap-mgmt/$(am__dirstamp) snap-mgmt/snap-mgmt-selinux

snap-mgmt/snap-mgmt-selinux: snap-mgmt/snap-mgmt-selinux.sh.in Makefile snap-mgmt/$(am__dirstamp)
	sed -e 's,[@]SNAP_MOUNT_DIR[@],$(SNAP_MOUNT_DIR),' <$< >$@
endif

##
## ubuntu-core-launcher
##

install-exec-hook::
	install -d -m 755 $(DESTDIR)$(bindir)
	ln -sf $(libexecdir)/snap-confine $(DESTDIR)$(bindir)/ubuntu-core-launcher

##
## snap-device-helper
##

libexec_PROGRAMS += \
	snap-device-helper/snap-device-helper

snap_device_helper_snap_device_helper_SOURCES = \
	snap-device-helper/main.c \
	snap-device-helper/snap-device-helper.c
snap_device_helper_snap_device_helper_LDFLAGS = $(AM_LDFLAGS)
snap_device_helper_snap_device_helper_LDADD = libsnap-confine-private.a

if WITH_UNIT_TESTS
noinst_PROGRAMS += snap-device-helper/unit-tests
snap_device_helper_unit_tests_SOURCES = \
	libsnap-confine-private/test-utils.c \
	libsnap-confine-private/string-utils.c \
	libsnap-confine-private/utils.c \
	libsnap-confine-private/cleanup-funcs.c \
	libsnap-confine-private/panic.c \
	libsnap-confine-private/snap.c \
	libsnap-confine-private/error.c \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	libsnap-confine-private/unit-tests.h \
	snap-device-helper/snap-device-helper-test.c
snap_device_helper_unit_tests_CFLAGS = $(AM_CFLAGS) $(snap_device_helper_snap_device_helper_CFLAGS) $(GLIB_CFLAGS)
snap_device_helper_unit_tests_LDADD = $(GLIB_LIBS)
snap_device_helper_unit_tests_LDFLAGS =$(snap_device_helper_snap_device_helper_LDFLAGS)

endif  # WITH_UNIT_TESTS

##
## snap-discard-ns
##

libexec_PROGRAMS += snap-discard-ns/snap-discard-ns
if HAVE_RST2MAN
dist_man_MANS += snap-discard-ns/snap-discard-ns.8
CLEANFILES += snap-discard-ns/snap-discard-ns.8
endif
EXTRA_DIST += snap-discard-ns/snap-discard-ns.rst

snap_discard_ns_snap_discard_ns_SOURCES = \
	snap-discard-ns/snap-discard-ns.c
snap_discard_ns_snap_discard_ns_LDFLAGS = $(AM_LDFLAGS)
snap_discard_ns_snap_discard_ns_LDADD = libsnap-confine-private.a
snap_discard_ns_snap_discard_ns_STATIC =

# Use a hacked rule if we're doing static build. This allows us to inject the LIBS += .. rule below.
snap-discard-ns/snap-discard-ns$(EXEEXT): $(snap_discard_ns_snap_discard_ns_OBJECTS) $(snap_discard_ns_snap_discard_ns_DEPENDENCIES) $(EXTRA_snap_discard_ns_snap_discard_ns_DEPENDENCIES) snap-discard-ns/$(am__dirstamp)
	@rm -f snap-discard-ns/snap-discard-ns$(EXEEXT)
	$(AM_V_CCLD)$(snap_discard_ns_snap_discard_ns_LINK) $(snap_discard_ns_snap_discard_ns_OBJECTS) $(snap_discard_ns_snap_discard_ns_LDADD) $(LIBS)

snap-discard-ns/snap-discard-ns$(EXEEXT): LIBS += -Wl,-Bstatic $(snap_discard_ns_snap_discard_ns_STATIC) -Wl,-Bdynamic -pthread

##
## system-shutdown
##

libexec_PROGRAMS += system-shutdown/system-shutdown

system_shutdown_system_shutdown_SOURCES = \
	system-shutdown/system-shutdown-utils.c \
	system-shutdown/system-shutdown-utils.h \
	system-shutdown/system-shutdown.c
system_shutdown_system_shutdown_LDADD = libsnap-confine-private.a

if WITH_UNIT_TESTS
noinst_PROGRAMS += system-shutdown/unit-tests
system_shutdown_unit_tests_SOURCES = \
	libsnap-confine-private/unit-tests-main.c \
	libsnap-confine-private/unit-tests.c \
	system-shutdown/system-shutdown-utils-test.c
system_shutdown_unit_tests_LDADD = libsnap-confine-private.a
system_shutdown_unit_tests_CFLAGS = $(AM_CFLAGS) $(GLIB_CFLAGS)
system_shutdown_unit_tests_LDADD +=  $(GLIB_LIBS)
endif

##
## snap-gdb-shim
##

libexec_PROGRAMS += snap-gdb-shim/snap-gdb-shim

snap_gdb_shim_snap_gdb_shim_SOURCES = \
	snap-gdb-shim/snap-gdb-shim.c

snap_gdb_shim_snap_gdb_shim_LDADD = libsnap-confine-private.a
snap_gdb_shim_snap_gdb_shim_LDFLAGS = -static

##
## snap-gdbserver-shim
##

libexec_PROGRAMS += snap-gdb-shim/snap-gdbserver-shim

snap_gdb_shim_snap_gdbserver_shim_SOURCES = \
	snap-gdb-shim/snap-gdbserver-shim.c

snap_gdb_shim_snap_gdbserver_shim_LDADD = libsnap-confine-private.a
snap_gdb_shim_snap_gdbserver_shim_LDFLAGS = -static

##
## snapd-generator
##

systemdsystemgeneratordir = $(SYSTEMD_SYSTEM_GENERATOR_DIR)
systemdsystemgenerator_PROGRAMS = snapd-generator/snapd-generator

snapd_generator_snapd_generator_SOURCES = snapd-generator/main.c
snapd_generator_snapd_generator_LDADD = libsnap-confine-private.a

##
## snapd-env-generator
##

systemdsystemenvgeneratordir=$(SYSTEMD_SYSTEM_ENV_GENERATOR_DIR)
systemdsystemenvgenerator_PROGRAMS = snapd-env-generator/snapd-env-generator

snapd_env_generator_snapd_env_generator_SOURCES = snapd-env-generator/main.c
snapd_env_generator_snapd_env_generator_LDADD = libsnap-confine-private.a
EXTRA_DIST += snapd-env-generator/snapd-env-generator.rst

if HAVE_RST2MAN
dist_man_MANS += snapd-env-generator/snapd-env-generator.8
CLEANFILES += snapd-env-generator/snapd-env-generator.8
endif

install-exec-local::
	install -d -m 755 $(DESTDIR)$(libexecdir)
