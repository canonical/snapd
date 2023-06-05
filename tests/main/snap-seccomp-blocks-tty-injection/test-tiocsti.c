#define _GNU_SOURCE
#include <termios.h>
#include <sys/ioctl.h>
#include <unistd.h>
#include <stdio.h>
#include <sys/syscall.h>
#include <errno.h>

static int ioctl64(int fd, unsigned long nr, void *arg) {
  errno = 0;
  return syscall(__NR_ioctl, fd, nr, arg);
}

int main(void) {
  int res;
  char pushmeback = '#';

  unsigned long syscallnr = TIOCSTI;
  res = ioctl64(0, syscallnr, &pushmeback);
  printf("normal TIOCSTI: %d (%m)\n", res);

#ifdef __LP64__
  // this high bit check only works on 64bit systems, on 32bit it will fail:
  // "error: left shift count >= width of type [-Werror=shift-count-overflow]"
  syscallnr = TIOCSTI | (1UL<<32);
#endif
  res = ioctl64(0, syscallnr, &pushmeback);
  printf("high-bit-set TIOCSTI: %d (%m)\n", res);
  return res;
}
