libexec_PROGRAMS = snap-discard-ns
dist_man_MANS = snap-discard-ns.5

CLEANFILES = snap-discard-ns.5
EXTRA_DIST = snap-discard-ns.rst

snap_discard_ns_SOURCES = \
	../snap-confine/ns-support.c \
	../snap-confine/ns-support.h \
	../snap-confine/apparmor-support.c \
	../snap-confine/apparmor-support.h \
	../snap-confine/cleanup-funcs.c \
	../snap-confine/cleanup-funcs.h \
	../snap-confine/mountinfo.c \
	../snap-confine/mountinfo.h \
	snap-discard-ns.c
snap_discard_ns_CFLAGS = -Wall -Werror $(AM_CFLAGS)
snap_discard_ns_LDFLAGS = $(AM_LDFLAGS)
snap_discard_ns_LDADD = ../snap-confine/libsnap-confine-private.a
snap_discard_ns_CFLAGS += $(SECCOMP_CFLAGS)
snap_discard_ns_LDADD += $(SECCOMP_LIBS)

if APPARMOR
snap_discard_ns_CFLAGS += $(APPARMOR_CFLAGS)
snap_discard_ns_LDADD += $(APPARMOR_LIBS)
endif

# Force particular coding style on all source and header files.
.PHONY: check-syntax
check-syntax:
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(wildcard $(srcdir)/*.c) $(wildcard $(srcdir)/*.h); do \
	       out="$$d/`basename $$f.out`"; \
	       echo "Checking $$f ... "; \
	       indent -linux "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-syntax

.PHONY: fmt
fmt:
	for f in $(wildcard $(srcdir)/*.c) $(wildcard $(srcdir)/*.h); do \
	       echo "Formatting $$f ... "; \
	       indent -linux "$$f"; \
	done;

%.5: %.rst
	rst2man $^ > $@
