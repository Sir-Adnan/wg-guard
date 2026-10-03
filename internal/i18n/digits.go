package i18n

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DigitStyle affects human presentation only. Protocols and format helpers keep
// canonical Latin values; the web view explicitly applies this final layer.
type DigitStyle string

const (
	LatinDigits   DigitStyle = "latin"
	PersianDigits DigitStyle = "persian"
)

func (d DigitStyle) Valid() bool { return d == LatinDigits || d == PersianDigits }
func (d DigitStyle) Apply(text string) string {
	return strings.Map(func(r rune) rune {
		if r >= '۰' && r <= '۹' {
			r = '0' + r - '۰'
		} else if r >= '٠' && r <= '٩' {
			r = '0' + r - '٠'
		}
		if d == PersianDigits && r >= '0' && r <= '9' {
			return '۰' + r - '0'
		}
		return r
	}, text)
}

// Keep printf specifications and literal technical examples canonical. String
// arguments such as usernames, IPs and versions are never rewritten.
var proseTechnical = regexp.MustCompile(`%[-+# 0]*(?:\[[0-9]+\])?[0-9]*(?:\.[0-9]+)?[a-zA-Z%]|[0-9]+(?:\.[0-9]+){2,}(?:/[0-9]+)?|/[0-9]+|awg[0-9]+|[0-9]{4}-[0-9]{2}-[0-9]{2}`)

func (d DigitStyle) Prose(text string) string {
	text = LatinDigits.Apply(text)
	if d != PersianDigits {
		return text
	}
	var out strings.Builder
	last := 0
	for _, part := range proseTechnical.FindAllStringIndex(text, -1) {
		out.WriteString(d.Apply(text[last:part[0]]))
		out.WriteString(text[part[0]:part[1]])
		last = part[1]
	}
	out.WriteString(d.Apply(text[last:]))
	return out.String()
}

type styledNumber struct {
	value any
	style DigitStyle
}

func (n styledNumber) Format(state fmt.State, verb rune) {
	format := "%"
	for _, flag := range "-+# 0" {
		if state.Flag(int(flag)) {
			format += string(flag)
		}
	}
	if width, ok := state.Width(); ok {
		format += strconv.Itoa(width)
	}
	if precision, ok := state.Precision(); ok {
		format += "." + strconv.Itoa(precision)
	}
	format += string(verb)
	_, _ = state.Write([]byte(n.style.Apply(fmt.Sprintf(format, n.value))))
}

func TDigits(locale Locale, style DigitStyle, key string, args ...any) string {
	format := style.Prose(T(locale, key))
	if len(args) == 0 {
		return format
	}
	styled := append([]any(nil), args...)
	if style == PersianDigits {
		for n, arg := range styled {
			switch arg.(type) {
			case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
				styled[n] = styledNumber{arg, style}
			}
		}
	}
	return fmt.Sprintf(format, styled...)
}
