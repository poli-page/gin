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
// project + "welcome" template, provisioned into every organization's
// sandbox environment, is the canonical demo content; the API key
// selects the environment.
const (
	demoProject  = "getting-started"
	demoTemplate = "welcome"
	// missingTemplate is the template slug every Poli Page demo renders
	// to show a 404 TEMPLATE_NOT_FOUND.
	missingTemplate = "does-not-exist"
)

func demoInput() polipage.RenderInput {
	return polipage.RenderInput{
		Project:  demoProject,
		Template: demoTemplate,
		Data:     map[string]any{"name": "SDK Demo"},
	}
}

// registerRoutes wires the demo routes of the Poli Page SDK demo
// contract (GET /render/pdf, GET|DELETE /documents/:id,
// GET /documents/:id/preview, GET /documents/:id/thumbnails,
// GET /errors/not-found), a few extra SDK showcases, and the demo UI
// assets. Handlers are paper-thin — pull the client off c, call one SDK
// method, hand the result to a polipagegin helper, or queue the error
// with c.Error for the middleware's error handler. Production handlers
// in real Gin apps look the same.
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

	r.GET("/render/pdf", renderPDFHandler)
	r.GET("/render/stream", renderPDFStreamHandler)
	r.POST("/render/file", renderFileHandler)
	r.POST("/documents", postDocumentHandler)
	r.GET("/documents/:id", getDocumentHandler)
	r.GET("/documents/:id/preview", getDocumentPreviewHandler)
	r.GET("/documents/:id/thumbnails", getDocumentThumbnailsHandler)
	r.DELETE("/documents/:id", deleteDocumentHandler)
	r.GET("/errors/not-found", notFoundErrorHandler)
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

// notFoundErrorHandler renders a template that does not exist so the
// caller can exercise the error handler end-to-end. The API answers
// 404 TEMPLATE_NOT_FOUND; the middleware's default error handler answers
// 404 with the error's ToPayload() body {code, message, status, requestId}.
func notFoundErrorHandler(c *gin.Context) {
	client := polipagegin.ClientFrom(c)
	_, err := client.Render.PDF(c.Request.Context(), polipage.RenderInput{
		Project:  demoProject,
		Template: missingTemplate,
		Data:     map[string]any{},
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"unexpected": "no error returned for template " + missingTemplate})
}
