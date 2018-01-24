package agent

import (
	"strings"
)

// A set of strings that tracks the count of each member (a multiset).
// Blame go's lack of generics for this one.
type StringBag map[string]int

// Splits the string on the given separator and returns it as a bag.
func SplitStringBag(s string, sep string) StringBag {
	split := strings.Split(s, sep)
	var values []string
	for _, s := range split {
		tmp := strings.TrimSpace(s)
		if len(tmp) > 0 {
			values = append(values, tmp)
		}
	}
	return NewStringBag(values)
}

// Turns a list of strings into a bag.
func NewStringBag(values []string) StringBag {
	s := make(StringBag)
	s.AddAll(values)
	return s
}

// Adds all values to the bag.
func (s StringBag) AddAll(values []string) {
	for _, v := range values {
		s.Add(v)
	}
}

// Removes all values from the bag.
func (s StringBag) RemoveAll(values []string) {
	for _, v := range values {
		s.Remove(v)
	}
}

// Adds a value to the bag.
// Returns true if the bag did not contain the value.
func (s StringBag) Add(value string) bool {
	count := s[value]
	s[value] += 1
	return count == 0
}

// Removes a value from the bag.
// Returns false if the bag did not contain the value.
func (s StringBag) Remove(value string) bool {
	count := s[value]
	if count == 0 {
		return false
	}
	count--
	if count > 0 {
		s[value] = count
	} else {
		delete(s, value)
	}
	return true
}

// Whether the bag contains the given value.
func (s StringBag) Contains(value string) bool {
	_, ok := s[value]
	return ok
}

// Returns a count of all matching values contained in this bag.
func (s StringBag) Matches(values []string) int {
	count := 0
	for _, v := range values {
		count += s[v]
	}
	return count
}

// Returns all values common between both bags.
func (s StringBag) Intersection(other StringBag) []string {
	var common []string
	for k := range s {
		if other.Contains(k) {
			common = append(common, k)
		}
	}
	return common
}

// Returns all values contained in this bag but not in the other bag.
func (s StringBag) Difference(other StringBag) []string {
	var diff []string
	for k := range s {
		if !other.Contains(k) {
			diff = append(diff, k)
		}
	}
	return diff
}

// Make new copy of StringBag
func (s StringBag) NewCopy() StringBag {
	nsb := make(StringBag)
	for k, v := range s {
		nsb[k] = v
	}
	return nsb
}
