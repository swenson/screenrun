package main

import (
	"testing"
	"unsafe"
)

func TestMagic(t *testing.T) {
	magic := 1836279556
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
		name := "screenMessageCreate"
		size = 2104
		value := screenMessageCreate{}
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
	{
		name := "screenMessageDetach"
		size = 264
		value := screenMessageDetach{}
		if size != int64(unsafe.Sizeof(value)) {
			t.Errorf("%s should be %d bytes in size, got %d", name, size, unsafe.Sizeof(value))
		}
	}
	{
		name := "screenMessageCommand"
		size = 2340
		value := screenMessageCommand{}
		if size != int64(unsafe.Sizeof(value)) {
			t.Errorf("%s should be %d bytes in size, got %d", name, size, unsafe.Sizeof(value))
		}
	}
	{
		name := "screenMessageMessage"
		size = 2048
		value := screenMessageMessage{}
		if size != int64(unsafe.Sizeof(value)) {
			t.Errorf("%s should be %d bytes in size, got %d", name, size, unsafe.Sizeof(value))
		}
	}
}
