package whatsapp

import (
	"fmt"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

var nondigitPattern = regexp.MustCompile(`\D`)

func ParseTargetJID(raw string) (types.JID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return types.EmptyJID, fmt.Errorf("target is required")
	}

	if strings.Contains(raw, "@") {
		return types.ParseJID(raw)
	}

	phone := strings.TrimPrefix(NormalizePhone(raw), "+")
	if phone == "" {
		return types.EmptyJID, fmt.Errorf("invalid phone number")
	}

	return types.NewJID(phone, types.DefaultUserServer), nil
}

func NormalizePhone(raw string) string {
	clean := nondigitPattern.ReplaceAllString(raw, "")
	if clean == "" {
		return ""
	}
	return "+" + clean
}

func CandidatePhones(raw string) []string {
	normalized := NormalizePhone(raw)
	if normalized == "" {
		return nil
	}

	digits := strings.TrimPrefix(normalized, "+")
	candidates := []string{normalized}

	if strings.HasPrefix(digits, "55") {
		local := digits[2:]
		switch {
		case len(local) == 10:
			candidates = append(candidates, "+55"+local[:2]+"9"+local[2:])
		case len(local) == 11 && strings.HasPrefix(local[2:], "9"):
			candidates = append(candidates, "+55"+local[:2]+local[3:])
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}

	return unique
}
