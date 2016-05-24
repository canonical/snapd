CFLAGS= -D_GNU_SOURCE -O2 -Wall -Werror $(shell dpkg-buildflags --get CFLAGS)
LD_FLAGS = $(shell dpkg-buildflags --get LDFLAGS)
LIBS = -lapparmor -lseccomp -ludev
TMPDIR = ./tmp
FMT = indent -linux

BIN = snap-run
HDRS = $(wildcard *.h)
SRCS = $(wildcard *.c)
OBJS = $(SRCS:.c=.o)

snap-run: $(OBJS)
	$(CC) $(OBJS) -o $@ $(LD_FLAGS) $(LIBS)

%.o: %.c
	$(CC) -c $(CFLAGS) $< -o $@

distclean: clean
clean:
	rm -f *.o $(BIN) *~
	rm -rf $(TMPDIR)

fmt:
	for f in $(HDRS) $(SRCS); do \
		echo "$(FMT) $$f ... "; \
		$(FMT) "$$f"; \
	done; \

syntax-check:
	$(shell mkdir $(TMPDIR) 2>/dev/null)
	for f in $(HDRS) $(SRCS); do \
		out=$(TMPDIR)/$$f.out; \
		echo "Checking '$(FMT) $$f' ... "; \
		$(FMT) "$$f" -o "$$out"; \
		diff -Naur "$$f" "$$out" || exit 1; \
	done; \

install:
	# create dirs
	install -d -m755  ${DESTDIR}/usr/bin
	install -d -m755  ${DESTDIR}/lib/udev/rules.d
	# install the launcher, must be suid root
	install -m4755 $(BIN) ${DESTDIR}/usr/bin
	# install the udev helper
	install -m755 snappy-app-dev ${DESTDIR}/lib/udev/
	# install the udev rule
	install -m644 80-snappy-assign.rules  ${DESTDIR}/lib/udev/rules.d/

.PHONY: test
