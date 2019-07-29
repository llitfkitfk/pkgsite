// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package frontend

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"path"
	"path/filepath"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
	"golang.org/x/discovery/internal"
	"golang.org/x/discovery/internal/postgres"
)

// ReadMeDetails contains all of the data that the readme template
// needs to populate.
type ReadMeDetails struct {
	ModulePath string
	ReadMe     template.HTML
}

// fetchReadMeDetails fetches data for the module version specified by path and version
// from the database and returns a ReadMeDetails.
func fetchReadMeDetails(ctx context.Context, db *postgres.DB, pkg *internal.VersionedPackage) (*ReadMeDetails, error) {
	return &ReadMeDetails{
		ModulePath: pkg.VersionInfo.ModulePath,
		ReadMe:     readmeHTML(pkg.VersionInfo.ReadmeFilePath, pkg.VersionInfo.ReadmeContents, pkg.RepositoryURL),
	}, nil
}

// readmeHTML sanitizes readmeContents based on bluemondy.UGCPolicy and returns
// a template.HTML. If readmeFilePath indicates that this is a markdown file,
// it will also render the markdown contents using blackfriday.
func readmeHTML(readmeFilePath string, readmeContents []byte, repositoryURL string) template.HTML {
	if filepath.Ext(readmeFilePath) != ".md" {
		return template.HTML(fmt.Sprintf(`<pre class="readme">%s</pre>`, html.EscapeString(string(readmeContents))))
	}

	// bluemonday.UGCPolicy allows a broad selection of HTML elements and
	// attributes that are safe for user generated content. This policy does
	// not whitelist iframes, object, embed, styles, script, etc.
	p := bluemonday.UGCPolicy()

	// Allow width and align attributes on img. This is used to size README
	// images appropriately where used, like the gin-gonic/logo/color.png
	// image in the github.com/gin-gonic/gin README.
	p.AllowAttrs("width", "align").OnElements("img")

	// The parsed repositoryURL is used to construct absolute image paths.
	parsedRepo, err := url.Parse(repositoryURL)
	if err != nil {
		parsedRepo = &url.URL{}
	}

	// blackfriday.Run() uses CommonHTMLFlags and CommonExtensions by default.
	renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{Flags: blackfriday.CommonHTMLFlags})
	parser := blackfriday.New(blackfriday.WithExtensions(blackfriday.CommonExtensions))

	b := &bytes.Buffer{}
	rootNode := parser.Parse(readmeContents)
	rootNode.Walk(func(node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		if node.Type == blackfriday.Image {
			translateRelativeLink(node, parsedRepo)
		}
		return renderer.RenderNode(b, node, entering)
	})
	return template.HTML(p.SanitizeReader(b).String())
}

// translateRelativeLink modifies a blackfriday.Node to convert relative image
// paths to absolute paths.
func translateRelativeLink(node *blackfriday.Node, repository *url.URL) {
	if repository.Hostname() != "github.com" {
		return
	}
	imageURL, err := url.Parse(string(node.LinkData.Destination))
	if err != nil || imageURL.IsAbs() {
		return
	}
	abs := &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: path.Join(repository.Path, "master", path.Clean(imageURL.Path))}
	node.LinkData.Destination = []byte(abs.String())
}
