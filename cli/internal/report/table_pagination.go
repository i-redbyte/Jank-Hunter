package report

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

const reportTablePageSize = 50

var deferredScriptClosingTag = []byte("</script>")

// paginateReportTables transforms every table while the report template is streaming. Short tables
// are copied byte-for-byte. Once a row group crosses reportTablePageSize, only its first page stays
// in the live HTML tree; later pages become script-safe JSON payloads consumed by report.js.
// Memory is bounded by the first page, one deferred page and one current row, irrespective of the
// complete table size.
func paginateReportTables(source io.Reader, target io.Writer) error {
	reader := bufio.NewReaderSize(source, 64*1024)
	var table *reportTablePager
	rawElement := ""

	write := func(data string) error {
		if table != nil {
			return table.writeRaw(data)
		}
		return writeAll(target, []byte(data))
	}
	writeBytes := func(data []byte) error {
		if table != nil {
			return table.writeRawBytes(data)
		}
		return writeAll(target, data)
	}

	for {
		if rawElement != "" {
			var err error
			if rawElement == "deferred-script" {
				err = copyDeferredTableChunk(reader, writeBytes)
			} else {
				err = copyRawHTMLElement(reader, rawElement, write)
			}
			if err != nil {
				return err
			}
			rawElement = ""
			continue
		}
		text, err := reader.ReadString('<')
		if len(text) == 0 && err == io.EOF {
			break
		}
		if len(text) > 0 {
			hasTag := text[len(text)-1] == '<'
			if hasTag {
				text = text[:len(text)-1]
			}
			if err := write(text); err != nil {
				return err
			}
			if !hasTag {
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				continue
			}
		}
		if err != nil && err != io.EOF {
			return err
		}

		tail, tagErr := reader.ReadString('>')
		tag := "<" + tail
		if tail == "" {
			if err := write("<"); err != nil {
				return err
			}
			break
		}
		completeTag := tail[len(tail)-1] == '>'
		if !completeTag {
			if err := write(tag); err != nil {
				return err
			}
			if tagErr == io.EOF {
				break
			}
			return tagErr
		}

		if table == nil && isOpeningHTMLTag(tag, "table") {
			if err := writeAll(target, []byte(tag)); err != nil {
				return err
			}
			table = newReportTablePager(target)
			continue
		}
		if table != nil {
			closed, tableErr := table.writeTag(tag)
			if tableErr != nil {
				return tableErr
			}
			if closed {
				table = nil
			}
		}
		if isOpeningHTMLTag(tag, "script") {
			if containsFold(tag, "data-table-chunk") {
				rawElement = "deferred-script"
			} else {
				rawElement = "script"
			}
		} else if isOpeningHTMLTag(tag, "style") {
			rawElement = "style"
		}
		if table == nil && !isClosingHTMLTag(tag, "table") {
			if err := writeAll(target, []byte(tag)); err != nil {
				return err
			}
		}
	}
	if table != nil {
		return fmt.Errorf("paginate report tables: unterminated table")
	}
	return nil
}

// copyDeferredTableChunk uses the invariant established by renderDeferredRowPages: every closing
// tag inside the JSON string has an escaped slash, so the first literal </script> is the container
// boundary. Searching whole buffered blocks avoids allocating one string per HTML tag embedded in
// a large deferred page.
func copyDeferredTableChunk(reader *bufio.Reader, write func([]byte) error) error {
	for {
		buffer, peekErr := reader.Peek(reader.Size())
		if offset := bytes.Index(buffer, deferredScriptClosingTag); offset >= 0 {
			length := offset + len(deferredScriptClosingTag)
			if err := write(buffer[:length]); err != nil {
				return err
			}
			_, _ = reader.Discard(length)
			return nil
		}

		length := len(buffer) - len(deferredScriptClosingTag) + 1
		if length > 0 {
			if err := write(buffer[:length]); err != nil {
				return err
			}
			_, _ = reader.Discard(length)
		}
		if peekErr != nil && peekErr != bufio.ErrBufferFull {
			if peekErr == io.EOF {
				return fmt.Errorf("paginate report tables: unterminated deferred script element")
			}
			return peekErr
		}
	}
}

// copyRawHTMLElement treats script and style contents as raw text, as required by HTML parsing
// rules. In particular, JavaScript comparison operators such as "index < length" must not consume
// the real closing tag and accidentally disable pagination for every following table.
func copyRawHTMLElement(reader *bufio.Reader, name string, write func(string) error) error {
	closingPrefix := []byte("/" + name)
	for {
		text, err := reader.ReadString('<')
		hasLess := len(text) > 0 && text[len(text)-1] == '<'
		if hasLess {
			text = text[:len(text)-1]
		}
		if text != "" {
			if writeErr := write(text); writeErr != nil {
				return writeErr
			}
		}
		if !hasLess {
			if err == io.EOF {
				return fmt.Errorf("paginate report tables: unterminated %s element", name)
			}
			return err
		}

		prefix, peekErr := reader.Peek(len(closingPrefix) + 1)
		isClosing := len(prefix) == len(closingPrefix)+1 &&
			bytes.EqualFold(prefix[:len(closingPrefix)], closingPrefix) &&
			(prefix[len(closingPrefix)] == '>' || prefix[len(closingPrefix)] == ' ' ||
				prefix[len(closingPrefix)] == '\t' || prefix[len(closingPrefix)] == '\r' ||
				prefix[len(closingPrefix)] == '\n')
		if isClosing {
			tail, tailErr := reader.ReadString('>')
			if err := write("<" + tail); err != nil {
				return err
			}
			if len(tail) == 0 || tail[len(tail)-1] != '>' {
				if tailErr != nil {
					return fmt.Errorf("paginate report tables: unterminated %s closing tag: %w", name, tailErr)
				}
				return fmt.Errorf("paginate report tables: unterminated %s closing tag", name)
			}
			return nil
		}
		if peekErr != nil && peekErr != bufio.ErrBufferFull && peekErr != io.EOF {
			return peekErr
		}
		if err := write("<"); err != nil {
			return err
		}
	}
}

func isOpeningHTMLTag(tag, name string) bool {
	tag = strings.TrimSpace(tag)
	nameEnd := 1 + len(name)
	return len(tag) >= nameEnd && tag[0] == '<' && strings.EqualFold(tag[1:nameEnd], name) &&
		(len(tag) == nameEnd || isHTMLTagBoundary(tag[nameEnd], true))
}

func isClosingHTMLTag(tag, name string) bool {
	tag = strings.TrimSpace(tag)
	nameEnd := 2 + len(name)
	return len(tag) >= nameEnd && strings.HasPrefix(tag, "</") && strings.EqualFold(tag[2:nameEnd], name) &&
		(len(tag) == nameEnd || isHTMLTagBoundary(tag[nameEnd], false))
}

func isHTMLTagBoundary(char byte, allowSlash bool) bool {
	return char == '>' || allowSlash && char == '/' || char == ' ' || char == '\t' || char == '\r' || char == '\n'
}

func containsFold(value, fragment string) bool {
	if fragment == "" {
		return true
	}
	for offset := 0; offset+len(fragment) <= len(value); offset++ {
		if strings.EqualFold(value[offset:offset+len(fragment)], fragment) {
			return true
		}
	}
	return false
}

func countOpeningHTMLTags(value, name string) int {
	count := 0
	for {
		start := strings.IndexByte(value, '<')
		if start < 0 {
			return count
		}
		value = value[start:]
		if isOpeningHTMLTag(value, name) {
			count++
		}
		value = value[1:]
	}
}

type reportTablePager struct {
	target     io.Writer
	depth      int
	inHead     int
	row        bytes.Buffer
	inRow      bool
	columns    int
	group      *reportTableRowGroup
	groupIndex int
}

func newReportTablePager(target io.Writer) *reportTablePager {
	return &reportTablePager{target: target, depth: 1}
}

func (p *reportTablePager) writeRaw(data string) error {
	if p.inRow {
		_, _ = p.row.WriteString(data)
		return nil
	}
	if p.group != nil {
		return p.group.writeRaw(data)
	}
	return writeAll(p.target, []byte(data))
}

func (p *reportTablePager) writeRawBytes(data []byte) error {
	if p.inRow {
		_, _ = p.row.Write(data)
		return nil
	}
	if p.group != nil {
		return p.group.writeRawBytes(data)
	}
	return writeAll(p.target, data)
}

func (p *reportTablePager) writeTag(tag string) (bool, error) {
	if p.inRow {
		_, _ = p.row.WriteString(tag)
		switch {
		case isOpeningHTMLTag(tag, "table"):
			p.depth++
		case isClosingHTMLTag(tag, "table"):
			p.depth--
		case p.depth == 1 && isClosingHTMLTag(tag, "tr"):
			p.inRow = false
			return false, p.finishRow()
		}
		return false, nil
	}

	if p.depth == 1 && isOpeningHTMLTag(tag, "script") && containsFold(tag, "data-table-chunk") {
		if p.group != nil {
			if err := p.group.startPassThrough(); err != nil {
				return false, err
			}
		}
	}

	switch {
	case isOpeningHTMLTag(tag, "table"):
		p.depth++
		return false, writeAll(p.target, []byte(tag))
	case isClosingHTMLTag(tag, "table"):
		if p.depth > 1 {
			p.depth--
			return false, writeAll(p.target, []byte(tag))
		}
		if p.group != nil {
			if err := p.finishGroup(); err != nil {
				return false, err
			}
		}
		if err := writeAll(p.target, []byte(tag)); err != nil {
			return false, err
		}
		return true, nil
	case p.depth == 1 && isOpeningHTMLTag(tag, "thead"):
		p.inHead++
		return false, writeAll(p.target, []byte(tag))
	case p.depth == 1 && isClosingHTMLTag(tag, "thead"):
		if p.inHead > 0 {
			p.inHead--
		}
		return false, writeAll(p.target, []byte(tag))
	case p.depth == 1 && isOpeningHTMLTag(tag, "tbody"):
		if p.group != nil {
			return false, fmt.Errorf("paginate report tables: nested tbody")
		}
		if err := writeAll(p.target, []byte(tag)); err != nil {
			return false, err
		}
		p.group = newReportTableRowGroup(p.target, true, p.groupIndex)
		p.groupIndex++
		return false, nil
	case p.depth == 1 && isClosingHTMLTag(tag, "tbody"):
		if p.group != nil {
			if err := p.finishGroup(); err != nil {
				return false, err
			}
		}
		return false, writeAll(p.target, []byte(tag))
	case p.depth == 1 && isOpeningHTMLTag(tag, "tfoot"):
		if p.group != nil {
			if err := p.finishGroup(); err != nil {
				return false, err
			}
		}
		return false, writeAll(p.target, []byte(tag))
	case p.depth == 1 && isOpeningHTMLTag(tag, "tr"):
		p.inRow = true
		p.row.Reset()
		_, _ = p.row.WriteString(tag)
		return false, nil
	default:
		return false, p.writeRaw(tag)
	}
}

func (p *reportTablePager) finishRow() error {
	row := p.row.String()
	tdCount := countOpeningHTMLTags(row, "td")
	thCount := countOpeningHTMLTags(row, "th")
	cellCount := tdCount + thCount
	if cellCount > p.columns {
		p.columns = cellCount
	}
	header := p.inHead > 0 || (thCount > 0 && tdCount == 0)
	if header && p.group == nil {
		return writeAll(p.target, []byte(row))
	}
	if p.group == nil {
		p.group = newReportTableRowGroup(p.target, false, p.groupIndex)
		p.groupIndex++
	}
	return p.group.writeRow(row, !header, p.columns)
}

func (p *reportTablePager) finishGroup() error {
	err := p.group.finish()
	p.group = nil
	return err
}

type reportTableRowGroup struct {
	target       io.Writer
	explicitBody bool
	index        int
	prefix       bytes.Buffer
	chunk        bytes.Buffer
	dataRows     int
	chunkRows    int
	columns      int
	active       bool
	passThrough  bool
}

func newReportTableRowGroup(target io.Writer, explicitBody bool, index int) *reportTableRowGroup {
	return &reportTableRowGroup{target: target, explicitBody: explicitBody, index: index}
}

func (g *reportTableRowGroup) writeRaw(data string) error {
	switch {
	case g.passThrough:
		return writeAll(g.target, []byte(data))
	case g.active:
		_, _ = g.chunk.WriteString(data)
	default:
		_, _ = g.prefix.WriteString(data)
	}
	return nil
}

func (g *reportTableRowGroup) writeRawBytes(data []byte) error {
	switch {
	case g.passThrough:
		return writeAll(g.target, data)
	case g.active:
		_, _ = g.chunk.Write(data)
	default:
		_, _ = g.prefix.Write(data)
	}
	return nil
}

func (g *reportTableRowGroup) writeRow(row string, data bool, columns int) error {
	if columns > g.columns {
		g.columns = columns
	}
	if g.passThrough {
		return writeAll(g.target, []byte(row))
	}
	if !data {
		return g.writeRaw(row)
	}
	g.dataRows++
	if !g.active && g.dataRows <= reportTablePageSize {
		_, _ = g.prefix.WriteString(row)
		return nil
	}
	if !g.active {
		g.active = true
		if !g.explicitBody {
			if err := writeAll(g.target, []byte(`<tbody data-deferred-table>`)); err != nil {
				return err
			}
		}
		if err := writeAll(g.target, g.prefix.Bytes()); err != nil {
			return err
		}
		g.prefix.Reset()
	}
	_, _ = g.chunk.WriteString(row)
	g.chunkRows++
	if g.chunkRows == reportTablePageSize {
		return g.flushChunk()
	}
	return nil
}

func (g *reportTableRowGroup) startPassThrough() error {
	if g.active {
		return fmt.Errorf("paginate report tables: nested deferred payload after generic pagination started")
	}
	if err := writeAll(g.target, g.prefix.Bytes()); err != nil {
		return err
	}
	g.prefix.Reset()
	g.passThrough = true
	return nil
}

func (g *reportTableRowGroup) flushChunk() error {
	if g.chunkRows == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(
		g.target,
		`<script type="application/json" data-table-chunk data-table-chunk-size="%d">`,
		g.chunkRows,
	); err != nil {
		return err
	}
	if err := writeScriptSafeJSONString(g.target, g.chunk.String()); err != nil {
		return err
	}
	if err := writeAll(g.target, []byte(`</script>`)); err != nil {
		return err
	}
	g.chunk.Reset()
	g.chunkRows = 0
	return nil
}

func (g *reportTableRowGroup) finish() error {
	if g.passThrough {
		return nil
	}
	if !g.active {
		return writeAll(g.target, g.prefix.Bytes())
	}
	if err := g.flushChunk(); err != nil {
		return err
	}
	// A row-aligned flush can leave only formatting whitespace in the chunk buffer. Keep it in
	// the document instead of silently changing the template output around the loader row.
	if g.chunk.Len() > 0 {
		if err := writeAll(g.target, g.chunk.Bytes()); err != nil {
			return err
		}
		g.chunk.Reset()
	}
	columns := max(1, g.columns)
	remaining := g.dataRows - reportTablePageSize
	next := min(reportTablePageSize, remaining)
	if _, err := fmt.Fprintf(
		g.target,
		`<tr class="deferred-table-loader" data-deferred-loader data-deferred-total="%d" data-deferred-loaded="%d"><td colspan="%d"><button type="button" class="deferred-table-button" data-load-more-rows>Показать ещё %d</button><span data-deferred-remaining>Осталось строк: %d</span><span class="sr-only"> в таблице %d</span></td></tr>`,
		g.dataRows,
		reportTablePageSize,
		columns,
		next,
		remaining,
		g.index+1,
	); err != nil {
		return err
	}
	if !g.explicitBody {
		return writeAll(g.target, []byte(`</tbody>`))
	}
	return nil
}
