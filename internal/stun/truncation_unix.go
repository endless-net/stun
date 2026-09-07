//go:build !windows

package stun

import (
	"errors"
	"syscall"
)

func isDatagramTruncated(err error) bool {
	return errors.Is(err, syscall.EMSGSIZE)
}
