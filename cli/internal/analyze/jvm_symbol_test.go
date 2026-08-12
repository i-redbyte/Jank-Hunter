package analyze

import "testing"

func TestFormatJVMMethodSymbol(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		pretty string
	}{
		{
			name:   "thread sleep",
			raw:    "Ljava/lang/Thread;->sleep(Ljava/lang/Object;JI)V",
			pretty: "java.lang.Thread.sleep(java.lang.Object, long, int): void",
		},
		{
			name:   "objects and arrays",
			raw:    "Lsample/Worker;->map([I[[Ljava/lang/String;)[Ljava/lang/Object;",
			pretty: "sample.Worker.map(int[], java.lang.String[][]): java.lang.Object[]",
		},
		{
			name:   "constructor",
			raw:    "Lsample/Worker;-><init>(Ljava/lang/String;)V",
			pretty: "sample.Worker(java.lang.String)",
		},
		{
			name:   "class initializer",
			raw:    "Lsample/Worker;-><clinit>()V",
			pretty: "sample.Worker.<инициализация класса>()",
		},
		{
			name:   "already readable",
			raw:    "sample.Worker.run()",
			pretty: "sample.Worker.run()",
		},
		{
			name:   "malformed descriptor",
			raw:    "Lsample/Worker;->run([)V",
			pretty: "Lsample/Worker;->run([)V",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatJVMMethodSymbol(test.raw); got != test.pretty {
				t.Fatalf("formatJVMMethodSymbol(%q) = %q, want %q", test.raw, got, test.pretty)
			}
		})
	}
}
