
CFLAGS= -O2 -Wall -Werror
LIBS = -lapparmor -lseccomp

BIN = ubuntu-core-launcher
SRCS = $(wildcard *.c)
OBJS = $(SRCS:.c=.o)

ubuntu-core-launcher: $(OBJS)
	$(CC) $(OBJS) -o $@ $(LIBS)

%.o: %.c
	$(CC) -c $(CFLAGS) $< -o $@

# To remove generated files
clean:
	rm -f *.o ${BIN}
