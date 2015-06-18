package toxics

import (
	"reflect"
	"sync"

	"github.com/Shopify/toxiproxy/stream"
)

// A Toxic is something that can be attatched to a link to modify the way
// data can be passed through (for example, by adding latency)
//
//              Toxic
//                v
// Client <-> ToxicStub <-> Upstream
//
// Toxic's work in a pipeline fashion, and can be chained together
// with channels. The toxic itself only defines the settings and
// Pipe() function definition, and uses the ToxicStub struct to store
// per-connection information. This allows the same toxic to be used
// for multiple connections.

type Toxic interface {
	// Defines how packets flow through a ToxicStub. Pipe() blocks until the link is closed or interrupted.
	Pipe(*stream.ToxicStub)
}

type ToxicWrapper struct {
	Toxic `json:"-"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Index int    `json:"-"`
}

var ToxicRegistry map[string]Toxic
var registryMutex sync.RWMutex

func Register(typeName string, toxic Toxic) {
	registryMutex.Lock()
	defer registryMutex.Unlock()

	if ToxicRegistry == nil {
		ToxicRegistry = make(map[string]Toxic)
	}
	ToxicRegistry[typeName] = toxic
}

func New(typeName string) Toxic {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	orig, ok := ToxicRegistry[typeName]
	if !ok {
		return nil
	}
	return reflect.New(reflect.TypeOf(orig).Elem()).Interface().(Toxic)
}

func Count() int {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	return len(ToxicRegistry)
}
