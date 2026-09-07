package stun

import (
	"errors"
	"syscall"
)

func isDatagramTruncated(err error) bool {
	return errors.Is(err, syscall.Errno(10040)) // WSAEMSGSIZE
}
