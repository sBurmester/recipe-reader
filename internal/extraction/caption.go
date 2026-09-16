package extraction

import (
	"log/slog"
	"unicode/utf8"
)

// maxCaptionRunes caps the caption sent to the model. Instagram limits a real
// caption to 2,200 characters, so an ordinary post never reaches it. What does
// is a caption field nothing upstream validates — shaped by whoever wrote the
// post, or by a change in the unofficial API's response — on a run that pays
// per input token for every post it imports. At three to four characters a
// token this is a ceiling of roughly 2–3k input tokens, which is the same order
// as the 2048-token output cap.
const maxCaptionRunes = 8000

// capCaption returns caption cut to at most maxCaptionRunes runes.
//
// It cuts by runes, not bytes: these captions are German and emoji-heavy, and a
// byte slice through a multi-byte character would hand the model invalid UTF-8.
func capCaption(caption string) string {
	if utf8.RuneCountInString(caption) <= maxCaptionRunes {
		return caption
	}
	runes := 0
	for i := range caption {
		if runes == maxCaptionRunes {
			slog.Warn("extraction: caption truncated before the LLM call",
				"runes", utf8.RuneCountInString(caption), "kept", maxCaptionRunes)
			return caption[:i]
		}
		runes++
	}
	return caption
}
