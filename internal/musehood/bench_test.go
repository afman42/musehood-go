package musehood

import (
	"context"
	"strings"
	"testing"
)

func BenchmarkParseToolBlock(b *testing.B) {
	text := "Baik, saya cari dulu.\n<<TOOL:web_search>>\nharga emas hari ini\n<</TOOL>>"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseToolBlock(text)
	}
}

func BenchmarkDangerousCode(b *testing.B) {
	code := "import math\nfor i in range(100):\n    print(math.sqrt(i))"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dangerousCode(code)
	}
}

func BenchmarkSandboxWriteRead(b *testing.B) {
	app := &App{dir: b.TempDir()}
	content := strings.Repeat("baris isi file\n", 100)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		app.toolWriteFile("bench.txt\n" + content)
		app.toolReadFile("bench.txt")
	}
}

func BenchmarkHTMLToText(b *testing.B) {
	raw := "<html><head><style>.x{color:red}</style><script>var a=1</script></head><body>" +
		strings.Repeat("<p>Paragraf <b>penting</b> dengan &amp; entitas.</p>", 50) + "</body></html>"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		htmlToText(raw)
	}
}

// BenchmarkExecuteCodePython mengukur overhead penuh sandbox
// (systemd-run --scope + bwrap + startup python3) untuk kode trivial.
func BenchmarkExecuteCodePython(b *testing.B) {
	app := &App{dir: b.TempDir()}
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out := app.executeCode(ctx, "python3", "py", "print(1)")
		if out != "1" {
			b.Fatalf("output = %q", out)
		}
	}
}

func BenchmarkExecuteCodeNode(b *testing.B) {
	app := &App{dir: b.TempDir()}
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out := app.executeCode(ctx, "node", "js", "console.log(1)")
		if out != "1" {
			b.Fatalf("output = %q", out)
		}
	}
}
