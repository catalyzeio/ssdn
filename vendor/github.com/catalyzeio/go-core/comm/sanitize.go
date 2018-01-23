package comm

import (
	"bytes"
)

func SanitizeServices(services []string) []string {
	if services == nil {
		return nil
	}
	sanitized := make([]string, len(services))
	for i, v := range services {
		sanitized[i] = SanitizeService(v)
	}
	return sanitized
}

func SanitizeService(service string) string {
	var buffer bytes.Buffer
	for _, c := range service {
		buffer.WriteRune(sanitizeRune(c))
	}
	return buffer.String()
}

func sanitizeRune(c rune) rune {
	if c >= '0' && c <= '9' {
		return c
	}
	if c >= 'a' && c <= 'z' {
		return c
	}
	if c >= 'A' && c <= 'Z' {
		return c
	}
	if c == '_' || c == '-' {
		return c
	}
	return '_'
}
