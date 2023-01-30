package kvstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSet(t *testing.T) {
	store := NewKvStore()

	overwritten := store.Set("key", "oldvalue")
	assert.False(t, overwritten)

	overwritten = store.Set("key", "value")
	assert.True(t, overwritten)

	value, found := store.Get("key")
	assert.Equal(t, "value", value)
	assert.Equal(t, true, found)
}

func TestUnknownKey(t *testing.T) {
	store := NewKvStore()
	value, found := store.Get("unknownKey")
	assert.Equal(t, "", value)
	assert.Equal(t, false, found)
}

func TestDelete(t *testing.T) {
	store := NewKvStore()
	store.Set("key", "value")
	store.Delete("key")
	value, found := store.Get("key")
	assert.Equal(t, "", value)
	assert.Equal(t, false, found)
}
