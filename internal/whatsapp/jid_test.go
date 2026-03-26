package whatsapp

import (
	"reflect"
	"testing"
)

func TestCandidatePhonesBrazilianVariants(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "adds ninth digit for 8-digit brazilian local number",
			input:    "554184634146",
			expected: []string{"+554184634146", "+5541984634146"},
		},
		{
			name:     "keeps exact and removes ninth digit variant",
			input:    "5541984634146",
			expected: []string{"+5541984634146", "+554184634146"},
		},
		{
			name:     "non brazilian number only returns normalized input",
			input:    "+14155550123",
			expected: []string{"+14155550123"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := CandidatePhones(test.input)
			if !reflect.DeepEqual(result, test.expected) {
				t.Fatalf("unexpected candidates: got %v want %v", result, test.expected)
			}
		})
	}
}
