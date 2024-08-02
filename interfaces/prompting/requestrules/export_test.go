// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package requestrules

var JoinInternalErrors = joinInternalErrors

func (rdb *RuleDB) InjectRule(rule *Rule) {
	rdb.rules = append(rdb.rules, rule)
	rdb.ids[rule.ID] = len(rdb.rules) - 1
}

func (rdb *RuleDB) PerUser() map[uint32]*userDB {
	return rdb.perUser
}
