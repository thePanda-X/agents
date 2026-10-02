package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"agents/internal/harness"
)

var ErrNoClipboardImage = errors.New("clipboard does not contain a supported image")

const maxClipboardImageBytes = 8 << 20

// ReadClipboardImage reads the local desktop clipboard, not the remote terminal
// clipboard. It creates an in-memory attachment; no files or prompts are written.
func ReadClipboardImage(ctx context.Context) (harness.Attachment, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var data []byte
	var err error
	switch runtime.GOOS {
	case "darwin":
		data, err = clipboardCommand(ctx, true, "osascript", "-l", "JavaScript", "-e", macClipboardImage)
	case "windows":
		data, err = clipboardCommand(ctx, true, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsClipboardImage)
	case "linux":
		command, args := "", []string(nil)
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			command, args = "wl-paste", []string{"--no-newline", "--type"}
		} else if os.Getenv("DISPLAY") != "" {
			command, args = "xclip", []string{"-selection", "clipboard", "-out", "-target"}
		} else {
			return harness.Attachment{}, errors.New("image paste needs a local desktop clipboard (Wayland or X11); use /attach for remote/SSH sessions")
		}
		for _, mime := range []string{"image/png", "image/jpeg", "image/gif"} {
			data, err = clipboardCommand(ctx, false, command, append(args, mime)...)
			if err == nil {
				break
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				return harness.Attachment{}, err
			}
		}
		if err != nil && ctx.Err() == nil {
			return harness.Attachment{}, ErrNoClipboardImage
		}
	default:
		return harness.Attachment{}, fmt.Errorf("clipboard image paste is unavailable on %s; use /attach", runtime.GOOS)
	}
	if err != nil {
		return harness.Attachment{}, err
	}
	if len(data) == 0 {
		return harness.Attachment{}, ErrNoClipboardImage
	}
	return clipboardImageAttachment(data)
}

func clipboardImageAttachment(data []byte) (harness.Attachment, error) {
	if len(data) > maxClipboardImageBytes {
		return harness.Attachment{}, errors.New("clipboard image exceeds 8 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return harness.Attachment{}, fmt.Errorf("invalid clipboard image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width) > 40_000_000/int64(config.Height) {
		return harness.Attachment{}, errors.New("clipboard image exceeds 40 megapixels or has invalid dimensions")
	}
	return harness.Attachment{Name: "clipboard." + format, MIME: "image/" + format, Image: true, Data: data}, nil
}

type clipboardBuffer struct {
	bytes.Buffer
	limit int
}

func (b *clipboardBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, errors.New("clipboard image exceeds 8 MiB")
	}
	return b.Buffer.Write(data)
}

func clipboardCommand(ctx context.Context, encoded bool, name string, args ...string) ([]byte, error) {
	limit := maxClipboardImageBytes
	if encoded {
		limit = base64.StdEncoding.EncodedLen(limit) + 2 // command's trailing newline
	}
	output := &clipboardBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	cmd.Stdout = output
	// Do not retain stderr: it can contain platform-specific clipboard details.
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("image paste requires %s to be installed; use /attach instead", name)
		}
		return nil, fmt.Errorf("read clipboard with %s: %w", name, err)
	}
	if encoded {
		return base64.StdEncoding.DecodeString(strings.TrimSpace(output.String()))
	}
	return output.Bytes(), nil
}

const macClipboardImage = `
ObjC.import('AppKit');
function run() {
  var clipboard = $.NSPasteboard.generalPasteboard;
  var data = clipboard.dataForType($.NSPasteboardTypePNG);
  if (!data) data = clipboard.dataForType('public.jpeg');
  if (!data) data = clipboard.dataForType('com.compuserve.gif');
  if (!data) {
    var tiff = clipboard.dataForType($.NSPasteboardTypeTIFF);
    if (!tiff) return '';
    if (Number(tiff.length) > 32 * 1024 * 1024) throw Error('Clipboard image too large');
    var bitmap = $.NSBitmapImageRep.imageRepWithData(tiff);
    if (!bitmap) throw Error('Invalid clipboard image');
    if (Number(bitmap.pixelsWide) * Number(bitmap.pixelsHigh) > 40000000) throw Error('Clipboard image too large');
    data = bitmap.representationUsingTypeProperties(4, $.NSDictionary.dictionary);
  }
  if (!data || Number(data.length) > 8 * 1024 * 1024) throw Error('Clipboard image too large');
  return ObjC.unwrap(data.base64EncodedStringWithOptions(0));
}`

const windowsClipboardImage = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$image = [System.Windows.Forms.Clipboard]::GetImage()
if ($null -eq $image) { exit 0 }
$stream = New-Object System.IO.MemoryStream
try {
  if ([long]$image.Width * [long]$image.Height -gt 40000000) { throw 'Clipboard image too large' }
  $image.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
  if ($stream.Length -gt 8MB) { throw 'Clipboard image too large' }
  [Convert]::ToBase64String($stream.ToArray())
} finally { $stream.Dispose(); $image.Dispose() }
`
