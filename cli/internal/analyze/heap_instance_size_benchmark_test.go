package analyze

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkHprofInstanceSizeResolution(b *testing.B) {
	for _, order := range []string{"classes-first", "instances-first"} {
		for _, payload := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/payload=%t", order, payload), func(b *testing.B) {
				data := instanceSizeHprof(order, payload, false, 20000, 64)
				path := filepath.Join(b.TempDir(), "sizes.hprof")
				if err := os.WriteFile(path, data, 0600); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					p := newHprofParser(path, nil)
					if err := p.parse(); err != nil {
						b.Fatal(err)
					}
					if len(p.nodes) != 20001 {
						b.Fatalf("nodes=%d", len(p.nodes))
					}
				}
			})
		}
	}
}

// Each object has a direct GC root and optionally a null reference field. Class sizes
// include inherited storage; the parser must not add superclass size a second time.
func instanceSizeHprof(order string, payload, inherited bool, count int, size uint32) []byte {
	b := newMiniHprof()
	b.loadClass(0x301, b.string("com/app/LeakedActivity"))
	b.loadClass(0x302, b.string("com/app/Base"))
	field := b.string("reference")
	var fields []miniField
	var values []uint32
	if payload {
		fields = []miniField{{nameID: field, typ: hprofTypeObject}}
		values = []uint32{0}
	}
	var heap bytes.Buffer
	child := func() {
		if inherited {
			b.classDumpWithSuper(&heap, 0x301, 0x302, size, nil, nil)
		} else {
			b.classDump(&heap, 0x301, size, nil, fields)
		}
	}
	base := func() {
		if inherited {
			b.classDump(&heap, 0x302, 24, nil, fields)
		}
	}
	if order == "classes-first" {
		base()
		child()
	}
	if order == "superclass-last" {
		child()
	}
	for i := 0; i < count; i++ {
		id := uint32(0x1000 + i)
		heap.WriteByte(0x05)
		writeU4(&heap, id)
		b.instanceDump(&heap, id, 0x301, values)
	}
	if order == "instances-first" {
		child()
		base()
	}
	if order == "missing-superclass" {
		child()
	}
	if order == "superclass-last" {
		base()
	}
	b.record(hprofTagHeapDump, heap.Bytes())
	return b.bytes()
}
