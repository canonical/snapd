
CFLAGS= -D_GNU_SOURCE -O2 -Wall -Werror $(shell dpkg-buildflags --get CFLAGS)
LD_FLAGS = $(shell dpkg-buildflags --get LDFLAGS)
LIBS = -lapparmor -lseccomp -ludev

BIN = ubuntu-core-launcher
SRCS = $(wildcard *.c)
OBJS = $(SRCS:.c=.o)

ubuntu-core-launcher: $(OBJS)
	$(CC) $(OBJS) -o $@ $(LD_FLAGS) $(LIBS)

%.o: %.c
	$(CC) -c $(CFLAGS) $< -o $@

distclean: clean
clean:
	rm -f *.o ${BIN}

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
