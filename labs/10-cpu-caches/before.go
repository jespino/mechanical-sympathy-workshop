// Package cpucaches is the lab for module 10: CPU caches.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package cpucaches

import "math/rand/v2"

// ColumnSumsBefore returns the sum of each column of the n×n matrix m, stored
// in row-major order (row i is m[i*n : i*n+n]). It walks the matrix column by
// column, which is the natural way to write "sum of each column".
func ColumnSumsBefore(m []float64, n int) []float64 {
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

// Account is a 64-byte record: exactly one cache line.
type Account struct {
	ID      int64
	Balance int64
	Created int64
	Flags   int64
	Owner   [32]byte
}

func newAccount(i int) Account {
	return Account{ID: int64(i), Balance: int64(i%1000) - 300, Created: int64(i) * 17, Flags: int64(i & 7)}
}

// PtrLedgerBefore keeps a pointer to every account. The accounts were
// allocated one by one and the slice was later re-ordered (think of accounts
// loaded at different times, or looked up through a map), so consecutive
// pointers point all over the heap.
type PtrLedgerBefore struct {
	accounts []*Account
}

// NewPtrLedgerBefore builds a ledger with n accounts.
func NewPtrLedgerBefore(n int) *PtrLedgerBefore {
	accounts := make([]*Account, n)
	for i := range accounts {
		a := newAccount(i)
		accounts[i] = &a
	}
	r := rand.New(rand.NewPCG(1, 2))
	r.Shuffle(n, func(i, j int) { accounts[i], accounts[j] = accounts[j], accounts[i] })
	return &PtrLedgerBefore{accounts: accounts}
}

// Total returns the sum of all balances.
func (l *PtrLedgerBefore) Total() int64 {
	var t int64
	for _, a := range l.accounts {
		t += a.Balance
	}
	return t
}

// LedgerBefore stores accounts by value, as an array of structs.
type LedgerBefore struct {
	accounts []Account
}

// NewLedgerBefore builds a ledger with n accounts.
func NewLedgerBefore(n int) *LedgerBefore {
	accounts := make([]Account, n)
	for i := range accounts {
		accounts[i] = newAccount(i)
	}
	return &LedgerBefore{accounts: accounts}
}

// Total returns the sum of all balances. It only needs 8 bytes of each
// 64-byte account.
func (l *LedgerBefore) Total() int64 {
	var t int64
	for i := range l.accounts {
		t += l.accounts[i].Balance
	}
	return t
}
