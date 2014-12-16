
CFLAGS= -O2 -Wall -Werror
LIBS = -lapparmor

BIN = ubuntu-core-launcher
SRCS = $(wildcard *.c)
OBJS = $(SRCS:.cpp=.o)

ubuntu-core-launcher: $(OBJS)
	$(CC) $(OBJS) -o $@ $(LIBS)

%.o: %.c
	$(CC) -c $(CFLAGS) $< -o $@

# To remove generated files
clean:
	rm -f *.o
