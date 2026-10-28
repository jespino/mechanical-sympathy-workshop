//go:build !solution

package cpucaches

import "math/rand/v2"

// This is your file. It starts as a copy of before.go: change it so that it
// is friendlier to the CPU caches, then run the benchmarks to compare against
// before.go. The reference solution is in solution.go (run with -tags
// solution).

// ColumnSumsAfter returns the sum of each column of the n×n matrix m, stored
// in row-major order (row i is m[i*n : i*n+n]).
// TODO: consecutive i are n*8 bytes apart in memory. Walk the matrix in the
// order it is stored (row by row) and add each element to sums[j].
func ColumnSumsAfter(m []float64, n int) []float64 {
	sums := make([]float64, n)
	for j := 0; j < n; j++ {
		var s float64
		for i := 0; i < n; i++ {
			s += m[i*n+j]
		}
		sums[j] = s
	}
	return sums
}

// PtrLedgerAfter keeps a pointer to every account.
// TODO: every a.Balance is a pointer dereference to a random place in the
// heap. Store the accounts by value ([]Account) so they are contiguous.
type PtrLedgerAfter struct {
	accounts []*Account
}

// NewPtrLedgerAfter builds a ledger with n accounts.
func NewPtrLedgerAfter(n int) *PtrLedgerAfter {
	accounts := make([]*Account, n)
	for i := range accounts {
		a := newAccount(i)
		accounts[i] = &a
	}
	r := rand.New(rand.NewPCG(1, 2))
	r.Shuffle(n, func(i, j int) { accounts[i], accounts[j] = accounts[j], accounts[i] })
	return &PtrLedgerAfter{accounts: accounts}
}

// Total returns the sum of all balances.
func (l *PtrLedgerAfter) Total() int64 {
	var t int64
	for _, a := range l.accounts {
		t += a.Balance
	}
	return t
}

// LedgerAfter stores accounts by value, as an array of structs.
// TODO: Total only reads Balance, but every cache line it loads is a whole
// Account. Switch to a struct of arrays: one slice per field.
type LedgerAfter struct {
	accounts []Account
}

// NewLedgerAfter builds a ledger with n accounts.
func NewLedgerAfter(n int) *LedgerAfter {
	accounts := make([]Account, n)
	for i := range accounts {
		accounts[i] = newAccount(i)
	}
	return &LedgerAfter{accounts: accounts}
}

// Total returns the sum of all balances.
func (l *LedgerAfter) Total() int64 {
	var t int64
	for i := range l.accounts {
		t += l.accounts[i].Balance
	}
	return t
}
