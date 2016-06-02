bin_PROGRAMS = snap-confine
snap_confine_SOURCES = main.c utils.c
snap_confine_CFLAGS = -Wall -Werror
snap_confine_LDADD =

if STRICT_CONFINEMENT
snap_confine_SOURCES += seccomp_utils.c
snap_confine_CFLAGS += $(APPARMOR_CFLAGS) $(SECCOMP_CFLAGS) $(UDEV_CFLAGS)
snap_confine_LDADD += $(APPARMOR_LIBS) $(SECCOMP_LIBS) $(UDEV_LIBS)
endif

# Force particular coding style on all source and header files.
.PHONY: check-syntax
check-syntax:
	@d=`mktemp -d`; \
	trap 'rm -rf $d' EXIT; \
	for f in $(wildcard *.c) $(wildcard *.h); do \
	       out="$$d/$$f.out"; \
	       echo "Checking $$f ... "; \
	       indent -linux "$$f" -o "$$out"; \
	       diff -Naur "$$f" "$$out" || exit 1; \
	done;

# Run check-syntax when checking
# TODO: conver those to autotools-style tests later
check: check-syntax

.PHONY: fmt
fmt:
	for f in $(wildcard *.c) $(wildcard *.h); do \
	       echo "Formatting $$f ... "; \
	       indent -linux "$$f"; \
	done;

if STRICT_CONFINEMENT
# Install udev rules
install-data-local:
	install -d -m 755 $(DESTDIR)/lib/udev/rules.d
	install -m 644 80-snappy-assign.rules $(DESTDIR)/lib/udev/rules.d

# Install support script for udev rules
install-exec-local:
	install -d -m 755 $(DESTDIR)/lib/udev
	install -m 755 snappy-app-dev $(DESTDIR)/lib/udev/
endif

# Ensure that snap-confine is +s (setuid)
install-exec-hook:
	chmod 4755 $(DESTDIR)$(bindir)/snap-confine
