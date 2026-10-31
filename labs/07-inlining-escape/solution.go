//go:build solution

package inlesc

import "strings"

type CustomerAfter struct {
	Name  string
	Email string
}

// NewCustomerAfter is small enough to inline into CountValidAfter.
func NewCustomerAfter(input Signup) (*CustomerAfter, error) {
	c := &CustomerAfter{}
	if err := c.init(input); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *CustomerAfter) init(input Signup) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ErrName
	}
	email := strings.TrimSpace(input.Email)
	if strings.Count(email, "@") != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return ErrEmail
	}
	*c = CustomerAfter{Name: name, Email: email}
	return nil
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
