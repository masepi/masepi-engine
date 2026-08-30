package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishIncrementallyRebuildsDependencies(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	siteRoot := filepath.Join(root, "site")
	mustWrite(t, filepath.Join(content, "@home", "index.md"), "---\ntitle: Home\n---\nHome.\n")
	mustWrite(t, filepath.Join(content, "blog", "one.md"), "---\ntitle: One\ndate: 2026-01-01\n---\nFirst body.\n")
	mustWrite(t, filepath.Join(content, "blog", "two.md"), "---\ntitle: Two\ndate: 2026-01-02\n---\nSecond body.\n")
	mustWrite(t, filepath.Join(content, "blog", "photo.jpg"), "one")

	first, err := Publish(testPublishOptions(content, siteRoot, "commit-one", "engine-one"))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Full {
		t.Fatal("first release must be a full build")
	}
	firstDir := currentDirectory(t, siteRoot)
	unchangedBefore := mustStat(t, filepath.Join(firstDir, "blog", "two", "index.html"))
	indexBefore := mustStat(t, filepath.Join(firstDir, "blog", "index.html"))

	mustWrite(t, filepath.Join(content, "blog", "one.md"), "---\ntitle: One\ndate: 2026-01-01\n---\nChanged body.\n")
	second, err := Publish(testPublishOptions(content, siteRoot, "commit-two", "engine-one"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Full || second.Changed != 1 {
		t.Fatalf("expected one incremental input, got %+v", second)
	}
	secondDir := currentDirectory(t, siteRoot)
	if !strings.Contains(mustRead(t, filepath.Join(secondDir, "blog", "one", "index.html")), "Changed body") {
		t.Fatal("changed post was not rebuilt")
	}
	if !os.SameFile(unchangedBefore, mustStat(t, filepath.Join(secondDir, "blog", "two", "index.html"))) {
		t.Fatal("unchanged post was not hard-linked from previous release")
	}
	if !os.SameFile(indexBefore, mustStat(t, filepath.Join(secondDir, "blog", "index.html"))) {
		t.Fatal("section index changed after a body-only update")
	}
	postBeforeAsset := mustStat(t, filepath.Join(secondDir, "blog", "two", "index.html"))
	mustWrite(t, filepath.Join(content, "blog", "photo.jpg"), "two")
	assetResult, err := Publish(testPublishOptions(content, siteRoot, "commit-asset", "engine-one"))
	if err != nil {
		t.Fatal(err)
	}
	if assetResult.Full || mustRead(t, filepath.Join(currentDirectory(t, siteRoot), "blog", "photo.jpg")) != "two" {
		t.Fatal("asset-only update was not incremental")
	}
	if !os.SameFile(postBeforeAsset, mustStat(t, filepath.Join(currentDirectory(t, siteRoot), "blog", "two", "index.html"))) {
		t.Fatal("asset update rebuilt an unrelated post")
	}

	mustWrite(t, filepath.Join(content, "blog", "one.md"), "---\ntitle: Renamed\ndate: 2026-01-01\n---\nChanged body.\n")
	third, err := Publish(testPublishOptions(content, siteRoot, "commit-three", "engine-one"))
	if err != nil {
		t.Fatal(err)
	}
	if third.Full {
		t.Fatal("metadata-only post update should remain incremental")
	}
	thirdDir := currentDirectory(t, siteRoot)
	assertContains(t, mustRead(t, filepath.Join(thirdDir, "blog", "index.html")), "Renamed")

	mustWrite(t, filepath.Join(content, "notes", "index.md"), "---\ntitle: Notes\n---\nNotes.\n")
	fourth, err := Publish(testPublishOptions(content, siteRoot, "commit-four", "engine-one"))
	if err != nil {
		t.Fatal(err)
	}
	if !fourth.Full {
		t.Fatal("adding a navigation section must trigger a full build")
	}
}

func TestPublishFailureKeepsCurrentRelease(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	siteRoot := filepath.Join(root, "site")
	mustWrite(t, filepath.Join(content, "home", "index.md"), "---\ntitle: Home\n---\nValid.\n")
	if _, err := Publish(testPublishOptions(content, siteRoot, "good", "engine")); err != nil {
		t.Fatal(err)
	}
	before := currentDirectory(t, siteRoot)
	mustWrite(t, filepath.Join(content, "home", "index.md"), "---\ntitle: Broken\n")
	if _, err := Publish(testPublishOptions(content, siteRoot, "bad", "engine")); err == nil {
		t.Fatal("expected invalid Markdown to fail")
	}
	after := currentDirectory(t, siteRoot)
	if before != after {
		t.Fatalf("failed build switched release: %s -> %s", before, after)
	}
}

func TestPublishRebuildsSectionWhenCardinalityChanges(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	siteRoot := filepath.Join(root, "site")
	mustWrite(t, filepath.Join(content, "@home", "index.md"), "---\ntitle: Home\n---\nHome.\n")
	mustWrite(t, filepath.Join(content, "blog", "one.md"), "---\ntitle: One\n---\nOne.\n")
	if _, err := Publish(testPublishOptions(content, siteRoot, "one", "engine")); err != nil {
		t.Fatal(err)
	}
	firstDir := currentDirectory(t, siteRoot)
	assertContains(t, mustRead(t, filepath.Join(firstDir, "blog", "index.html")), "One.")

	mustWrite(t, filepath.Join(content, "blog", "two.md"), "---\ntitle: Two\n---\nTwo.\n")
	result, err := Publish(testPublishOptions(content, siteRoot, "two", "engine"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Full {
		t.Fatal("cardinality transition inside an existing section should be incremental")
	}
	secondDir := currentDirectory(t, siteRoot)
	assertContains(t, mustRead(t, filepath.Join(secondDir, "blog", "index.html")), "article-list")
	assertContains(t, mustRead(t, filepath.Join(secondDir, "blog", "one", "index.html")), "One.")

	if err := os.Remove(filepath.Join(content, "blog", "two.md")); err != nil {
		t.Fatal(err)
	}
	result, err = Publish(testPublishOptions(content, siteRoot, "three", "engine"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Full {
		t.Fatal("reverse cardinality transition should be incremental")
	}
	thirdDir := currentDirectory(t, siteRoot)
	assertContains(t, mustRead(t, filepath.Join(thirdDir, "blog", "index.html")), "One.")
	if _, err := os.Stat(filepath.Join(thirdDir, "blog", "one", "index.html")); !os.IsNotExist(err) {
		t.Fatal("obsolete nested URL remains after returning to a single-post section")
	}
}

func currentDirectory(t *testing.T, siteRoot string) string {
	t.Helper()
	value, err := filepath.EvalSymlinks(filepath.Join(siteRoot, "current"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testPublishOptions(content, siteRoot, contentVersion, engineVersion string) PublishOptions {
	return PublishOptions{
		ContentDir: content, SiteRoot: siteRoot, ContentVersion: contentVersion,
		EngineVersion: engineVersion, SiteTitle: "Test site", Language: "en",
	}
}

func mustStat(t *testing.T, name string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
