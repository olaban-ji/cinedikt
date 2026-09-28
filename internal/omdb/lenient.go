package omdb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// OMDb's answers are not always JSON. Its text fields hold whatever was
// typed into them, with only the double quote reliably escaped: a
// backslash before a letter ("Romance\Drama"), an escaped apostrophe, a
// raw tab or a stray control byte in a plot, and now and then a
// backslash as the last character of a field, which escapes the quote
// meant to close it. A strict decoder refuses the whole answer over one
// of those, and the title then has no poster, date or synopsis however
// often it is asked.
//
// So a body that does not decode is repaired once and decoded again.
// Everything that reads an OMDb body goes through decode, so no answer
// is read one way in one place and another way somewhere else.

// decode reads an OMDb body into v.
//
// Strict decoding comes first, and a body that passes it is never
// touched. Only a body that is not valid JSON is repaired, and what
// still does not decode after that is ErrUnreadable, as is a body that
// is JSON but not of the shape asked for.
//
// A body that is not a JSON object at all is not OMDb's answer, broken
// or otherwise: an empty body, or a proxy's error page. That stays a
// plain error, so the caller asks again, rather than being recorded for
// good as the title's answer.
func decode[T any](body []byte, v *T) error {
	err := json.Unmarshal(body, v)
	if err == nil {
		return nil
	}
	if !isObject(body) {
		return fmt.Errorf("omdb: decode: %w", err)
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		// The body is JSON, only not of the shape asked for. Repairing
		// it cannot help: OMDb will send the same shape next time.
		return fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	// The second attempt starts from nothing, so no field of the first
	// can survive into an answer read from the repaired body.
	var zero T
	*v = zero
	if err := json.Unmarshal(repair(body), v); err != nil {
		return fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return nil
}

// isObject reports whether body is shaped like a JSON object: its first
// byte that is not white space opens one.
func isObject(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// repair rewrites what OMDb gets wrong inside its strings, in one pass
// over the bytes, and copies everything outside a string as it is.
//
// Inside a string:
//   - a raw control byte is not allowed. A tab, newline or carriage
//     return reads as a space between words; anything else is noise and
//     is dropped.
//   - \' is how OMDb escapes an apostrophe, and JSON has no such escape.
//     It becomes the apostrophe.
//   - \" is kept, unless the quote is where the string ends: followed by
//     a comma and the next key, or by the end of the object or array. A
//     backslash typed as a field's last character swallows the quote
//     that closes it, and the rest of the object misreads from there.
//     That backslash is kept as a literal one, and the quote closes the
//     string.
//   - a backslash before any other valid escape is kept as it is.
//   - a backslash before anything else is a literal backslash, and is
//     written as one: "inner\spiritual" reads as OMDb holds it, not as
//     a guess at what was meant.
//
// Bytes of 0x80 and above are copied as they come, so a multi-byte
// UTF-8 character is never split.
func repair(body []byte) []byte {
	out := make([]byte, 0, len(body)+16)
	inString := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !inString {
			out = append(out, c)
			inString = c == '"'
			continue
		}
		switch {
		case c == '"':
			out = append(out, c)
			inString = false
		case c < 0x20:
			if c == '\t' || c == '\n' || c == '\r' {
				out = append(out, ' ')
			}
		case c != '\\':
			out = append(out, c)
		case i+1 == len(body):
			// A backslash that ends the body escapes nothing.
			out = append(out, '\\', '\\')
		default:
			next := body[i+1]
			switch {
			case next == '\'':
				out = append(out, '\'')
				i++
			case next == '"':
				if endsString(body, i+2) {
					out = append(out, '\\', '\\', '"')
					inString = false
				} else {
					out = append(out, '\\', '"')
				}
				i++
			case strings.IndexByte(`\/bfnrt`, next) >= 0:
				out = append(out, '\\', next)
				i++
			case next == 'u' && hex4(body, i+2):
				out = append(out, body[i:i+6]...)
				i += 5
			default:
				// The byte after the backslash is left for the next turn
				// of the loop: it may be a control byte or the start of a
				// multi-byte character, and is handled as either.
				out = append(out, '\\', '\\')
			}
		}
	}
	return out
}

// endsString reports whether a quote just before at is where a string
// really ends: what follows, after any white space, is the end of the
// object or array, or a comma and then the next key.
func endsString(body []byte, at int) bool {
	i := skipSpace(body, at)
	if i == len(body) {
		return false
	}
	switch body[i] {
	case '}', ']':
		// What closes an object or array is followed by more structure
		// or by nothing. A brace or bracket in a plot, just after a quote
		// that really is escaped, is followed by more of the plot.
		j := skipSpace(body, i+1)
		if j == len(body) || body[j] == '}' || body[j] == ']' {
			return true
		}
		return body[j] == ',' && (isKey(body, j+1) || j+1 < len(body) && body[j+1] == '{')
	case ',':
		return isKey(body, i+1)
	}
	return false
}

// isKey reports whether body at i is a key and its colon: a quote, a
// name, a quote and a colon. OMDb's names are all plain words, and
// holding a name to letters, digits and underscores keeps a quoted word
// inside a plot from being taken for one.
func isKey(body []byte, i int) bool {
	if i >= len(body) || body[i] != '"' {
		return false
	}
	j := i + 1
	for j < len(body) && isWordByte(body[j]) {
		j++
	}
	return j > i+1 && j+1 < len(body) && body[j] == '"' && body[j+1] == ':'
}

func isWordByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_'
}

func skipSpace(body []byte, i int) int {
	for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\r' || body[i] == '\n') {
		i++
	}
	return i
}

// hex4 reports whether the four bytes at i are hex digits, as a \u
// escape needs.
func hex4(body []byte, i int) bool {
	if i+4 > len(body) {
		return false
	}
	for _, c := range body[i : i+4] {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// cleanText is a text field as the app keeps it. A valid \u00XX escape
// can still carry a control character past the decoder, and one in a
// plot shows up on a page as a box or nothing at all. Tab, newline and
// carriage return read as a space; any other is dropped. The result is
// trimmed.
func cleanText(s string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r == '\t', r == '\n', r == '\r':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(clean)
}
