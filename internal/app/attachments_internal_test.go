package app

import "testing"

func TestAttachmentMediaTypeUsesAConservativeAllowlist(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "note.txt", want: "text/plain"},
		{path: "README.MD", want: "text/markdown"},
		{path: "data.json", want: "application/json"},
		{path: "data.csv", want: "text/csv"},
		{path: "page.html", want: "text/html"},
		{path: "feed.xml", want: "application/xml"},
		{path: "document.pdf", want: "application/pdf"},
		{path: "image.png", want: "image/png"},
		{path: "photo.jpg", want: "image/jpeg"},
		{path: "photo.jpeg", want: "image/jpeg"},
		{path: "animation.gif", want: "image/gif"},
		{path: "archive.zip", want: "application/octet-stream"},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := attachmentMediaType(test.path); got != test.want {
				t.Errorf("attachmentMediaType(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}
