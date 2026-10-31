package inlesc

import (
	"fmt"
	"testing"
)

const size = 1_000

func testSignups(n int) []Signup {
	inputs := make([]Signup, n)
	for i := range inputs {
		inputs[i] = Signup{
			Name:  fmt.Sprintf(" Customer %d ", i),
			Email: fmt.Sprintf(" customer%d@example.com ", i),
		}
	}
	return inputs
}

func TestCustomers(t *testing.T) {
	cases := []struct {
		name  string
		input Signup
		want  Signup
		err   error
	}{
		{"valid", Signup{"Ada", "ada@example.com"}, Signup{"Ada", "ada@example.com"}, nil},
		{"trim fields", Signup{" Ada \t", " ada@example.com "}, Signup{"Ada", "ada@example.com"}, nil},
		{"unicode whitespace", Signup{"\u2003Ada\u2003", "ada@example.com"}, Signup{"Ada", "ada@example.com"}, nil},
		{"blank name", Signup{" \t", "ada@example.com"}, Signup{}, ErrName},
		{"missing email", Signup{"Ada", ""}, Signup{}, ErrEmail},
		{"missing at", Signup{"Ada", "example.com"}, Signup{}, ErrEmail},
		{"two ats", Signup{"Ada", "a@@example.com"}, Signup{}, ErrEmail},
		{"missing local part", Signup{"Ada", "@example.com"}, Signup{}, ErrEmail},
		{"missing domain", Signup{"Ada", "ada@"}, Signup{}, ErrEmail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, be := NewCustomerBefore(tc.input)
			a, ae := NewCustomerAfter(tc.input)
			if be != tc.err || ae != tc.err {
				t.Fatalf("errors: before=%v after=%v want=%v", be, ae, tc.err)
			}
			if tc.err != nil {
				if b != nil || a != nil {
					t.Fatal("invalid signup returned a customer")
				}
				return
			}
			if b == nil || a == nil {
				t.Fatal("valid signup returned nil")
			}
			if b.Name != tc.want.Name || b.Email != tc.want.Email || a.Name != tc.want.Name || a.Email != tc.want.Email {
				t.Fatalf("before=%+v after=%+v want=%+v", b, a, tc.want)
			}
		})
	}
}

func TestCountValid(t *testing.T) {
	for _, n := range []int{0, 1, 100} {
		inputs := testSignups(n)
		if got := CountValidAfter(inputs); got != n {
			t.Fatalf("got %d, want %d", got, n)
		}
		inputs = append(inputs, Signup{}, Signup{"Ada", "invalid"})
		if b, a := CountValidBefore(inputs), CountValidAfter(inputs); b != n || a != n {
			t.Fatalf("before=%d after=%d want=%d", b, a, n)
		}
	}
}

var countSink int

func BenchmarkCountValid(b *testing.B) {
	inputs := testSignups(size)
	for _, impl := range []struct {
		name string
		fn   func([]Signup) int
	}{{"before", CountValidBefore}, {"after", CountValidAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				countSink = impl.fn(inputs)
			}
		})
	}
}
