package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"os"
	"testing"
)

func clipboardTestPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestClipboardImageAttachmentIsInMemoryAndValidated(t *testing.T) {
	data := clipboardTestPNG(t)
	a, err := clipboardImageAttachment(data)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Image || a.MIME != "image/png" || a.Name != "clipboard.png" || a.Path != "" || a.URI != "" || !bytes.Equal(a.Data, data) {
		t.Fatalf("invalid clipboard attachment: %+v", a)
	}
	for _, invalid := range [][]byte{nil, []byte("not an image"), make([]byte, maxClipboardImageBytes+1)} {
		if _, err := clipboardImageAttachment(invalid); err == nil {
			t.Fatal("invalid/oversized image accepted")
		}
	}
}

func TestClipboardCommandHelper(t *testing.T) {
	if os.Getenv("AGENTS_CLIPBOARD_HELPER") != "1" {
		return
	}
	data := clipboardTestPNG(t)
	switch os.Args[len(os.Args)-1] {
	case "encoded":
		data = []byte(base64.StdEncoding.EncodeToString(data) + "\n")
	case "empty":
		data = nil
	case "error":
		os.Exit(3)
	}
	_, _ = os.Stdout.Write(data)
	os.Exit(0)
}

func TestClipboardCommandBoundedReadAndBase64(t *testing.T) {
	t.Setenv("AGENTS_CLIPBOARD_HELPER", "1")
	for _, encoded := range []bool{false, true} {
		mode := "raw"
		if encoded {
			mode = "encoded"
		}
		data, err := clipboardCommand(context.Background(), encoded, os.Args[0], "-test.run=^TestClipboardCommandHelper$", "--", mode)
		if err != nil || !bytes.Equal(data, clipboardTestPNG(t)) {
			t.Fatalf("clipboard output: %v", err)
		}
	}
	data, err := clipboardCommand(context.Background(), true, os.Args[0], "-test.run=^TestClipboardCommandHelper$", "--", "empty")
	if err != nil || len(data) != 0 {
		t.Fatal("empty clipboard failed")
	}
	if _, err := clipboardCommand(context.Background(), false, os.Args[0], "-test.run=^TestClipboardCommandHelper$", "--", "error"); err == nil {
		t.Fatal("command failure ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := clipboardCommand(ctx, false, os.Args[0]); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	buffer := &clipboardBuffer{limit: 4}
	if _, err := buffer.Write([]byte("12345")); err == nil || buffer.Len() > 4 {
		t.Fatal("clipboard output is not bounded")
	}
}
