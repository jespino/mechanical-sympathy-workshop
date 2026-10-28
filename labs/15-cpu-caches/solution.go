//go:build solution

package cpucaches

// ColumnSumsAfter walks the matrix row by row, in the order it is stored.
// Every cache line it loads is used completely, the hardware prefetcher sees
// a simple sequential stream, and sums (n*8 bytes) stays in L1.
// Each column is still added in the same order (i ascending), so the
// floating-point results are bit-for-bit identical.
func ColumnSumsAfter(m []float64, n int) []float64 {
	sums := make([]float64, n)
	for i := 0; i < n; i++ {
		row := m[i*n : i*n+n]
		for j, v := range row {
			sums[j] += v
		}
	}
	return sums
}

// PtrLedgerAfter stores the accounts by value in one contiguous slice: no
// pointer to follow, and consecutive accounts are consecutive cache lines.
type PtrLedgerAfter struct {
	accounts []Account
}

// NewPtrLedgerAfter builds a ledger with n accounts.
func NewPtrLedgerAfter(n int) *PtrLedgerAfter {
	accounts := make([]Account, n)
	for i := range accounts {
		accounts[i] = newAccount(i)
	}
	return &PtrLedgerAfter{accounts: accounts}
}

// Total returns the sum of all balances.
func (l *PtrLedgerAfter) Total() int64 {
	var t int64
	for i := range l.accounts {
		t += l.accounts[i].Balance
	}
	return t
}

// LedgerAfter is a struct of arrays: each field has its own slice, so Total
// reads 8 balances per cache line instead of 1.
type LedgerAfter struct {
	ids      []int64
	balances []int64
	created  []int64
	flags    []int64
	owners   [][32]byte
}

// NewLedgerAfter builds a ledger with n accounts.
func NewLedgerAfter(n int) *LedgerAfter {
	l := &LedgerAfter{
		ids:      make([]int64, n),
		balances: make([]int64, n),
		created:  make([]int64, n),
		flags:    make([]int64, n),
		owners:   make([][32]byte, n),
	}
	for i := range n {
		a := newAccount(i)
		l.ids[i], l.balances[i], l.created[i], l.flags[i], l.owners[i] = a.ID, a.Balance, a.Created, a.Flags, a.Owner
	}
	return l
}

// Total returns the sum of all balances.
func (l *LedgerAfter) Total() int64 {
	var t int64
	for _, b := range l.balances {
		t += b
	}
	return t
}
