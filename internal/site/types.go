package site

import (
	"html/template"
	"time"
)

type Options struct {
	ContentDir string
	OutputDir  string
	BaseURL    string
	SiteTitle  string
	Language   string
}

type Config struct {
	Title    string
	Language string
	BaseURL  string
}

type frontMatter struct {
	Title string   `yaml:"title"`
	Date  string   `yaml:"date"`
	Tags  []string `yaml:"tags"`
}

type Post struct {
	Title       string
	Description string
	Date        time.Time
	Slug        string
	URL         string
	SourceRel   string
	Tags        []string
	Body        template.HTML
	Section     string
}

type Section struct {
	Name  string
	Slug  string
	URL   string
	Posts []*Post
}

type pageData struct {
	Site        Config
	PageTitle   string
	Description string
	Canonical   string
	Posts       []*Post
	Post        *Post
	Sections    []*Section
	Section     *Section
}

type BuildResult struct {
	Posts  int
	Assets int
	Output string
}
