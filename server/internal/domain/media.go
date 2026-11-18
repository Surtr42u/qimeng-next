package domain

import (
	"mime"
	"strings"
)

// Providers/browsers may have no MIME declaration, or use a conventional alias.
// This only interprets the declaration. Upload completion independently checks
// the actual container signature and media decoder before any publication.
func CompatibleMediaDeclaration(declared, canonical string) bool {
	parsed, _, e := mime.ParseMediaType(strings.TrimSpace(declared))
	if e != nil {
		return false
	}
	if parsed == "application/octet-stream" {
		return true
	}
	switch parsed {
	case "image/jpg", "image/pjpeg":
		parsed = "image/jpeg"
	case "image/x-png":
		parsed = "image/png"
	case "video/x-m4v", "video/m4v":
		parsed = "video/mp4"
	case "video/avi", "video/msvideo":
		parsed = "video/x-msvideo"
	}
	return parsed == canonical
}
