package transcript

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// LinkAt returns the target under a rendered terminal cell. OSC8 metadata keeps
// Markdown labels, reference links, and wrapped URLs tied to their full target.
// Plain URLs are also recognized in unformatted prompts and tool output.
func LinkAt(line string, column int) string {
	if column < 2 { // The transcript gutter is not content.
		return ""
	}
	position, target := 0, ""
	var state byte
	for remaining := line; len(remaining) > 0; {
		sequence, width, n, next := ansi.DecodeSequence(remaining, state, nil)
		if ansi.HasOscPrefix(sequence) {
			data := strings.TrimPrefix(strings.TrimPrefix(sequence, "\x1b]"), "\x9d")
			data = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(data, "\x07"), "\x1b\\"), "\x9c")
			if strings.HasPrefix(data, "8;") {
				_, target, _ = strings.Cut(data[2:], ";")
			}
		}
		if width > 0 && column < position+width {
			if target != "" {
				return target
			}
			break
		}
		position += width
		remaining, state = remaining[n:], next
	}
	plain := ansi.Strip(line)
	for _, match := range webURL.FindAllStringIndex(plain, -1) {
		url := trimURLPunctuation(plain[match[0]:match[1]])
		start := ansi.StringWidth(plain[:match[0]])
		if column >= start && column < start+ansi.StringWidth(url) {
			return url
		}
	}
	return ""
}

var webURL = regexp.MustCompile("(?i)https?://[^\\s<>\"'`]+")

func trimURLPunctuation(url string) string {
	for len(url) > 0 {
		last := url[len(url)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?':
		case ')', ']', '}':
			opening := map[byte]byte{')': '(', ']': '[', '}': '{'}[last]
			if strings.Count(url, string(last)) <= strings.Count(url, string(opening)) {
				return url
			}
		default:
			return url
		}
		url = url[:len(url)-1]
	}
	return url
}
