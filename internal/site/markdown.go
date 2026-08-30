package site

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

type markdownRenderer struct {
	engine goldmark.Markdown
}

var (
	imageTagPattern       = `<img src="[^"]*" alt="([^"]*)"(?: title="[^"]*")?>`
	imagePairPattern      = regexp.MustCompile(`<p>(` + imageTagPattern + `)\s*(` + imageTagPattern + `)</p>`)
	imageParagraphPattern = regexp.MustCompile(`<p>(` + imageTagPattern + `)</p>`)
)

func newMarkdownRenderer() *markdownRenderer {
	return &markdownRenderer{engine: goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
			mathExtension{},
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)}
}

func splitFrontMatter(input []byte) (frontMatter, []byte, error) {
	var fm frontMatter
	input = bytes.TrimPrefix(input, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(input, []byte("---\n")) && !bytes.HasPrefix(input, []byte("---\r\n")) {
		return fm, input, nil
	}

	lines := bytes.Split(input, []byte("\n"))
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return fm, nil, fmt.Errorf("front matter is not closed with ---")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(bytes.Join(lines[1:end], []byte("\n"))))
	if err := decoder.Decode(&fm); err != nil {
		return fm, nil, fmt.Errorf("invalid front matter: %w", err)
	}
	return fm, bytes.Join(lines[end+1:], []byte("\n")), nil
}

func (r *markdownRenderer) render(source []byte, sourceRel, title string) (template.HTML, string, error) {
	doc := r.engine.Parser().Parse(text.NewReader(source))
	rewriteDestinations(doc, sourceRel)
	removeDuplicateTitle(doc, source, title)
	description := firstParagraph(doc, source)

	var output bytes.Buffer
	if err := r.engine.Renderer().Render(&output, source, doc); err != nil {
		return "", "", err
	}
	rendered := imagePairPattern.ReplaceAllString(output.String(), `<div class="image-pair"><figure>$1<figcaption>$2</figcaption></figure><figure>$3<figcaption>$4</figcaption></figure></div>`)
	rendered = imageParagraphPattern.ReplaceAllString(rendered, `<figure>$1<figcaption>$2</figcaption></figure>`)
	return template.HTML(rendered), description, nil
}

func rewriteDestinations(doc ast.Node, sourceRel string) {
	sourceDir := path.Dir(filepath.ToSlash(sourceRel))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var destination *[]byte
		switch n := node.(type) {
		case *ast.Image:
			destination = &n.Destination
		case *ast.Link:
			destination = &n.Destination
		default:
			return ast.WalkContinue, nil
		}
		raw := string(*destination)
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "/") {
			return ast.WalkContinue, nil
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			return ast.WalkContinue, nil
		}
		clean := path.Clean(path.Join("/", sourceDir, parsed.Path))
		if strings.EqualFold(path.Ext(clean), ".md") {
			clean = strings.TrimSuffix(clean, path.Ext(clean)) + "/"
		}
		parsed.Path = clean
		*destination = []byte(parsed.String())
		return ast.WalkContinue, nil
	})
}

func removeDuplicateTitle(doc ast.Node, source []byte, title string) {
	first := doc.FirstChild()
	heading, ok := first.(*ast.Heading)
	if !ok || heading.Level != 1 {
		return
	}
	if strings.EqualFold(strings.TrimSpace(nodeText(heading, source)), strings.TrimSpace(title)) {
		doc.RemoveChild(doc, heading)
	}
}

func firstParagraph(doc ast.Node, source []byte) string {
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() != ast.KindParagraph {
			continue
		}
		text := strings.Join(strings.Fields(nodeText(child, source)), " ")
		return truncateRunes(text, 180)
	}
	return ""
}

func nodeText(node ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(node, func(current ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := current.(type) {
		case *ast.Text:
			b.Write(n.Segment.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.CodeSpan:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				if text, ok := child.(*ast.Text); ok {
					b.Write(text.Segment.Value(source))
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	cut := limit
	for cut > 0 && !unicode.IsSpace(runes[cut-1]) {
		cut--
	}
	if cut < limit/2 {
		cut = limit
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}

func parseDate(raw string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected YYYY-MM-DD or RFC3339, got %q", raw)
}
