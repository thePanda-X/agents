package transcript

import (
	"bytes"
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
	prose    *markdownRenderer
	tables   *tableLayout
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
	r := &markdownRenderer{markdown: markdown, tables: tables}
	if width > 96 {
		r.prose = newMarkdownRenderer(96)
	}
	return r
}

func (r *markdownRenderer) Render(source string) (string, error) {
	var buffer bytes.Buffer
	data := []byte(source)
	document := r.markdown.Parser().Parse(text.NewReader(data))
	// Injected Text nodes reference an appended marker, never parsed as source
	// Markdown. Glamour supports Text segments but not synthetic String nodes.
	data = append(data, r.tables.marker...)
	if r.prose != nil {
		// Render parsed top-level blocks at their appropriate widths. Reuse the
		// original AST so links, nested markup, and table markers stay intact.
		var blocks []string
		for node := document.FirstChild(); node != nil; {
			next := node.NextSibling()
			wide := false
			_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
				if entering && (child.Kind() == ast.KindFencedCodeBlock || child.Kind() == ast.KindCodeBlock || child.Kind() == asttable.KindTable) {
					wide = true
					return ast.WalkStop, nil
				}
				return ast.WalkContinue, nil
			})
			chunk := ast.NewDocument()
			chunk.AppendChild(chunk, node)
			target := r.prose.markdown
			if wide {
				target = r.markdown
			}
			buffer.Reset()
			if err := target.Renderer().Render(&buffer, data, chunk); err != nil {
				return "", err
			}
			blocks = append(blocks, strings.Trim(r.tables.rules(buffer.String()), "\n"))
			node = next
		}
		return strings.Join(blocks, "\n\n"), nil
	}
	err := r.markdown.Renderer().Render(&buffer, data, document)
	return r.tables.rules(buffer.String()), err
}

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
