#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/types.h>
#include <unistd.h>

#define MAP_SIZE 4096UL
#define MAP_MASK (MAP_SIZE - 1)

int main(int argc, char** argv)
{
    int fd;
    void *map_base, *virt_addr;
    uint32_t read_result, write_result;

    uint32_t writeval = 't';
    off_t address = 0x00000001;
    int retval = -1;

    /* Open character device */
    if ((fd = open("/dev/gpiomem", O_RDWR)) == -1) {
        perror("cannot open /dev/gpiomem");
        goto close;
    }

    /* Map one page */
    map_base = mmap(NULL, MAP_SIZE, PROT_READ | PROT_WRITE, MAP_SHARED, fd, address & ~MAP_MASK);
    if (map_base == MAP_FAILED) {
        perror("cannot map gpio memory");
        goto unmap;
    }
    printf("Memory mapped at address %p.\n", map_base);

    /* Read memory map */
    virt_addr = (char*)map_base + (address & MAP_MASK);
    read_result = *((uint32_t*)virt_addr);
    printf("Read value: %#010x\n", read_result);

    /* Write memory map */
    *((uint32_t*)virt_addr) = writeval;
    write_result = *((uint32_t*)virt_addr);
    printf("Written %#010x; readback %#010x\n", writeval, write_result);

    retval = 0;

unmap:
    /* Unmap the page */
    if (munmap(map_base, MAP_SIZE) == -1) {
        perror("cannot unmap gpio memory");
        retval = -1;
    }

close:
    close(fd);
    return retval;
}
