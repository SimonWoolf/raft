package kvstore

type KVStore struct {
	store map[string]string
}

func NewKvStore() KVStore {
	return KVStore{
		store: make(map[string]string),
	}
}

func (k *KVStore) Get(key string) (value string, found bool) {
	value, found = k.store[key]
	return
}

func (k *KVStore) Set(key string, value string) (found bool) {
	_, found = k.store[key]
	k.store[key] = value
	return
}

func (k *KVStore) Delete(key string) (found bool) {
	_, found = k.store[key]
	delete(k.store, key)
	return
}
