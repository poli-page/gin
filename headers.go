package polipagegin

import (
	"fmt"
	"strings"
)

// defaultDownloadFilename is the filename used when the caller passes the
// empty string. Both Content-Disposition slots get this value verbatim.
const defaultDownloadFilename = "document.pdf"

// contentDisposition builds a Content-Disposition header value per RFC 6266.
// The result has two slots:
//
//   - an ASCII filename="..." slot for legacy User-Agents that do not speak
//     RFC 5987 (the asciiFallback output, with backslashes and quotes
//     already escaped);
//   - an RFC 5987 ext-value slot (filename*=UTF-8 prefix, then the
//     percent-encoded UTF-8 bytes of the original filename), for modern UAs.
//
// inline picks the "inline" disposition (the UA may render the resource
// in-place, e.g. inside an <iframe>); false picks "attachment" (download).
func contentDisposition(filename string, inline bool) string {
	if filename == "" {
		filename = defaultDownloadFilename
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	return disposition + `; filename="` + asciiFallback(filename) +
		`"; filename*=UTF-8''` + rfc5987Encode(filename)
}

// asciiFallback returns the body of the ASCII filename="..." slot — that
// is, the string that appears between the surrounding quotes. Backslashes
// and double quotes are backslash-escaped (RFC 6266 §4.1 quoted-string);
// non-ASCII runes and control characters collapse to "_".
//
// The result never contains a literal "\"" or "\\" outside of the
// escape pair, and never contains a non-printable byte — so it can be
// safely interpolated into a "filename=\"<body>\"" header.
func asciiFallback(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r > 0x7E:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rfc5987Encode percent-encodes s for the ext-value slot defined in
// RFC 5987 §3.2.1. Only attr-chars (ALPHA / DIGIT / a small punctuation
// set) are kept literal; every other byte — including space, "/", ":",
// quotes, and every multi-byte UTF-8 sequence — is emitted as %HH.
//
// The iteration is byte-by-byte (not rune-by-rune) because the wire
// format is the UTF-8 byte stream percent-encoded; "é" must come out as
// "%C3%A9", not as one percent-escape of the Unicode code point.
func rfc5987Encode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// isAttrChar reports whether c is a member of the RFC 5987 attr-char set,
// the only characters that may appear unencoded in an ext-value.
func isAttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z',
		c >= 'A' && c <= 'Z',
		c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
