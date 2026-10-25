package main

import (
	"bytes"
	"encoding/json"
)

var nativeJSONBucketCounts = [...]uint32{
	7, 17, 29, 53, 97, 193, 389, 769, 1543, 3079, 6151, 12289,
	24593, 49157, 98317, 196613, 393241, 786433, 1572869, 3145739,
	6291469, 12582917, 25165843, 50331653, 100663319, 201326611,
	402653189, 805306457, 1610612741, 3221225473, 4294967291,
}

type NativeJSONMember[T any] struct {
	key   string
	value T
	next  int
}

type NativeJSONMap[T any] struct {
	buckets []int
	members []NativeJSONMember[T]
}

func (m *NativeJSONMap[T]) UnmarshalJSON(data []byte) error {
	return m.unmarshalJSON(data)
}

func (m *NativeJSONMap[T]) unmarshalJSON(data []byte) error {
	err := try(func() {
		*m = NativeJSONMap[T]{}

		decoder := json.NewDecoder(bytes.NewReader(data))
		opening := throw2(decoder.Token())

		if opening == nil {
			return
		}

		if opening != json.Delim('{') {
			throwFmt("resource JSON mapping must be an object")
		}

		for decoder.More() {
			key := throw2(decoder.Token()).(string)

			var value T

			throw(decoder.Decode(&value))
			m.put(key, value)
		}

		throw2(decoder.Token())
	})

	if err != nil {
		return err.asError()
	}

	return nil
}

func (m *NativeJSONMap[T]) put(key string, value T) {
	hash := cityHash64(strBytes(key))

	if len(m.buckets) > 0 {
		bucket := hash % uint64(len(m.buckets))

		for index := m.buckets[bucket]; index != 0; index = m.members[index-1].next {
			member := &m.members[index-1]

			if member.key == key {
				member.value = value

				return
			}
		}
	}

	if len(m.members) >= len(m.buckets) {
		m.grow()
	}

	bucket := hash % uint64(len(m.buckets))

	m.members = append(m.members, NativeJSONMember[T]{key: key, value: value, next: m.buckets[bucket]})
	m.buckets[bucket] = len(m.members)
}

func (m *NativeJSONMap[T]) grow() {
	var buckets []int

	for _, count := range nativeJSONBucketCounts {
		if uint64(count) > uint64(len(m.buckets)) {
			buckets = make([]int, count)

			break
		}
	}

	assert(len(buckets) > 0, "resource JSON mapping is too large")

	for _, head := range m.buckets {
		for index := head; index != 0; {
			member := &m.members[index-1]
			next := member.next
			bucket := cityHash64(strBytes(member.key)) % uint64(len(buckets))

			member.next = buckets[bucket]
			buckets[bucket] = index
			index = next
		}
	}

	m.buckets = buckets
}

func (m *NativeJSONMap[T]) each(visit func(string, T)) {
	for _, head := range m.buckets {
		for index := head; index != 0; index = m.members[index-1].next {
			member := &m.members[index-1]

			visit(member.key, member.value)
		}
	}
}
