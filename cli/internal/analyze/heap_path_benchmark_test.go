package analyze

import "testing"

func BenchmarkHprofRootProvenance(b *testing.B) {
	for _, mode := range []string{"bfs", "evidence"} {
		b.Run(mode, func(b *testing.B) {
			p := heapPathChain(200000, "JNI global")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "bfs" {
					if p.rootBFS().reachable != 200000 {
						b.Fatal("incomplete BFS")
					}
				} else {
					e := p.evidence()
					if len(e.Leaks) != 1 || e.Leaks[0].RetainedSizeBytes != 64 {
						b.Fatal("incorrect evidence")
					}
				}
			}
		})
	}
}
func heapPathChain(nodes int, root string) *hprofParser {
	p := newHprofParser("chain.hprof", map[string]struct{}{"app.Target": {}})
	for i := 1; i <= nodes; i++ {
		class := "app.Owner"
		if i == nodes {
			class = "app.Target"
		}
		p.ensureNode(uint64(i), class, 64)
		if i > 1 {
			p.addEdge(p.nodeByID(uint64(i-1)), uint64(i), "next", "field")
		}
	}
	p.roots = []heapRoot{{id: 1, kind: root}}
	return p
}
