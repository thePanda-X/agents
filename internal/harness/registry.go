package harness

import (
	"fmt"
	"slices"
)

type Descriptor struct {
	ID, Name, Description string
	Launch                Launch
	LoginHint             string
	Factory               Factory
}

// Registry is assembled at startup and then treated as immutable.
type Registry struct {
	descriptors map[string]Descriptor
}

func NewRegistry() *Registry {
	return &Registry{descriptors: make(map[string]Descriptor)}
}

func (r *Registry) Register(d Descriptor) error {
	if d.ID == "" || d.Name == "" || d.Factory == nil {
		return fmt.Errorf("harness registration requires an ID, name, and factory")
	}
	if _, exists := r.descriptors[d.ID]; exists {
		return fmt.Errorf("harness %q is already registered", d.ID)
	}
	d.Launch.Args = slices.Clone(d.Launch.Args)
	r.descriptors[d.ID] = d
	return nil
}

func (r *Registry) Lookup(id string) (Descriptor, error) {
	d, exists := r.descriptors[id]
	if !exists {
		return Descriptor{}, fmt.Errorf("unknown harness %q", id)
	}
	return d, nil
}

func (r *Registry) List() []Descriptor {
	result := make([]Descriptor, 0, len(r.descriptors))
	for _, d := range r.descriptors {
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b Descriptor) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return result
}
