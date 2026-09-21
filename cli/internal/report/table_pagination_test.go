package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPaginateReportTablesPreservesShortDocumentsExactly(t *testing.T) {
	input := `<!doctype html><html><head><style>.x{content:"<table>"}</style></head><body>` +
		`<table class="short"><tr><th>Имя</th></tr><tr><td>одна строка</td></tr></table>` +
		`<script>const example = "<table><tr><td>не HTML</td></tr></table>";</script></body></html>`
	var output bytes.Buffer
	if err := paginateReportTables(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != input {
		t.Fatalf("short report changed:\n got %q\nwant %q", got, input)
	}
}

func TestPaginateReportTablesIgnoresComparisonOperatorsInRawElements(t *testing.T) {
	var input strings.Builder
	input.WriteString(`<script>if (index < nodes.length) run();</script><style>.x::after{content:"a < b"}</style>`)
	input.WriteString(`<table><tr><th>row</th></tr>`)
	for index := range 51 {
		fmt.Fprintf(&input, `<tr><td>%d</td></tr>`, index)
	}
	input.WriteString(`</table>`)

	var output bytes.Buffer
	if err := paginateReportTables(strings.NewReader(input.String()), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `data-deferred-total="51"`) {
		t.Fatal("a JavaScript or CSS comparison operator disabled pagination of a following table")
	}
}

func TestPaginateReportTablesTreatsTagNamesCaseInsensitively(t *testing.T) {
	var input strings.Builder
	input.WriteString(`<TABLE><THEAD><TR><TH>row</TH></TR></THEAD><TBODY>`)
	for index := range 51 {
		fmt.Fprintf(&input, `<TR><TD>%d</TD></TR>`, index)
	}
	input.WriteString(`</TBODY></TABLE>`)

	var output bytes.Buffer
	if err := paginateReportTables(strings.NewReader(input.String()), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if !strings.Contains(html, `data-deferred-total="51"`) {
		t.Fatal("mixed-case HTML table was not paginated")
	}
	if !strings.Contains(html, `<THEAD><TR><TH>row</TH></TR></THEAD>`) {
		t.Fatal("mixed-case table header was not preserved")
	}
}

func TestPaginateReportTablesDefersEveryLargeRowGroup(t *testing.T) {
	var input strings.Builder
	input.WriteString(`<table class="large"><tr><th>Имя</th><th>Значение</th></tr>`)
	for index := range 120 {
		fmt.Fprintf(&input, `<tr><td>row-%03d</td><td>%d</td></tr>`, index, index)
	}
	input.WriteString(`</table>`)

	var output bytes.Buffer
	if err := paginateReportTables(strings.NewReader(input.String()), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	firstPayload := strings.Index(html, `<script type="application/json" data-table-chunk`)
	if firstPayload < 0 {
		t.Fatal("deferred payload is missing")
	}
	if got := strings.Count(html[:firstPayload], "<tr"); got != 51 {
		t.Fatalf("live rows before first payload = %d, want one header + 50 data rows", got)
	}
	for marker, want := range map[string]int{
		`data-table-chunk-size="50"`: 1,
		`data-table-chunk-size="20"`: 1,
		`data-deferred-total="120"`:  1,
		`data-deferred-loaded="50"`:  1,
		`Показать ещё 50`:            1,
		`Осталось строк: 70`:         1,
	} {
		if got := strings.Count(html, marker); got != want {
			t.Fatalf("%q count = %d, want %d", marker, got, want)
		}
	}
	if !strings.Contains(html, `<tbody data-deferred-table>`) || !strings.Contains(html, `</tbody></table>`) {
		t.Fatal("large table without tbody was not normalized to a deferred tbody")
	}
	if strings.Contains(html, `</script></script>`) || strings.Contains(html, `</tr>`) && strings.Contains(html, `row-119</td></tr></script>`) {
		t.Fatal("deferred payload contains a raw closing tag capable of terminating script text")
	}
	if !strings.Contains(decodeFirstTableChunk(t, html), "row-050") {
		t.Fatal("first deferred row disappeared")
	}
}

func TestPaginateReportTablesDoesNotPaginateAnExistingDeferredTableTwice(t *testing.T) {
	input := `<table><tbody><tr><td>live</td></tr>` +
		`<script type="application/json" data-table-chunk data-table-chunk-size="1">"<tr><td>later<\/td><\/tr>"</script>` +
		`<tr data-deferred-loader data-deferred-total="2" data-deferred-loaded="1"><td>load</td></tr>` +
		`</tbody></table>`
	var output bytes.Buffer
	if err := paginateReportTables(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != input {
		t.Fatalf("existing deferred table changed:\n got %q\nwant %q", got, input)
	}
}

func decodeFirstTableChunk(t *testing.T, document string) string {
	t.Helper()
	start := strings.Index(document, `data-table-chunk-size="`)
	if start < 0 {
		t.Fatal("table chunk is missing")
	}
	start = strings.Index(document[start:], ">") + start + 1
	end := strings.Index(document[start:], `</script>`)
	if end < 0 {
		t.Fatal("table chunk terminator is missing")
	}
	var decoded string
	if err := json.Unmarshal([]byte(document[start:start+end]), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
