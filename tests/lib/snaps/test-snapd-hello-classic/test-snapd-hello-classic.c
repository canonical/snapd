#include <stdio.h>
#include <stdlib.h>

int main(int argc, char **argv)
{
	if (argc == 1) {
		printf("Hello Classic!\n");
	} else {
		printf("TMPDIR=%s\n", getenv("TMPDIR"));
	}
	return 0;
}
