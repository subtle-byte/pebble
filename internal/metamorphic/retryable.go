// Copyright 2020 The LevelDB-Go and Pebble Authors. All rights reserved. Use
// of this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

package metamorphic

import (
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/internal/errorfs"
	"github.com/cockroachdb/pebble/internal/testkeys"
)

// withRetries executes fn, retrying it whenever an errorfs.ErrInjected error
// is returned.  It returns the first nil or non-errorfs.ErrInjected error
// returned by fn.
func withRetries(fn func() error) error {
	for {
		if err := fn(); !errors.Is(err, errorfs.ErrInjected) {
			return err
		}
	}
}

// retryableIter holds an iterator and the state necessary to reset it to its
// state after the last successful operation. This allows us to retry failed
// iterator operations by running them again on a non-error iterator with the
// same pre-operation state.
type retryableIter struct {
	iter    *pebble.Iterator
	lastKey []byte

	// When filterMax is >0, this iterator filters out keys with suffixes
	// outside of the range [filterMin, filterMax). Keys without suffixes are
	// surfaced. This is used to ensure determinism regardless of whether
	// block-property filters filter keys or not.
	filterMin, filterMax uint64
}

func (i *retryableIter) shouldFilter() bool {
	k := i.iter.Key()
	n := testkeys.Comparer.Split(k)
	if n == len(k) {
		// No suffix, don't filter it.
		return false
	}
	v, err := testkeys.ParseSuffix(k[n:])
	if err != nil {
		panic(err)
	}
	ts := uint64(v)
	return ts < i.filterMin || ts >= i.filterMax
}

func (i *retryableIter) needRetry() bool {
	return errors.Is(i.iter.Error(), errorfs.ErrInjected)
}

func (i *retryableIter) withRetry(fn func()) {
	for {
		fn()
		if !i.needRetry() {
			break
		}
		for i.needRetry() {
			i.iter.SeekGE(i.lastKey)
		}
	}

	i.lastKey = i.lastKey[:0]
	if i.iter.Valid() {
		i.lastKey = append(i.lastKey, i.iter.Key()...)
	}
}

func (i *retryableIter) Close() error {
	return i.iter.Close()
}

func (i *retryableIter) Error() error {
	return i.iter.Error()
}

func (i *retryableIter) First() bool {
	var valid bool
	i.withRetry(func() {
		valid = i.iter.First()
	})
	if valid && i.shouldFilter() {
		valid = i.Next()
	}
	return valid
}

func (i *retryableIter) Key() []byte {
	return i.iter.Key()
}

func (i *retryableIter) RangeKeyChanged() bool {
	// A single operation on the retryableIter may result in many operations on
	// i.iter if we need to skip filtered keys. To provide determinism, we
	// return RangeKeyChanged()=false for all iterators configured with filters.
	//
	// TODO(jackson): We should be able to provide more test coverage here by
	// returning true if i.iter.RangeKeyChanged()=true after any of the
	// individual repositioning methods.
	return i.filterMax == 0 && i.iter.RangeKeyChanged()
}

func (i *retryableIter) HasPointAndRange() (bool, bool) {
	return i.iter.HasPointAndRange()
}

func (i *retryableIter) RangeBounds() ([]byte, []byte) {
	return i.iter.RangeBounds()
}

func (i *retryableIter) RangeKeys() []pebble.RangeKeyData {
	return i.iter.RangeKeys()
}

func (i *retryableIter) Last() bool {
	var valid bool
	i.withRetry(func() { valid = i.iter.Last() })
	if valid && i.shouldFilter() {
		valid = i.Prev()
	}
	return valid
}

func (i *retryableIter) Next() bool {
	var valid bool
	i.withRetry(func() {
		valid = i.iter.Next()
		for valid && i.shouldFilter() {
			valid = i.iter.Next()
		}
	})
	return valid
}

func (i *retryableIter) NextWithLimit(limit []byte) pebble.IterValidityState {
	var validity pebble.IterValidityState
	i.withRetry(func() {
		validity = i.iter.NextWithLimit(limit)
		for validity == pebble.IterValid && i.shouldFilter() {
			validity = i.iter.NextWithLimit(limit)
		}
	})
	return validity
}

func (i *retryableIter) Prev() bool {
	var valid bool
	i.withRetry(func() {
		valid = i.iter.Prev()
		for valid && i.shouldFilter() {
			valid = i.iter.Prev()
		}
	})
	return valid
}

func (i *retryableIter) PrevWithLimit(limit []byte) pebble.IterValidityState {
	var validity pebble.IterValidityState
	i.withRetry(func() {
		validity = i.iter.PrevWithLimit(limit)
		for validity == pebble.IterValid && i.shouldFilter() {
			validity = i.iter.PrevWithLimit(limit)
		}
	})
	return validity
}

func (i *retryableIter) SeekGE(key []byte) bool {
	var valid bool
	i.withRetry(func() { valid = i.iter.SeekGE(key) })
	if valid && i.shouldFilter() {
		valid = i.Next()
	}
	return valid
}

func (i *retryableIter) SeekGEWithLimit(key []byte, limit []byte) pebble.IterValidityState {
	var validity pebble.IterValidityState
	i.withRetry(func() { validity = i.iter.SeekGEWithLimit(key, limit) })
	if validity == pebble.IterValid && i.shouldFilter() {
		validity = i.NextWithLimit(limit)
	}
	return validity
}

func (i *retryableIter) SeekLT(key []byte) bool {
	var valid bool
	i.withRetry(func() { valid = i.iter.SeekLT(key) })
	if valid && i.shouldFilter() {
		valid = i.Prev()
	}
	return valid
}

func (i *retryableIter) SeekLTWithLimit(key []byte, limit []byte) pebble.IterValidityState {
	var validity pebble.IterValidityState
	i.withRetry(func() { validity = i.iter.SeekLTWithLimit(key, limit) })
	if validity == pebble.IterValid && i.shouldFilter() {
		validity = i.PrevWithLimit(limit)
	}
	return validity
}

func (i *retryableIter) SeekPrefixGE(key []byte) bool {
	var valid bool
	i.withRetry(func() { valid = i.iter.SeekPrefixGE(key) })
	if valid && i.shouldFilter() {
		valid = i.Next()
	}
	return valid
}

func (i *retryableIter) SetBounds(lower, upper []byte) {
	i.iter.SetBounds(lower, upper)
}

func (i *retryableIter) SetOptions(opts *pebble.IterOptions) {
	i.iter.SetOptions(opts)
}

func (i *retryableIter) Valid() bool {
	return i.iter.Valid()
}

func (i *retryableIter) Value() []byte {
	return i.iter.Value()
}
