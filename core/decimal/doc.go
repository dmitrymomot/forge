// Package decimal provides an exact base-10 fixed-point number for monetary and
// percentage math without binary-float drift.
//
// A Decimal represents value = coef × 10^(−scale) with scale ≥ 0. It keeps a
// fast int64 coefficient path and transparently promotes to math/big.Int when an
// operation would overflow, demoting back to int64 when the result again fits.
// Add/Sub/Mul are exact and never round; Div requires an explicit result scale
// and RoundingMode (HalfEven is the default mode). String preserves the stored
// scale (2.50 stays "2.50"); Cmp/Equal are scale-normalized (2.50 equals 2.5).
//
// What this is NOT: it is not arbitrary-precision "exact" division (a
// non-terminating quotient like 1/3 must name a scale+mode); it does not parse
// scientific notation in v1 (rejected as ErrSyntax); Float64 is best-effort and
// lossy — never use it for money math. For currency-aware money with a currency
// tag and penny-perfect allocation, see the money package, which builds on this.
package decimal
