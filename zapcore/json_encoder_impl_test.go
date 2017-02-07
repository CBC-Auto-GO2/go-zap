// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package zapcore

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"go.uber.org/zap/internal/multierror"

	"github.com/stretchr/testify/assert"
)

func TestJSONClone(t *testing.T) {
	// The parent encoder is created with plenty of excess capacity.
	parent := &jsonEncoder{
		bytes: make([]byte, 0, 128),
	}
	clone := parent.Clone()

	// Adding to the parent shouldn't affect the clone, and vice versa.
	parent.AddString("foo", "bar")
	clone.AddString("baz", "bing")

	assertJSON(t, `"foo":"bar"`, parent)
	assertJSON(t, `"baz":"bing"`, clone.(*jsonEncoder))
}

func TestJSONEscaping(t *testing.T) {
	enc := &jsonEncoder{}
	// Test all the edge cases of JSON escaping directly.
	cases := map[string]string{
		// ASCII.
		`foo`: `foo`,
		// Special-cased characters.
		`"`: `\"`,
		`\`: `\\`,
		// Special-cased characters within everyday ASCII.
		`foo"foo`: `foo\"foo`,
		"foo\n":   `foo\n`,
		// Special-cased control characters.
		"\n": `\n`,
		"\r": `\r`,
		"\t": `\t`,
		// \b and \f are sometimes backslash-escaped, but this representation is also
		// conformant.
		"\b": `\u0008`,
		"\f": `\u000c`,
		// The standard lib special-cases angle brackets and ampersands by default,
		// because it wants to protect users from browser exploits. In a logging
		// context, we shouldn't special-case these characters.
		"<": "<",
		">": ">",
		"&": "&",
		// ASCII bell - not special-cased.
		string(byte(0x07)): `\u0007`,
		// Astral-plane unicode.
		`☃`: `☃`,
		// Decodes to (RuneError, 1)
		"\xed\xa0\x80":    `\ufffd\ufffd\ufffd`,
		"foo\xed\xa0\x80": `foo\ufffd\ufffd\ufffd`,
	}
	for input, output := range cases {
		enc.truncate()
		enc.safeAddString(input)
		assertJSON(t, output, enc)
	}
}

func TestJSONEncoderObjectFields(t *testing.T) {
	tests := []struct {
		desc     string
		expected string
		f        func(Encoder)
	}{
		{"bool", `"k\\":true`, func(e Encoder) { e.AddBool(`k\`, true) }}, // test key escaping once
		{"bool", `"k":true`, func(e Encoder) { e.AddBool("k", true) }},
		{"bool", `"k":false`, func(e Encoder) { e.AddBool("k", false) }},
		{"byte", `"k":1`, func(e Encoder) { e.AddByte("k", 1) }},
		{"complex128", `"k":"1+2i"`, func(e Encoder) { e.AddComplex128("k", 1+2i) }},
		{"complex64", `"k":"1+2i"`, func(e Encoder) { e.AddComplex64("k", 1+2i) }},
		{"duration", `"k":0.000000001`, func(e Encoder) { e.AddDuration("k", 1) }},
		{"float64", `"k":1`, func(e Encoder) { e.AddFloat64("k", 1.0) }},
		{"float64", `"k":10000000000`, func(e Encoder) { e.AddFloat64("k", 1e10) }},
		{"float64", `"k":"NaN"`, func(e Encoder) { e.AddFloat64("k", math.NaN()) }},
		{"float64", `"k":"+Inf"`, func(e Encoder) { e.AddFloat64("k", math.Inf(1)) }},
		{"float64", `"k":"-Inf"`, func(e Encoder) { e.AddFloat64("k", math.Inf(-1)) }},
		{"float32", `"k":1`, func(e Encoder) { e.AddFloat32("k", 1.0) }},
		{"float32", `"k":10000000000`, func(e Encoder) { e.AddFloat32("k", 1e10) }},
		{"float32", `"k":"NaN"`, func(e Encoder) { e.AddFloat32("k", float32(math.NaN())) }},
		{"float32", `"k":"+Inf"`, func(e Encoder) { e.AddFloat32("k", float32(math.Inf(1))) }},
		{"float32", `"k":"-Inf"`, func(e Encoder) { e.AddFloat32("k", float32(math.Inf(-1))) }},
		{"int", `"k":42`, func(e Encoder) { e.AddInt("k", 42) }},
		{"int64", `"k":42`, func(e Encoder) { e.AddInt64("k", 42) }},
		{"int32", `"k":42`, func(e Encoder) { e.AddInt32("k", 42) }},
		{"int16", `"k":42`, func(e Encoder) { e.AddInt16("k", 42) }},
		{"int8", `"k":42`, func(e Encoder) { e.AddInt8("k", 42) }},
		{"rune", `"k":42`, func(e Encoder) { e.AddRune("k", rune(42)) }},
		{"string", `"k":"v\\"`, func(e Encoder) { e.AddString(`k`, `v\`) }},
		{"string", `"k":"v"`, func(e Encoder) { e.AddString("k", "v") }},
		{"string", `"k":""`, func(e Encoder) { e.AddString("k", "") }},
		{"time", `"k":1`, func(e Encoder) { e.AddTime("k", time.Unix(1, 0)) }},
		{"uint", `"k":42`, func(e Encoder) { e.AddUint("k", 42) }},
		{"uint64", `"k":42`, func(e Encoder) { e.AddUint64("k", 42) }},
		{"uint32", `"k":42`, func(e Encoder) { e.AddUint32("k", 42) }},
		{"uint16", `"k":42`, func(e Encoder) { e.AddUint16("k", 42) }},
		{"uint8", `"k":42`, func(e Encoder) { e.AddUint8("k", 42) }},
		{"uintptr", `"k":42`, func(e Encoder) { e.AddUintptr("k", 42) }},
		{
			desc:     "object (success)",
			expected: `"k":{"loggable":"yes"}`,
			f: func(e Encoder) {
				assert.NoError(t, e.AddObject("k", loggable{true}), "Unexpected error calling MarshalLogObject.")
			},
		},
		{
			desc:     "object (error)",
			expected: `"k":{}`,
			f: func(e Encoder) {
				assert.Error(t, e.AddObject("k", loggable{false}), "Expected an error calling MarshalLogObject.")
			},
		},
		{
			desc:     "object (with nested array)",
			expected: `"turducken":{"ducks":[{"in":"chicken"},{"in":"chicken"}]}`,
			f: func(e Encoder) {
				assert.NoError(
					t,
					e.AddObject("turducken", turducken{}),
					"Unexpected error calling MarshalLogObject with nested ObjectMarshalers and ArrayMarshalers.",
				)
			},
		},
		{
			desc:     "array (with nested object)",
			expected: `"turduckens":[{"ducks":[{"in":"chicken"},{"in":"chicken"}]},{"ducks":[{"in":"chicken"},{"in":"chicken"}]}]`,
			f: func(e Encoder) {
				assert.NoError(
					t,
					e.AddArray("turduckens", turduckens(2)),
					"Unexpected error calling MarshalLogObject with nested ObjectMarshalers and ArrayMarshalers.",
				)
			},
		},
		{
			desc:     "array (success)",
			expected: `"k":[true]`,
			f: func(e Encoder) {
				assert.NoError(t, e.AddArray(`k`, loggable{true}), "Unexpected error calling MarshalLogArray.")
			},
		},
		{
			desc:     "array (error)",
			expected: `"k":[]`,
			f: func(e Encoder) {
				assert.Error(t, e.AddArray("k", loggable{false}), "Expected an error calling MarshalLogArray.")
			},
		},
		{
			desc:     "reflect (success)",
			expected: `"k":{"loggable":"yes"}`,
			f: func(e Encoder) {
				assert.NoError(t, e.AddReflected("k", map[string]string{"loggable": "yes"}), "Unexpected error JSON-serializing a map.")
			},
		},
		{
			desc:     "reflect (failure)",
			expected: "",
			f: func(e Encoder) {
				assert.Error(t, e.AddReflected("k", noJSON{}), "Unexpected success JSON-serializing a noJSON.")
			},
		},
		{
			desc: "namespace",
			// EncodeEntry is responsible for closing all open namespaces.
			expected: `"outermost":{"outer":{"foo":1,"inner":{"foo":2,"innermost":{`,
			f: func(e Encoder) {
				e.OpenNamespace("outermost")
				e.OpenNamespace("outer")
				e.AddInt("foo", 1)
				e.OpenNamespace("inner")
				e.AddInt("foo", 2)
				e.OpenNamespace("innermost")
			},
		},
	}

	for _, tt := range tests {
		assertOutput(t, tt.desc, tt.expected, tt.f)
	}
}

func TestJSONEncoderArrays(t *testing.T) {
	tests := []struct {
		desc     string
		expected string // expect f to be called twice
		f        func(ArrayEncoder)
	}{
		{"bool", `[true,true]`, func(e ArrayEncoder) { e.AppendBool(true) }},
		{"byte", `[1,1]`, func(e ArrayEncoder) { e.AppendByte(1) }},
		{"complex128", `["1+2i","1+2i"]`, func(e ArrayEncoder) { e.AppendComplex128(1 + 2i) }},
		{"complex64", `["1+2i","1+2i"]`, func(e ArrayEncoder) { e.AppendComplex64(1 + 2i) }},
		{"durations", `[0.000000002,0.000000002]`, func(e ArrayEncoder) { e.AppendDuration(2) }},
		{"float64", `[3.14,3.14]`, func(e ArrayEncoder) { e.AppendFloat64(3.14) }},
		{"float32", `[3.14,3.14]`, func(e ArrayEncoder) { e.AppendFloat32(3.14) }},
		{"int", `[42,42]`, func(e ArrayEncoder) { e.AppendInt(42) }},
		{"int64", `[42,42]`, func(e ArrayEncoder) { e.AppendInt64(42) }},
		{"int32", `[42,42]`, func(e ArrayEncoder) { e.AppendInt32(42) }},
		{"int16", `[42,42]`, func(e ArrayEncoder) { e.AppendInt16(42) }},
		{"int8", `[42,42]`, func(e ArrayEncoder) { e.AppendInt8(42) }},
		{"rune", `[1,1]`, func(e ArrayEncoder) { e.AppendRune(1) }},
		{"string", `["k","k"]`, func(e ArrayEncoder) { e.AppendString("k") }},
		{"string", `["k\\","k\\"]`, func(e ArrayEncoder) { e.AppendString(`k\`) }},
		{"times", `[1,1]`, func(e ArrayEncoder) { e.AppendTime(time.Unix(1, 0)) }},
		{"uint", `[42,42]`, func(e ArrayEncoder) { e.AppendUint(42) }},
		{"uint64", `[42,42]`, func(e ArrayEncoder) { e.AppendUint64(42) }},
		{"uint32", `[42,42]`, func(e ArrayEncoder) { e.AppendUint32(42) }},
		{"uint16", `[42,42]`, func(e ArrayEncoder) { e.AppendUint16(42) }},
		{"uint8", `[42,42]`, func(e ArrayEncoder) { e.AppendUint8(42) }},
		{"uintptr", `[42,42]`, func(e ArrayEncoder) { e.AppendUintptr(42) }},
		{
			desc:     "arrays (success)",
			expected: `[[true],[true]]`,
			f: func(arr ArrayEncoder) {
				assert.NoError(t, arr.AppendArray(ArrayMarshalerFunc(func(inner ArrayEncoder) error {
					inner.AppendBool(true)
					return nil
				})), "Unexpected error appending an array.")
			},
		},
		{
			desc:     "arrays (error)",
			expected: `[[true],[true]]`,
			f: func(arr ArrayEncoder) {
				assert.Error(t, arr.AppendArray(ArrayMarshalerFunc(func(inner ArrayEncoder) error {
					inner.AppendBool(true)
					return errors.New("fail")
				})), "Expected an error appending an array.")
			},
		},
		{
			desc:     "objects (success)",
			expected: `[{"loggable":"yes"},{"loggable":"yes"}]`,
			f: func(arr ArrayEncoder) {
				assert.NoError(t, arr.AppendObject(loggable{true}), "Unexpected error appending an object.")
			},
		},
		{
			desc:     "objects (error)",
			expected: `[{},{}]`,
			f: func(arr ArrayEncoder) {
				assert.Error(t, arr.AppendObject(loggable{false}), "Expected an error appending an object.")
			},
		},
		{
			desc:     "reflect (success)",
			expected: `[{"foo":5},{"foo":5}]`,
			f: func(arr ArrayEncoder) {
				assert.NoError(
					t,
					arr.AppendReflected(map[string]int{"foo": 5}),
					"Unexpected an error appending an object with reflection.",
				)
			},
		},
		{
			desc:     "reflect (error)",
			expected: `[]`,
			f: func(arr ArrayEncoder) {
				assert.Error(
					t,
					arr.AppendReflected(noJSON{}),
					"Unexpected an error appending an object with reflection.",
				)
			},
		},
	}

	for _, tt := range tests {
		f := func(enc Encoder) error {
			return enc.AddArray("array", ArrayMarshalerFunc(func(arr ArrayEncoder) error {
				tt.f(arr)
				tt.f(arr)
				return nil
			}))
		}
		assertOutput(t, tt.desc, `"array":`+tt.expected, func(enc Encoder) {
			err := f(enc)
			assert.NoError(t, err, "Unexpected error adding array to JSON encoder.")
		})
	}
}

func assertJSON(t *testing.T, expected string, enc *jsonEncoder) {
	assert.Equal(t, expected, string(enc.bytes), "Encoded JSON didn't match expectations.")
}

func assertOutput(t testing.TB, desc string, expected string, f func(Encoder)) {
	enc := &jsonEncoder{EncoderConfig: &EncoderConfig{
		EncodeTime:     EpochTimeEncoder,
		EncodeDuration: SecondsDurationEncoder,
	}}
	f(enc)
	assert.Equal(t, expected, string(enc.bytes), "Unexpected encoder output after adding a %s.", desc)

	enc.truncate()
	enc.AddString("foo", "bar")
	f(enc)
	expectedPrefix := `"foo":"bar"`
	if expected != "" {
		// If we expect output, it should be comma-separated from the previous
		// field.
		expectedPrefix += ","
	}
	assert.Equal(t, expectedPrefix+expected, string(enc.bytes), "Unexpected encoder output after adding a %s as a second field.", desc)
}

// Nested Array- and ObjectMarshalers.
type turducken struct{}

func (t turducken) MarshalLogObject(enc ObjectEncoder) error {
	return enc.AddArray("ducks", ArrayMarshalerFunc(func(arr ArrayEncoder) error {
		for i := 0; i < 2; i++ {
			arr.AppendObject(ObjectMarshalerFunc(func(inner ObjectEncoder) error {
				inner.AddString("in", "chicken")
				return nil
			}))
		}
		return nil
	}))
}

type turduckens int

func (t turduckens) MarshalLogArray(enc ArrayEncoder) error {
	var errs multierror.Error
	tur := turducken{}
	for i := 0; i < int(t); i++ {
		errs = errs.Append(enc.AppendObject(tur))
	}
	return errs.AsError()
}

type loggable struct{ bool }

func (l loggable) MarshalLogObject(enc ObjectEncoder) error {
	if !l.bool {
		return errors.New("can't marshal")
	}
	enc.AddString("loggable", "yes")
	return nil
}

func (l loggable) MarshalLogArray(enc ArrayEncoder) error {
	if !l.bool {
		return errors.New("can't marshal")
	}
	enc.AppendBool(true)
	return nil
}

type noJSON struct{}

func (nj noJSON) MarshalJSON() ([]byte, error) {
	return nil, errors.New("no")
}

func zapEncodeString(s string) []byte {
	enc := &jsonEncoder{}
	// Escape and quote a string using our encoder.
	var ret []byte
	enc.safeAddString(s)
	ret = make([]byte, 0, len(enc.bytes)+2)
	ret = append(ret, '"')
	ret = append(ret, enc.bytes...)
	ret = append(ret, '"')
	return ret
}

func roundTripsCorrectly(original string) bool {
	// Encode using our encoder, decode using the standard library, and assert
	// that we haven't lost any information.
	encoded := zapEncodeString(original)

	var decoded string
	err := json.Unmarshal(encoded, &decoded)
	if err != nil {
		return false
	}
	return original == decoded
}

type ASCII string

func (s ASCII) Generate(r *rand.Rand, size int) reflect.Value {
	bs := make([]byte, size)
	for i := range bs {
		bs[i] = byte(r.Intn(128))
	}
	a := ASCII(bs)
	return reflect.ValueOf(a)
}

func asciiRoundTripsCorrectly(s ASCII) bool {
	return roundTripsCorrectly(string(s))
}

func TestJSONQuick(t *testing.T) {
	// Test the full range of UTF-8 strings.
	err := quick.Check(roundTripsCorrectly, &quick.Config{MaxCountScale: 100.0})
	if err != nil {
		t.Error(err.Error())
	}

	// Focus on ASCII strings.
	err = quick.Check(asciiRoundTripsCorrectly, &quick.Config{MaxCountScale: 100.0})
	if err != nil {
		t.Error(err.Error())
	}
}