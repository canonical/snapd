
CFLAGS= -O2 -Wall -Werror $(shell dpkg-buildflags --get CFLAGS)
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
	# install the launcher
	install -m755 $(BIN) ${DESTDIR}/usr/bin
	# install the udev helper
	install -m4755 snappy-app-dev ${DESTDIR}/lib/udev/

.PHONY: test
