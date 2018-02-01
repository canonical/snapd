#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/types.h>
#include <termios.h>
#include <unistd.h>

#define MAP_SIZE 4096UL
#define MAP_MASK (MAP_SIZE - 1)

int main(int argc, char** argv)
{
    int fd;
    void *map_base, *virt_addr;
    uint32_t read_result, writeval;
    off_t target;

    if (argc < 2) {
        printf("Usage: gpiomem ADDRESS [DATA]\n"
               "\tADDRESS : memory address to act upon\n"
               "\tDATA    : data to be written\n\n");
        exit(1);
    }
    target = strtoul(argv[1], 0, 0);

    if ((fd = open("/dev/gpiomem", O_RDWR | O_SYNC)) == -1) {
        fprintf(stderr, "Error: File /dev/gpiomem could not be opened\n");
        exit(1);
    }

    /* Map one page */
    map_base = mmap(NULL, MAP_SIZE, PROT_READ | PROT_WRITE, MAP_SHARED, fd, target & ~MAP_MASK);
    if (map_base == MAP_FAILED) {
        fprintf(stderr, "Error: Page could not be mapped\n");
        exit(1);
    }

    printf("Memory mapped at address %p.\n", map_base);

    virt_addr = map_base + (target & MAP_MASK);
    read_result = *((uint32_t*)virt_addr);

    printf("Read value: 0x%ui\n", read_result);

    if (argc > 2) {
        writeval = strtoul(argv[2], 0, 0);
        *((uint32_t*)virt_addr) = writeval;
        read_result = *((uint32_t*)virt_addr);

        printf("Written 0x%ui; readback 0x%ui\n", writeval, read_result);
    }

    if (munmap(map_base, MAP_SIZE) == -1) {
        fprintf(stderr, "Error: Data could not be written\n");
        exit(1);
    }

    close(fd);
    return 0;
}
