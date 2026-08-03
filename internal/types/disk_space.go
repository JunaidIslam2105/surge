package types

import (
	"errors"
	"fmt"
)

// AnnotateInsufficientDiskSpace wraps err with ErrInsufficientDiskSpace
// when the underlying cause is a platform disk-full / quota errno.
// Idempotent: if err already matches the sentinel, returns err as-is.
// Non-disk errors pass through unchanged.
func AnnotateInsufficientDiskSpace(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInsufficientDiskSpace) {
		return err
	}
	if isDiskFull(err) {
		return fmt.Errorf("%w: %w", err, ErrInsufficientDiskSpace)
	}
	return err
}

// IsInsufficientDiskSpace reports whether err is (or wraps) a disk-full
// or quota failure. Also checks raw platform errno so callers stay
// safe even if a site forgets to annotate.
func IsInsufficientDiskSpace(err error) bool {
	return err != nil && (errors.Is(err, ErrInsufficientDiskSpace) || isDiskFull(err))
}
