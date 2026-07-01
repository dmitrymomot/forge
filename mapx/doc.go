// Package mapx provides generic map helpers that the standard library maps
// package does not: Merge, MapValues, Invert, Filter, Entries/FromEntries, and
// an insertion-ordered Ordered[K,V] with order-preserving JSON.
//
// Like slicex, mapx is a gap-filler and does NOT re-implement or re-export
// stdlib maps functions (Clone, Keys, Values, Equal, Copy, DeleteFunc). Import
// "maps" directly alongside mapx.
package mapx
