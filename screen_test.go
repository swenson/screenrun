package main

import (
	"testing"
	"unsafe"
)

func TestMagic(t *testing.T) {
	magic := 1836279557
	if magic != msgRevision {
		t.Errorf("Expected msgRevision to be %d, got %d", magic, msgRevision)
	}
}

func TestStructSizes(t *testing.T) {
	var size int64
	{
		name := "screenMessage"
		size = 3372
		value := screenMessage{}
		if size != int64(unsafe.Sizeof(value)) {
			t.Errorf("%s should be %d bytes in size, got %d", name, size, unsafe.Sizeof(value))
		}
	}
	{
		name := "screenMessageAttach"
		size = 348
		value := screenMessageAttach{}
		if size != int64(unsafe.Sizeof(value)) {
			t.Errorf("%s should be %d bytes in size, got %d", name, size, unsafe.Sizeof(value))
		}
	}
}
