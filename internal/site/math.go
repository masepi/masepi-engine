package site

import (
	"bytes"

	mathjax "github.com/litao91/goldmark-mathjax"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type mathExtension struct{}

func (mathExtension) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(parser.WithBlockParsers(
		util.Prioritized(mathjax.NewMathJaxBlockParser(), 701),
	))
	markdown.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(mathjax.NewInlineMathParser(), 501),
	))
	markdown.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(mathHTMLRenderer{}, 501),
	))
}

type mathHTMLRenderer struct{}

func (mathHTMLRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(mathjax.KindInlineMath, renderInlineMath)
	registerer.Register(mathjax.KindMathBlock, renderBlockMath)
}

func renderInlineMath(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString(`\)</span>`)
		return ast.WalkContinue, nil
	}

	_, _ = w.WriteString(`<span class="math-inline">\(`)
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		segment := child.(*ast.Text).Segment
		value := segment.Value(source)
		endsWithNewline := bytes.HasSuffix(value, []byte("\n"))
		if endsWithNewline {
			value = value[:len(value)-1]
		}
		_, _ = w.Write(util.EscapeHTML(value))
		if endsWithNewline && child != node.LastChild() {
			_, _ = w.WriteString(" ")
		}
	}
	return ast.WalkSkipChildren, nil
}

func renderBlockMath(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString(`\]</div>` + "\n")
		return ast.WalkContinue, nil
	}

	_, _ = w.WriteString(`<div class="math-display">\[`)
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = w.Write(util.EscapeHTML(line.Value(source)))
	}
	return ast.WalkContinue, nil
}
