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

package clientutil_test

import (
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/client/clientutil"
	. "gopkg.in/check.v1"
)

type parseSuite struct{}

var _ = Suite(&parseSuite{})

func (s *parseSuite) TestParseConfigValues(c *C) {
	// check basic setting and unsetting behaviour
	confValues, keys, err := clientutil.ParseConfigValues([]string{"foo=bar", "baz!"}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": "bar",
		"baz": nil,
	})
	c.Assert(keys, DeepEquals, []string{"foo", "baz"})

	// parses JSON
	opts := &clientutil.ParseConfigOptions{
		Typed: true,
	}
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, opts)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": json.Number("1"),
		},
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// stores strings w/o parsing
	opts.String = true
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, opts)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": `{"bar": 1}`,
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// default is to parse
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": json.Number("1"),
		},
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// unless it's not valid JSON
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1`}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": `{"bar": 1`,
	})
	c.Assert(keys, DeepEquals, []string{"foo"})
}

func (s *parseSuite) TestJSONValidation(c *C) {

	invalidJson := []string{
		`key={"key0":"val0",:"val1","key2":{"key3":"val2","key4":"val3","key5":{"key6":"val4","key7":"val5"}},"key8":{"key9":{"key10":"val6"}}}`,
		`key={"key0":"val0","key1":"val1","key2"{"key3":"val2","key4":"val3",` +
			`"key5":{"key6":"val4","key7":"val5"}},"key8":{"key9":{"key10":"val6"}},` +
			`"key11":[{"key12":"val7"},{"key13":"val8"},{"key14":["val9","val10"]}]}`,
		`key={"key0": ["a", "b" "c"]}`,
		`key=[{"key1": "a"} {"key2": "b"}]`,
		`key=[{"key1": "a"}, {"key2": "b}]`,
		`key={"key1": [123, "abc"}`,
		`key={"key1": {"key2": "a", "key2",: ["b"]}}`,
		`key={"key": {"key1": ,"value"}}`,
	}

	for _, json := range invalidJson {
		_, _, err := clientutil.ParseConfigValues([]string{json}, nil)
		// When ParseConfigValues is called with invalid JSON,
		// it store the string as-is.
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Printf("JSON: %v\n", json)
		}
		c.Check(err, IsNil)
	}

	invalidJsonKeys := []string{
		`key={"key0":"val0","key1":"val1","key2":{"key3":"val2","key4":"val3","key5":{"INVALID":"val4","key7":"val5"}},"key8":{"key9":{"key10":"val6"}}}`,
		`key={"key0":"val0","key1":"val1","key2":{"key3":"val2","key4":"val3",` +
			`"key5":{"key6":"val4","key7":"val5"}},"key8":{"key9":{"key10":"val6"}},` +
			`"key11":[{"key12":"val7"},{"KEY13":"val8"},{"key14":["val9","val10"]}]}`,
		`key={"KEY0": ["a", "b", "c"]}`,
		`key=[{"key1": "a"}, {"KEY2": "b"}]`,
		`key={"KEY1": [123, "abc"]}`,
		`key={"key1": {"key2": "a", "KEY2": ["b"]}}`,
		`key={"KEY1": [[123, 456], [789]]}`,
		`key={"key": {"KEY1": "value"}}`,
		`key={"KEY": "val", "KEY": "val"}`,
	}

	for _, json := range invalidJsonKeys {
		_, _, err := clientutil.ParseConfigValues([]string{json}, nil)
		if err == nil {
			fmt.Printf("Failed on:\n%v\n", json)
		}
		c.Check(err, ErrorMatches, "invalid option name:.*", Commentf("json: `%s`"))
	}
}

func (s *parseSuite) TestValidateJSONKeysHappy(c *C) {
	validJsons := []interface{}{
		// key={"key0": ["a", "b", "c"]}
		map[string]interface{}{
			"key0": []interface{}{"a", "b", "c"},
		},
		// key=[{"key1": "a"}, {"key2": "b"}]
		[]map[string]interface{}{
			{"key1": "a"},
			{"key2": "b"},
		},
		// key=[{"key1": "a"}, {"key2": "b"}]
		[]interface{}{
			map[string]interface{}{"key1": "a"},
			map[string]interface{}{"key2": "b"},
		},
		// key="123"
		"123",
		// key=123
		123,
		// key=[123, 456]
		[]interface{}{123, 456},
		// key={"key1": [123, "abc"]}
		map[string]interface{}{
			"key1": []interface{}{123, "abc"},
		},
		// key={"key1": {["a", "b"]}}
		map[string]interface{}{
			"key1": map[string]interface{}{
				"key2": []interface{}{"a", "b"},
			},
		},
		// key={"key1": [[123, 456], [789]]}
		map[string]interface{}{
			"key1": []interface{}{
				[]interface{}{123, 456},
				[]interface{}{789},
			},
		},
		// key=[[123, 456], [789]]
		[]interface{}{
			[]interface{}{123, 456},
			[]interface{}{789},
		},
		// key=[123, [456, 789]]
		[]interface{}{
			123,
			[]interface{}{456, 789},
		},
		// key={"key": {"key": "value"}}
		map[string]interface{}{
			"key": map[string]interface{}{
				"key": "value",
			},
		},
		// key={"key0":"val0","key1":"val1","key2":{"key3":"val2","key4":"val3","key5":{"key6":"val4","key7":"val5"}},"key8":{"key9":{"key10":"val6"}}}
		map[string]interface{}{
			"key0": "val0",
			"key1": "val1",
			"key2": map[string]interface{}{
				"key3": "val2",
				"key4": "val3",
				"key5": map[string]interface{}{
					"key6": "val4",
					"key7": "val5",
				},
			},
			"key8": map[string]interface{}{
				"key9": map[string]interface{}{
					"key10": "val6",
				},
			},
		},
	}

	for _, json := range validJsons {
		err := clientutil.ValidateJSONKeys(json)
		if err != nil {
			fmt.Printf("Failed on:\n%+v\n", json)
		}
		c.Check(err, IsNil)
	}

}

func (s *parseSuite) TestValidateJSONKeysUnhappy(c *C) {
	validJsons := []interface{}{
		// key={"KEY0": ["a", "b", "c"]}
		map[string]interface{}{
			"KEY0": []interface{}{"a", "b", "c"},
		},
		// key=[{"key1": "a"}, {"KEY2": "b"}]
		[]map[string]interface{}{
			{"key1": "a"},
			{"KEY2": "b"},
		},
		// key={"KEY1": [123, "abc"]}
		map[string]interface{}{
			"KEY1": []interface{}{123, "abc"},
		},
		// key={"key1": {"KEY2": ["a", "b"]}}
		map[string]interface{}{
			"key1": map[string]interface{}{
				"KEY2": []interface{}{"a", "b"},
			},
		},
		// key={"KEY1": [[123, 456], [789]]}
		map[string]interface{}{
			"KEY1": []interface{}{
				[]interface{}{123, 456},
				[]interface{}{789},
			},
		},
		// key={"key": {"KEY": "value"}}
		map[string]interface{}{
			"key": map[string]interface{}{
				"KEY": "value",
			},
		},
		// key={"key0":"val0","key1":"val1","key2":{"key3":"val2","key4":"val3","key5":{"key6":"val4","KEY7":"val5"}},"key8":{"key9":{"key10":"val6"}}}
		map[string]interface{}{
			"key0": "val0",
			"key1": "val1",
			"key2": map[string]interface{}{
				"key3": "val2",
				"key4": "val3",
				"key5": map[string]interface{}{
					"key6": "val4",
					"KEY7": "val5",
				},
			},
			"key8": map[string]interface{}{
				"key9": map[string]interface{}{
					"key10": "val6",
				},
			},
		},
		// key={"key0":"val0","key1":"val1","key2":{"key3":"val2","key4":"val3","key5":{"key6":"val4","key7":"val5"}},"key8":{"key9":{"KEY10":"val6"}}}
		map[string]interface{}{
			"key0": "val0",
			"key1": "val1",
			"key2": map[string]interface{}{
				"key3": "val2",
				"key4": "val3",
				"key5": map[string]interface{}{
					"key6": "val4",
					"key7": "val5",
				},
			},
			"key8": map[string]interface{}{
				"key9": map[string]interface{}{
					"KEY10": "val6",
				},
			},
		},
	}

	for _, json := range validJsons {
		err := clientutil.ValidateJSONKeys(json)
		if err == nil {
			fmt.Printf("Succeed on:\n%+v\n", json)
		}
		c.Check(err, ErrorMatches, "invalid option name:.*", Commentf("invalid option name:.*"))
	}

}
