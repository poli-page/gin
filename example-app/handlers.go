package main

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
)

// Demo fixtures shared by all render handlers. The "getting-started"
// project + "welcome" template at version 1.0.0 is the canonical demo
// content on api-develop.poli.page.
const (
	demoProject  = "getting-started"
	demoTemplate = "welcome"
	demoVersion  = "1.0.0"
)

func demoInput() polipage.ProjectModeInput {
	return polipage.ProjectModeInput{
		Project:  demoProject,
		Template: demoTemplate,
		Version:  polipage.Opt(demoVersion),
		Data:     map[string]any{"name": "demo"},
	}
}

// registerRoutes wires every row of spec §14.1's table plus the demo
// UI assets. Handlers are paper-thin — pull the client off c, call one
// SDK method, hand the result to a polipagegin helper. Production
// handlers in real Gin apps look the same.
func registerRoutes(r *gin.Engine, assets embed.FS) {
	// Demo dashboard at /. The HTML is the entry point; static/* serves
	// the CSS and JS it references.
	r.GET("/", func(c *gin.Context) {
		c.FileFromFS("templates/demo.html", http.FS(assets))
	})
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	r.StaticFS("/static", http.FS(staticFS))

	api := r.Group("/api")
	api.GET("/render/pdf", renderPDFHandler)
	api.GET("/render/stream", renderPDFStreamHandler)
	api.GET("/render/preview", renderPreviewHandler)
	api.POST("/render/file", renderFileHandler)
	api.POST("/documents", postDocumentHandler)
	api.GET("/documents/:id", getDocumentHandler)
	api.GET("/documents/:id/preview", getDocumentPreviewHandler)
	api.GET("/documents/:id/thumbnails", getDocumentThumbnailsHandler)
	api.DELETE("/documents/:id", deleteDocumentHandler)
	api.GET("/render/error", renderErrorHandler)
}

func renderPDFHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	pdf, err := client.Render.PDF(c.Request.Context(), demoInput())
	if err != nil {
		_ = c.Error(err)
		return
	}
	polipagegin.PDF(c, pdf, polipagegin.PDFOptions{Filename: "welcome.pdf", Inline: true})
}

func renderPDFStreamHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	body, err := client.Render.PDFStream(c.Request.Context(), demoInput())
	if err != nil {
		_ = c.Error(err)
		return
	}
	defer func() { _ = body.Close() }()
	polipagegin.PDFStream(c, body, polipagegin.PDFOptions{Filename: "welcome.pdf", Inline: true})
}

func renderPreviewHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	result, err := client.Render.Preview(c.Request.Context(), demoInput())
	if err != nil {
		_ = c.Error(err)
		return
	}
	polipagegin.Preview(c, result.HTML)
}

// renderFileHandler streams a PDF straight to disk via polipage.RenderToFile.
// Memory-bounded regardless of PDF size — the SDK pipes the body through
// io.Copy without buffering. Parent directories are created if missing.
func renderFileHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	path := filepath.Join("output", "welcome.pdf")
	if err := polipage.RenderToFile(c.Request.Context(), client, demoInput(), path); err != nil {
		_ = c.Error(err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"path":      path,
		"sizeBytes": info.Size(),
	})
}

func postDocumentHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	doc, err := client.Render.Document(c.Request.Context(), demoInput())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"documentId": doc.DocumentID,
		"expiresAt":  doc.ExpiresAt,
	})
}

func getDocumentHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	doc, err := client.Documents.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	polipagegin.DocumentRedirect(c, doc)
}

func getDocumentPreviewHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	result, err := client.Documents.Preview(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	polipagegin.Preview(c, result.HTML)
}

func getDocumentThumbnailsHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	thumbs, err := client.Documents.Thumbnails(c.Request.Context(), c.Param("id"), polipage.ThumbnailOptions{
		Width: 200,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"thumbnails": thumbs})
}

func deleteDocumentHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	if err := client.Documents.Delete(c.Request.Context(), c.Param("id")); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// renderErrorHandler triggers a deliberate validation error so the
// caller can exercise ErrorMiddleware end-to-end. An invalid semver
// version surfaces from the API as VALIDATION_ERROR / 400; the
// middleware maps it to a JSON {code, message, requestId} response.
func renderErrorHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	badInput := demoInput()
	badInput.Version = polipage.Opt("not-a-semver-version")
	_, err := client.Render.PDF(c.Request.Context(), badInput)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"unexpected": "no error returned from deliberately bad version"})
}
