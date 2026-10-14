package transcript

import "strings"

// LargeText is shared by the composer and sent-prompt folds. It uses bytes, not
// display width, and logical lines so the threshold does not change on resize.
func LargeText(text string) bool {
	return len(text) >= 4096 || strings.Count(text, "\n")+1 >= 40
}
