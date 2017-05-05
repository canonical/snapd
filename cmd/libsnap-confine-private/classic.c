#include "config.h"
#include "classic.h"

#include <unistd.h>

bool is_running_on_classic_distribution()
{
	// NOTE: keep this list sorted please
	return false
	    || access("/var/lib/dpkg/status", F_OK) == 0
	    || access("/var/lib/pacman", F_OK) == 0
	    || access("/var/lib/portage", F_OK) == 0
	    || access("/var/lib/rpm", F_OK) == 0
	    || access("/sbin/procd", F_OK) == 0;
}
