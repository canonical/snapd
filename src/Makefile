
CFLAGS= -O2 -Wall -Werror
LIBS = -lapparmor -lseccomp

BIN = ubuntu-core-launcher
SRCS = $(wildcard *.c)
OBJS = $(SRCS:.c=.o)

ubuntu-core-launcher: $(OBJS)
	$(CC) $(OBJS) -o $@ $(LIBS)

%.o: %.c
	$(CC) -c $(CFLAGS) $< -o $@

distclean: clean
clean:
	rm -f *.o ${BIN}

install:
	install -m755 $(BIN) ${DESTDIR}/usr/bin

.PHONY: test
