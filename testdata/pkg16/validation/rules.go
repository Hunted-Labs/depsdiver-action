package validation

import (
	"regexp"
	"strings"
)

type Rule func(string) bool

func Required(value string) bool {
	return strings.TrimSpace(value) != ""
}

func MinLength(min int) Rule {
	return func(value string) bool {
		return len(value) >= min
	}
}

func MaxLength(max int) Rule {
	return func(value string) bool {
		return len(value) <= max
	}
}

func MatchesPattern(pattern string) Rule {
	re := regexp.MustCompile(pattern)
	return func(value string) bool {
		return re.MatchString(value)
	}
}

func Validate(value string, rules ...Rule) bool {
	for _, rule := range rules {
		if !rule(value) {
			return false
		}
	}
	return true
}

