package polipagegin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentDisposition(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		inline   bool
		want     string
	}{
		{
			name:     "ascii filename",
			filename: "invoice-42.pdf",
			want:     `attachment; filename="invoice-42.pdf"; filename*=UTF-8''invoice-42.pdf`,
		},
		{
			name:     "non-ascii filename is downgraded to underscores in the ascii slot and percent-encoded in the UTF-8 slot",
			filename: "facture-élève.pdf",
			want:     `attachment; filename="facture-_l_ve.pdf"; filename*=UTF-8''facture-%C3%A9l%C3%A8ve.pdf`,
		},
		{
			name:     "double quote is backslash-escaped in ascii slot and percent-encoded in UTF-8 slot",
			filename: `bad"name.pdf`,
			want:     `attachment; filename="bad\"name.pdf"; filename*=UTF-8''bad%22name.pdf`,
		},
		{
			name:     "backslash is escaped in ascii slot and percent-encoded in UTF-8 slot",
			filename: `a\b.pdf`,
			want:     `attachment; filename="a\\b.pdf"; filename*=UTF-8''a%5Cb.pdf`,
		},
		{
			name:     "empty filename defaults to document.pdf",
			filename: "",
			want:     `attachment; filename="document.pdf"; filename*=UTF-8''document.pdf`,
		},
		{
			name:     "inline disposition for previewable attachments",
			filename: "invoice-42.pdf",
			inline:   true,
			want:     `inline; filename="invoice-42.pdf"; filename*=UTF-8''invoice-42.pdf`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := contentDisposition(tc.filename, tc.inline)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAsciiFallback(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain ascii passes through", "invoice-42.pdf", "invoice-42.pdf"},
		{"double quote is backslash-escaped", `bad"name.pdf`, `bad\"name.pdf`},
		{"backslash is escaped", `a\b.pdf`, `a\\b.pdf`},
		{"non-ascii rune becomes underscore (one per rune, not per UTF-8 byte)", "facture-élève.pdf", "facture-_l_ve.pdf"},
		{"control character becomes underscore", "tab\there.pdf", "tab_here.pdf"},
		{"DEL (0x7F) becomes underscore", "x\x7fy.pdf", "x_y.pdf"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, asciiFallback(tc.input))
		})
	}
}
