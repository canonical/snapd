CFLAGS += -g -O0 -Wall -Wstrict-prototypes

# snapcraft will copy anything from here
INSTALL_DIR := ../../install/bin

all: parec-simple

parec-simple.o: parec-simple.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS} $(shell pkg-config --cflags --libs libpulse-simple)

parec-simple: parec-simple.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS} $(shell pkg-config --cflags --libs libpulse-simple)

install: parec-simple
	mkdir -p ${INSTALL_DIR}
	cp -f parec-simple ${INSTALL_DIR}

clean:
	rm -f ./*.o
	rm -f parec-simple
