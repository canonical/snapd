# could use 'uname_p := $(shell uname -p)' but this doesn't work right on the
# builders where i386 is built in a 32 bit chroot with amd64 kernel (eg, LP)
# amd64		x86_64-linux-gnu
# arm64		aarch64-linux-gnu
# armhf		arm-linux-gnueabihf
# i386		i386-linux-gnu
# ppc64el	powerpc64le-linux-gnu
# s390x		s390x-linux-gnu
arch_triplet := $(SNAPCRAFT_ARCH_TRIPLET)

CFLAGS += -g -O0 -Wall -Wstrict-prototypes

# snapcraft will copy anything from here
INSTALL_DIR := ../../install/bin

buildNative := drop drop-exec drop-syscall setgid setregid setresgid setuid setreuid setresuid chown lchown fchown fchownat
buildSecond := drop32 drop-exec32 drop-syscall32 setgid32 setregid32 setresgid32 setuid32 setreuid32 setresuid32 chown32 lchown32 fchown32 fchownat32

#ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu aarch64-linux-gnu))
ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu))
all: $(buildNative) $(buildSecond)
else
all: $(buildNative)
endif

display.o: display.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop.o: drop.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop: display.o drop.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

drop-exec.o: drop-exec.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop-exec: display.o drop-exec.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

# Since drop-syscall bypasses glibc, 32 bit archs need to use drop-syscall32.c
# with the *32 syscalls for set*id/setgroups to uids/gids > 2^16. For all the
# other binaries, glibc is used and does this automatically.
ifneq (,$(filter $(arch_triplet), i386-linux-gnu arm-linux-gnueabihf))
drop-syscall.o: drop-syscall32.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop-syscall: display.o drop-syscall32.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}
else
drop-syscall.o: drop-syscall.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop-syscall: display.o drop-syscall.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}
endif

setgid.o: setgid.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setgid: display.o setgid.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setregid.o: setregid.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setregid: display.o setregid.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setresgid.o: setresgid.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setresgid: display.o setresgid.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setuid.o: setuid.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setuid: display.o setuid.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setreuid.o: setreuid.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setreuid: display.o setreuid.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setresuid.o: setresuid.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setresuid: display.o setresuid.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

chown.o: chown.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

chown: display.o chown.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

lchown.o: lchown.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

lchown: display.o lchown.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

fchown.o: fchown.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

fchown: display.o fchown.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

fchownat.o: fchownat.c
	${CC} ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

fchownat: display.o fchownat.o
	${CC} ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

#ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu aarch64-linux-gnu))
ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu))
display32.o: display32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop32.o: drop32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop32: display32.o drop32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

drop-exec32.o: drop-exec32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop-exec32: display32.o drop-exec32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

drop-syscall32.o: drop-syscall32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

drop-syscall32: display32.o drop-syscall32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setgid32.o: setgid32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setgid32: display32.o setgid32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setregid32.o: setregid32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setregid32: display32.o setregid32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setresgid32.o: setresgid32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setresgid32: display32.o setresgid32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setuid32.o: setuid32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setuid32: display32.o setuid32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setreuid32.o: setreuid32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setreuid32: display32.o setreuid32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

setresuid32.o: setresuid32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

setresuid32: display32.o setresuid32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

chown32.o: chown32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

chown32: display32.o chown32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

lchown32.o: lchown32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

lchown32: display32.o lchown32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

fchown32.o: fchown32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

fchown32: display32.o fchown32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}

fchownat32.o: fchownat32.c
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $< -c ${LDLIBS}

fchownat32: display32.o fchownat32.o
	${CC} -m32 ${CFLAGS} ${LDFLAGS} $^ -o $@ ${LDLIBS}
endif


# Only depend on buildSecond on architectures where we are building it
# with -m32
#ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu aarch64-linux-gnu))
ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu))
install: $(buildNative) $(buildSecond)
else
install: $(buildNative)
endif
	mkdir -p ${INSTALL_DIR}
	cp -f drop ${INSTALL_DIR}/drop
	cp -f drop-exec ${INSTALL_DIR}/drop-exec
	cp -f drop-syscall ${INSTALL_DIR}/drop-syscall
	cp -f setgid ${INSTALL_DIR}/setgid
	cp -f setregid ${INSTALL_DIR}/setregid
	cp -f setresgid ${INSTALL_DIR}/setresgid
	cp -f setuid ${INSTALL_DIR}/setuid
	cp -f setreuid ${INSTALL_DIR}/setreuid
	cp -f setresuid ${INSTALL_DIR}/setresuid
	cp -f chown ${INSTALL_DIR}/chown
	cp -f lchown ${INSTALL_DIR}/lchown
	cp -f fchown ${INSTALL_DIR}/fchown
	cp -f fchownat ${INSTALL_DIR}/fchownat
#ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu aarch64-linux-gnu))
ifneq (,$(filter $(arch_triplet), x86_64-linux-gnu))
	cp -f drop32 ${INSTALL_DIR}/drop32
	cp -f drop-exec32 ${INSTALL_DIR}/drop-exec32
	cp -f drop-syscall32 ${INSTALL_DIR}/drop-syscall32
	cp -f setgid32 ${INSTALL_DIR}/setgid32
	cp -f setregid32 ${INSTALL_DIR}/setregid32
	cp -f setresgid32 ${INSTALL_DIR}/setresgid32
	cp -f setuid32 ${INSTALL_DIR}/setuid32
	cp -f setreuid32 ${INSTALL_DIR}/setreuid32
	cp -f setresuid32 ${INSTALL_DIR}/setresuid32
	cp -f chown32 ${INSTALL_DIR}/chown32
	cp -f lchown32 ${INSTALL_DIR}/lchown32
	cp -f fchown32 ${INSTALL_DIR}/fchown32
	cp -f fchownat32 ${INSTALL_DIR}/fchownat32
else
	# *32 binaries are same on 32 bit systems
	cp -f drop ${INSTALL_DIR}/drop32
	cp -f drop-exec ${INSTALL_DIR}/drop-exec32
	cp -f drop-syscall ${INSTALL_DIR}/drop-syscall32
	cp -f setgid ${INSTALL_DIR}/setgid32
	cp -f setregid ${INSTALL_DIR}/setregid32
	cp -f setresgid ${INSTALL_DIR}/setresgid32
	cp -f setuid ${INSTALL_DIR}/setuid32
	cp -f setreuid ${INSTALL_DIR}/setreuid32
	cp -f setresuid ${INSTALL_DIR}/setresuid32
	cp -f chown ${INSTALL_DIR}/chown32
	cp -f lchown ${INSTALL_DIR}/lchown32
	cp -f fchown ${INSTALL_DIR}/fchown32
	cp -f fchownat ${INSTALL_DIR}/fchownat32
endif


clean:
	rm -f ./*.o
	rm -f ./drop ./drop32 ./drop-exec ./drop-exec32 ./drop-syscall ./drop-syscall32
	rm -f ./setgid ./setgid32 ./setregid ./setregid32 ./setresgid ./setresgid32
	rm -f ./setuid ./setuid32 ./setreuid ./setreuid32 ./setresuid ./setresuid32
	rm -f ./chown ./chown32 lchown lchown32 fchown fchown32 fchownat fchownat32
