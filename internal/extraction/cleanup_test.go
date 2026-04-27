package extraction

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain JSON object passes through",
			in:   `{"a":1,"b":2}`,
			want: `{"a":1,"b":2}`,
		},
		{
			name: "plain JSON array passes through",
			in:   `[1,2,3]`,
			want: `[1,2,3]`,
		},
		{
			name: "fenced with language tag",
			in:   "```json\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "fenced without language tag",
			in:   "```\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "JSON followed by trailing fence (multi-block LLM output)",
			in:   "{\"a\":1}\n```\n\n```json\n{\"b\":2}\n```",
			want: `{"a":1}`,
		},
		{
			name: "leading whitespace trimmed",
			in:   "   {\"a\":1}   ",
			want: `{"a":1}`,
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "only whitespace",
			in:   "   \n   ",
			want: "",
		},
		{
			name: "no JSON, no fences — passes through",
			in:   `not a JSON document`,
			want: `not a JSON document`,
		},
		{
			name: "unclosed fence",
			in:   "```json\n{\"a\":1}",
			want: `{"a":1}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, StripCodeFences(tc.in))
		})
	}
}
