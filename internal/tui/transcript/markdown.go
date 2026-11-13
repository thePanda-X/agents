package transcript

import (
	"bytes"
	"slices"
	"strings"

	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	terminalansi "github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	asttable "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type markdownRenderer struct {
	markdown goldmark.Markdown
	tables   *tableLayout
	// cellGap is the blank space kept after a cell's widest content: Glamour's
	// one-column cell margin plus the configured horizontal padding.
	cellGap int
}

func newMarkdownRenderer(width int) *markdownRenderer {
	style := styles.DarkStyleConfig
	style.Document.Margin = new(uint(0))
	style.Document.Color = new("#E8E6EA")
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	style.Heading.Color = new("#F2B8CF")
	style.H1.Color, style.H1.BackgroundColor = new("#F2B8CF"), nil
	style.H1.Prefix, style.H1.Suffix = "", ""
	style.H2.Prefix, style.H3.Prefix, style.H4.Prefix = "", "", ""
	style.Link.Color = new("#F2B8CF")
	style.Code.Color, style.Code.BackgroundColor = new("#F2B8CF"), new("#27272D")
	// The colored chip marks inline code. Padding spaces around it read as
	// stray double spaces ("in  parse() .") once colors are removed.
	style.Code.Prefix, style.Code.Suffix = "", ""
	style.CodeBlock.Theme = "dracula"
	padding := uint(1)
	if width >= 60 {
		padding = 2
	}
	style.Table.Margin = &padding

	// Use Glamour's renderer with the same Goldmark extensions as TermRenderer.
	// A presentation-only AST pass adds table boundaries without parsing Markdown
	// with regexes or changing the stored transcript and clipboard text.
	tables := &tableLayout{}
	markdown := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
		goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithASTTransformers(util.Prioritized(tables, 100))),
	)
	// Install after extensions so their HTML renderers cannot replace Glamour's
	// handlers for table and definition-list nodes.
	markdown.SetRenderer(renderer.NewRenderer(renderer.WithNodeRenderers(
		util.Prioritized(ansi.NewRenderer(ansi.Options{Styles: style, WordWrap: width}), 1000),
	)))
	return &markdownRenderer{markdown: markdown, tables: tables, cellGap: 1 + int(padding)}
}

func (r *markdownRenderer) Render(source string) (string, error) {
	var buffer bytes.Buffer
	data := []byte(source)
	document := r.markdown.Parser().Parse(text.NewReader(data))
	// Injected Text nodes reference an appended marker, never parsed as source
	// Markdown. Glamour supports Text segments but not synthetic String nodes.
	data = append(data, r.tables.marker...)
	err := r.markdown.Renderer().Render(&buffer, data, document)
	return compactTables(r.tables.rules(buffer.String()), r.cellGap), err
}

// compactTables shrinks Glamour tables to their content. Glamour lays every
// table out at the full wrap width, so a two-column table of short values
// would otherwise span the whole terminal. Column boundaries come from the
// header rule's "┼" junctions; rows are cut ANSI-safely at those columns, so
// styling, wrapping, and wide characters are preserved. Tables that need the
// full width are left unchanged.
func compactTables(rendered string, gap int) string {
	lines := strings.Split(rendered, "\n")
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = terminalansi.Strip(line)
	}
	for i := 0; i < len(lines); i++ {
		junctions := ruleJunctions(plain[i])
		if junctions == nil {
			continue
		}
		member := func(index int) bool {
			if j := ruleJunctions(plain[index]); j != nil {
				return equalColumns(j, junctions)
			}
			return separatorsAt(plain[index], '│', junctions)
		}
		start, end := i, i
		for start > 0 && member(start-1) {
			start--
		}
		for end+1 < len(lines) && member(end+1) {
			end++
		}
		bounds := make([][2]int, len(junctions)+1)
		previous := 0
		for k, junction := range junctions {
			bounds[k] = [2]int{previous, junction}
			previous = junction + 1
		}
		last := 0
		for index := start; index <= end; index++ {
			last = max(last, terminalansi.StringWidth(lines[index]))
		}
		bounds[len(junctions)] = [2]int{previous, last}
		// For each column keep [offset, offset+width): drop surplus leading
		// space (centered/right-aligned cells) beyond the standard gap, and
		// trailing space beyond the gap. The first column keeps its leading
		// indentation, which belongs to enclosing lists or quotes.
		offsets, widths := make([]int, len(bounds)), make([]int, len(bounds))
		for k, bound := range bounds {
			right, lead, content := 0, bound[1]-bound[0], false
			for index := start; index <= end; index++ {
				if ruleJunctions(plain[index]) != nil {
					continue
				}
				segment := terminalansi.Strip(terminalansi.Cut(lines[index], bound[0], bound[1]))
				trimmed := strings.TrimRight(segment, " ")
				if strings.TrimSpace(trimmed) == "" {
					continue
				}
				content = true
				right = max(right, terminalansi.StringWidth(trimmed))
				lead = min(lead, terminalansi.StringWidth(trimmed)-terminalansi.StringWidth(strings.TrimLeft(trimmed, " ")))
			}
			if !content {
				widths[k] = bound[1] - bound[0]
				continue
			}
			if k > 0 {
				offsets[k] = max(0, lead-gap)
			}
			widths[k] = right - offsets[k]
			if k < len(junctions) {
				widths[k] += gap
			}
			widths[k] = min(widths[k], bound[1]-bound[0]-offsets[k])
		}
		for index := start; index <= end; index++ {
			var b strings.Builder
			for k, bound := range bounds {
				from := bound[0] + offsets[k]
				b.WriteString(terminalansi.Cut(lines[index], from, from+widths[k]))
				if k < len(junctions) {
					b.WriteString(terminalansi.Cut(lines[index], junctions[k], junctions[k]+1))
				}
			}
			lines[index] = b.String()
		}
		i = end
	}
	return strings.Join(lines, "\n")
}

// ruleJunctions returns the cell columns of "┼" in a table rule line, or nil
// when the line is not a rule.
func ruleJunctions(plain string) []int {
	trimmed := strings.TrimLeft(plain, " ")
	if !strings.Contains(trimmed, "┼") || strings.Trim(trimmed, "─┼ ") != "" {
		return nil
	}
	return runeColumns(plain, '┼')
}

func separatorsAt(plain string, separator rune, columns []int) bool {
	found := runeColumns(plain, separator)
	for _, column := range columns {
		if !slices.Contains(found, column) {
			return false
		}
	}
	return true
}

func runeColumns(plain string, target rune) []int {
	var columns []int
	column := 0
	for _, r := range plain {
		if r == target {
			columns = append(columns, column)
		}
		column += terminalansi.StringWidth(string(r))
	}
	return columns
}

func equalColumns(a, b []int) bool { return slices.Equal(a, b) }

// Glamour's table style exposes horizontal padding but no row padding or
// header-specific style. Marked presentation rows become horizontal rules after
// column layout, so wrapped cells stay together and native inline rendering is
// preserved. Emphasis makes headers distinct while retaining inline Markdown.
type tableLayout struct{ marker string }

func (layout *tableLayout) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	// A zero-width, source-unique marker identifies only injected rows; actual
	// empty cells and pipes in code blocks must never be treated as separators.
	layout.marker = "\u2063"
	for bytes.Contains(reader.Source(), []byte(layout.marker)) {
		layout.marker += "\u2063"
	}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		table, ok := node.(*asttable.Table)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		for row := table.FirstChild(); row != nil; {
			next := row.NextSibling()
			if row.Kind() == asttable.KindTableHeader {
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					header := ast.NewEmphasis(2)
					for child := cell.FirstChild(); child != nil; child = cell.FirstChild() {
						header.AppendChild(header, child)
					}
					cell.AppendChild(cell, header)
				}
			} else if next != nil {
				spacer := asttable.NewTableRow(table.Alignments)
				for column, alignment := range table.Alignments {
					cell := asttable.NewTableCell()
					cell.Alignment = alignment
					if column == 0 {
						cell.AppendChild(cell, ast.NewTextSegment(text.NewSegment(len(reader.Source()), len(reader.Source())+len(layout.marker))))
					}
					spacer.AppendChild(spacer, cell)
				}
				table.InsertAfter(table, row, spacer)
			}
			row = next
		}
		return ast.WalkContinue, nil
	})
}

func (layout *tableLayout) rules(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for index, line := range lines {
		if !strings.Contains(line, layout.marker) {
			continue
		}
		// Glamour already emits a header rule with the exact laid-out column
		// widths, junctions, styling, and enclosing blockquote/list indentation.
		// Reuse that geometry rather than guessing widths from Markdown source.
		for previous := index - 1; previous >= 0; previous-- {
			plain := terminalansi.Strip(lines[previous])
			start := strings.Index(plain, "─")
			if start < 0 {
				continue
			}
			if strings.Trim(plain[start:], "─┼ ") == "" {
				lines[index] = lines[previous]
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
