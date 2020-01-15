// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package widgets

import (
	"fmt"
	"sort"

	"github.com/snapcore/snapd/render"
)

// H1 returns a widget for a header one.
func H1(text string) render.Widget {
	return &Padding{Top: 2, Bottom: 1, Body: &Pre{Text: fmt.Sprintf("# %s", text)}}
}

// H2 returns a widget for a header two.
func H2(text string) render.Widget {
	return &Padding{Top: 1, Body: &Pre{Text: fmt.Sprintf("## %s", text)}}
}

// T returns a widget for pre-formatted text.
func T(text string) render.Widget {
	return &Pre{Text: text}
}

// Seq returns a widget for displaying widgets on consecutive lines.
func Seq(items ...render.Widget) render.Widget {
	return &VBox{Items: items}
}

// Map returns a widget for displaying a key-value map of other widgets.
func Map(things map[string]render.Widget) render.Widget {
	keys := make([]string, 0, len(things))
	for key := range things {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hbox := &VBox{
		Items: make([]render.Widget, len(things)),
	}
	for i, key := range keys {
		hbox.Items[i] = &HBox{
			Items:   []render.Widget{T(key), &Padding{Right: 1, Body: T(":")}, things[key]},
			Spacing: 0,
		}
	}
	return &Padding{Left: 3, Body: hbox}
}

// List returns a widget for displaying itemized list of other widgets.
func List(marker string, things ...render.Widget) render.Widget {
	hbox := &VBox{
		Items: make([]render.Widget, len(things)),
	}
	for i, item := range things {
		hbox.Items[i] = &HBox{
			Items: []render.Widget{&Padding{Left: 1, Right: 1, Body: T(marker)}, item},
		}
	}
	return hbox
}
