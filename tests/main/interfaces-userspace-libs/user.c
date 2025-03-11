#include <stdlib.h>
#include <stdio.h>

int mysquare(int x);
int mymultiply(int x, int y);

int main(int argc, char** argv) {
    int val;
    if (argc != 2) {
        return -1;
    }
    val = (int) strtol(argv[1], NULL, 10);
    printf("%d\n", mysquare(val) + mymultiply(val, val));
}
