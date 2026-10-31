// Package inlesc is the lab for module 07: inlining and escape analysis.
// before.go is the baseline. Do not edit it.
package inlesc

import (
	"errors"
	"strings"
)

// Signup is one row from a customer signup form.
type Signup struct {
	Name  string
	Email string
}

var (
	ErrName  = errors.New("name is required")
	ErrEmail = errors.New("email must contain one @ with text on both sides")
)

// CustomerBefore holds the cleaned form fields.
type CustomerBefore struct {
	Name  string
	Email string
}

// NewCustomerBefore trims fields and applies the form's basic checks.
// These checks are for the exercise, not a complete email-address validator.
func NewCustomerBefore(input Signup) (*CustomerBefore, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrName
	}
	email := strings.TrimSpace(input.Email)
	if strings.Count(email, "@") != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return nil, ErrEmail
	}
	return &CustomerBefore{Name: name, Email: email}, nil
}

// CountValidBefore counts signups that pass the form checks.
func CountValidBefore(signups []Signup) int {
	var count int
	for _, input := range signups {
		customer, err := NewCustomerBefore(input)
		if err == nil && customer.Name != "" {
			count++
		}
	}
	return count
}
