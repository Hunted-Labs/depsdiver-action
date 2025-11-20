package pkg1

import (
	"regexp"
	"time"
)

func ValidateEmail(email string) bool {
	pattern := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return pattern.MatchString(email)
}

func GetCurrentTimestamp() time.Time {
	return time.Now()
}

