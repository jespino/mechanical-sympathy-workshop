//go:build !solution

package inlesc

import "strings"

type CustomerAfter struct {
	Name  string
	Email string
}

// TODO: Move the validation and field assignment into an init method.
// Leave a small NewCustomerAfter that allocates, calls init and returns.
// Keep CountValidAfter unchanged: its callers should not need a new API.
func NewCustomerAfter(input Signup) (*CustomerAfter, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrName
	}
	email := strings.TrimSpace(input.Email)
	if strings.Count(email, "@") != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return nil, ErrEmail
	}
	return &CustomerAfter{Name: name, Email: email}, nil
}

func CountValidAfter(signups []Signup) int {
	var count int
	for _, input := range signups {
		customer, err := NewCustomerAfter(input)
		if err == nil && customer.Name != "" {
			count++
		}
	}
	return count
}
