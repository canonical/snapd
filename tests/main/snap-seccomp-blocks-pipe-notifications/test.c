#define _GNU_SOURCE
#include <stdio.h>
#include <unistd.h>
#include <sys/fcntl.h>

int main(void) {
  int fd[2];
  // O_EXCL is O_NOTIFICATION_PIPE. Even if the kernel does not support it,
  // the seccomp filter is expected to reject it.
  if (pipe2(fd, O_EXCL|O_CLOEXEC) < 0) {
    perror("pipe2");
    return 1;
  }
  close(fd[0]);
  close(fd[1]);
  return 0;
}
