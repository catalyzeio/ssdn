package comm

import (
	"crypto/rand"
	"fmt"
)

// Generates a random identifier using crypto/rand.
// The first character will be alphabetic ([a-zA-Z]), while the rest
// will be alphanumeric or '_' and '-'.
func GenerateIdentifier(n int) (string, error) {
	if n == 0 {
		return "", nil
	}
	id := make([]rune, n)
	// force first character to be alpha (don't care about modulo bias)
	if err := cryptoRandRune(52, id, 0); err != nil {
		return "", err
	}
	// the rest can use any character in the approved list
	for i := 1; i < n; i++ {
		if err := cryptoRandRune(64, id, i); err != nil {
			return "", err
		}
	}
	return string(id), nil
}

// Like GenerateIdentifier, except the first character is not forced to
// be an alphabetic character.
func GenerateChars(n int) (string, error) {
	if n == 0 {
		return "", nil
	}
	id := make([]rune, n)
	for i := 0; i < n; i++ {
		if err := cryptoRandRune(64, id, i); err != nil {
			return "", err
		}
	}
	return string(id), nil
}

func idRune(i int) rune {
	if i < 26 {
		return 'a' + rune(i)
	}
	if i < 52 {
		return 'A' + rune(i-26)
	}
	if i < 62 {
		return '0' + rune(i-52)
	}
	if i == 62 {
		return '_'
	}
	return '-'
}

func cryptoRandRune(n int, dest []rune, i int) error {
	n, err := cryptoRandByteN(n)
	if err != nil {
		return err
	}
	dest[i] = idRune(n)
	return nil
}

// Returns a random number in [0, n).
// n must be less than 0xff.
// If n is not a power of two, the returned value will suffer from
// modulo bias.
func cryptoRandByteN(n int) (int, error) {
	if n > 0xFF || n <= 0 {
		return 0, fmt.Errorf("cryptoRandByteN: invalid argument")
	}
	b := []byte{0}
	if _, err := rand.Read(b); err != nil {
		return 0, err
	}
	return int(b[0]&0xFF) % n, nil
}
