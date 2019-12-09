CFLAGS += -g -O0 -Wall -Wstrict-prototypes

PAREC_SIMPLE = parec-simple

all: $(PAREC_SIMPLE)

parec-simple.o: parec-simple.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS} $(shell pkg-config --cflags --libs libpulse-simple)

$(PAREC_SIMPLE): parec-simple.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS} $(shell pkg-config --cflags --libs libpulse-simple)

install: $(PAREC_SIMPLE)
	install -D -m755 $(PAREC_SIMPLE) $(DESTDIR)/bin/$(PAREC_SIMPLE)

clean:
	rm -f ./*.o ./$(PAREC_SIMPLE)
