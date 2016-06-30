/*
 * Copyright (C) 2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
#ifdef _UNIT_TESTING
#include "unit-tests.h"
#else
#include "sc-main.h"
#endif

int main(int argc, char **argv)
{
#ifdef _UNIT_TESTING
	return sc_run_unit_tests(&argc, &argv);
#else				// _UNIT_TESTING
	return sc_main(argc, argv);
#endif				// _UNIT_TESTING
}
