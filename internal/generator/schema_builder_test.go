package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"camelCase", "camel_case"},
		{"kebab-case", "kebab_case"},
		{"snake_case", "snake_case"},
		{"PascalCase", "pascal_case"},
		{"movie_id", "movie_id"},
		// Dot-notation params (TMDb, Elasticsearch style)
		{"primary_release_date.gte", "primary_release_date_gte"},
		{"vote_average.gte", "vote_average_gte"},
		{"vote_average.lte", "vote_average_lte"},
		{"vote_count.gte", "vote_count_gte"},
		{"field.nested.deep", "field_nested_deep"},
		// Combined dots and hyphens
		{"with.dots-and-hyphens", "with_dots_and_hyphens"},
		// No transformation needed
		{"simple", "simple"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toSnakeCase(tt.input))
		})
	}
}
