package harness

import (
	"context"
	"testing"
)

func TestRegistryRejectsInvalidAndDuplicateRegistration(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Descriptor{ID: "missing"}); err == nil {
		t.Fatal("invalid descriptor accepted")
	}
	d := Descriptor{ID: "test", Name: "Test", Factory: func(context.Context, Options, Sink) (Adapter, error) { return nil, nil }}
	if err := r.Register(d); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(d); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := r.Lookup("unknown"); err == nil {
		t.Fatal("unknown lookup succeeded")
	}
}
