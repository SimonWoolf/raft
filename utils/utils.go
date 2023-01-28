package utils

import (
	"log"

	"golang.org/x/exp/constraints"
)

// can't believe this shit isn't all in the standard library

func Min[T constraints.Ordered](a T, b T) T {
	if a < b {
		return a
	}
	return b
}

func Max[T constraints.Ordered](a T, b T) T {
	if a < b {
		return b
	}
	return a
}

func Map[T, U any](slice []T, f func(T) U) []U {
	us := make([]U, len(slice))
	for i := range slice {
		us[i] = f(slice[i])
	}
	return us
}

func Reduce[T, U any](slice []T, initial U, f func(U, T) U) U {
	acc := initial
	for _, v := range slice {
		acc = f(acc, v)
	}
	return acc
}

func Filter[T any](slice []T, f func(T) bool) []T {
	var res []T
	for _, v := range slice {
		if f(v) {
			res = append(res, v)
		}
	}
	return res
}

func Find[T any](slice []T, f func(T) bool) *T {
	for _, v := range slice {
		if f(v) {
			return &v
		}
	}
	return nil
}

// NR == No Result
func MustSucceedNR(err error) {
	if err != nil {
		log.Fatalf("Unhandled error: %v", err)
	}
}

func MustSucceed[T any](res T, err error) T {
	if err != nil {
		log.Fatalf("Unhandled error: %v", err)
	}
	return res
}
