// Package terminal provides a small, streaming English terminal interface.
// It owns presentation and input only; callers own actions and persistence.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"golang.org/x/term"
)

var ErrCanceled = errors.New("terminal: canceled")
var ErrBack = errors.New("terminal: back")

type Options struct {
	Context    context.Context
	Locale     i18n.Locale
	Width      int
	TTY, Color bool
}
type UI struct {
	Context  context.Context
	In       io.Reader
	Out      io.Writer
	Locale   i18n.Locale
	width    int
	color    bool
	inputTTY bool
}

const (
	colorReset   = "\x1b[0m"
	colorInfo    = "\x1b[36m"
	colorHeading = "\x1b[36;1m"
	colorSuccess = "\x1b[32;1m"
	colorWarning = "\x1b[33;1m"
	colorFailure = "\x1b[31;1m"
)

// StatusField is one compact, sanitized value in a manager status card.
type StatusField struct {
	Label string
	Value string
}

// Detect uses the actual input/output files; redirected output is always plain.
func Detect(in io.Reader, out io.Writer, locale i18n.Locale) Options {
	o := Options{Locale: locale, Width: 80, Color: true}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		o.TTY = true
		if w, _, err := term.GetSize(int(f.Fd())); err == nil {
			o.Width = w
		}
	}
	return o
}
func IsTerminal(in io.Reader) bool { f, ok := in.(*os.File); return ok && term.IsTerminal(int(f.Fd())) }

// Digits normalizes numerals only at numeric-field call sites, never secrets or paths.
func Digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '۰' && r <= '۹' {
			return r - '۰' + '0'
		}
		if r >= '٠' && r <= '٩' {
			return r - '٠' + '0'
		}
		return r
	}, s)
}
func New(in io.Reader, out io.Writer, o Options) *UI {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	if !o.Locale.Valid() {
		o.Locale = i18n.En
	}
	if o.Width <= 0 {
		o.Width = 80
	}
	if o.Width < 20 {
		o.Width = 20
	}
	if o.Width > 160 {
		o.Width = 160
	}
	if o.Context == nil {
		o.Context = context.Background()
	}
	return &UI{Context: o.Context, In: in, Out: out, Locale: o.Locale, width: o.Width, color: o.Color && o.TTY && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb", inputTTY: IsTerminal(in)}
}
func (u *UI) T(key string, args ...any) string { return i18n.T(u.Locale, key, args...) }

// Clean removes terminal escapes, controls and bidi overrides from dynamic data.
func Clean(s string) string {
	var b strings.Builder
	state := 0
	for _, r := range s {
		if state == 1 {
			if r == '[' {
				state = 2
			} else if r == ']' {
				state = 3
			} else {
				state = 0
			}
			continue
		}
		if state == 2 {
			if r >= 0x40 && r <= 0x7e {
				state = 0
			}
			continue
		}
		if state == 3 {
			if r == 7 {
				state = 0
			} else if r == 27 {
				state = 4
			}
			continue
		}
		if state == 4 {
			if r == '\\' {
				state = 0
			} else {
				state = 3
			}
			continue
		}
		if r == 27 {
			state = 1
			continue
		}
		if unicode.IsControl(r) || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
func (u *UI) Text(s string) {
	for _, p := range strings.Split(s, "\n") {
		runes := []rune(Clean(p))
		for len(runes) > u.width {
			cut := u.width
			for i := cut; i > cut/2; i-- {
				if runes[i-1] == ' ' {
					cut = i
					break
				}
			}
			fmt.Fprintln(u.Out, strings.TrimRight(string(runes[:cut]), " "))
			runes = runes[cut:]
		}
		fmt.Fprintln(u.Out, string(runes))
	}
}

func (u *UI) tone(code, message string) {
	if u.color {
		fmt.Fprint(u.Out, code)
	}
	u.Text(message)
	if u.color {
		fmt.Fprint(u.Out, colorReset)
	}
}

// Semantic terminal messages keep meaning visible with color while retaining
// identical plain text in redirects, logs and terminals that disable color.
func (u *UI) Info(message string)    { u.tone(colorInfo, message) }
func (u *UI) Success(message string) { u.tone(colorSuccess, message) }
func (u *UI) Warning(message string) { u.tone(colorWarning, message) }
func (u *UI) Failure(message string) { u.tone(colorFailure, message) }

// Header renders a compact product identity that remains readable in narrow
// SSH terminals and degrades to plain text when color is unavailable.
func (u *UI) Header(title, subtitle string) {
	fmt.Fprintln(u.Out)
	if u.color {
		fmt.Fprint(u.Out, "\x1b[36;1m")
	}
	u.Text(title)
	if u.color {
		fmt.Fprint(u.Out, "\x1b[0;2m")
	}
	if strings.TrimSpace(subtitle) != "" {
		u.Text(subtitle)
	}
	if u.color {
		fmt.Fprint(u.Out, "\x1b[0m")
	}
	measure := u.width
	if measure > 72 {
		measure = 72
	}
	fmt.Fprintln(u.Out, strings.Repeat("─", measure))
}

func (u *UI) Section(title string) {
	u.section(title, colorHeading)
}

func (u *UI) section(title, color string) {
	fmt.Fprintln(u.Out)
	if u.color {
		fmt.Fprint(u.Out, color)
	}
	u.Text(title)
	if u.color {
		fmt.Fprint(u.Out, "\x1b[0m")
	}
	measure := u.width
	if measure > 72 {
		measure = 72
	}
	fmt.Fprintln(u.Out, strings.Repeat("─", measure))
}
func (u *UI) Field(label, value string) {
	if strings.TrimSpace(label) == "" {
		u.Text(Clean(value))
		return
	}
	u.Text(Clean(label) + ": " + Clean(value))
}

// StatusCard keeps the current state and the next useful facts together. It
// intentionally uses the same streaming/wrapping primitives as the rest of
// the UI so it remains legible in narrow and non-color SSH terminals.
func (u *UI) StatusCard(status, title string, fields []StatusField) {
	cleanStatus := strings.ToUpper(Clean(status))
	tone := colorHeading
	switch {
	case strings.Contains(cleanStatus, "FAILED"), strings.Contains(cleanStatus, "ERROR"), strings.Contains(cleanStatus, "UNHEALTHY"), strings.Contains(cleanStatus, "NOT RESPONDING"):
		tone = colorFailure
	case strings.Contains(cleanStatus, "ACTION"), strings.Contains(cleanStatus, "ATTENTION"), strings.Contains(cleanStatus, "WARNING"):
		tone = colorWarning
	case strings.Contains(cleanStatus, "READY"), strings.Contains(cleanStatus, "HEALTHY"), strings.Contains(cleanStatus, "RESPONDING"):
		tone = colorSuccess
	}
	u.section(cleanStatus+" · "+Clean(title), tone)
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		u.Field("  "+field.Label, field.Value)
	}
}
func (u *UI) Result(err error) {
	if err == nil {
		u.Success(u.T("terminal.done"))
	} else {
		message := err.Error()
		var localized interface{ Localized(i18n.Locale) string }
		if errors.As(err, &localized) {
			message = localized.Localized(u.Locale)
		}
		u.Failure(u.T("terminal.failed", Clean(message)))
		u.Warning(u.T("terminal.recovery"))
	}
}

// ReadLine deliberately performs no read-ahead: secret reads can safely switch
// to the actual terminal FD even when several answers were pasted together.
// A partial final line is cancellation, never confirmation.
func ReadLine(in io.Reader) (string, error) {
	return readLine(context.Background(), in, false)
}
func readLine(ctx context.Context, in io.Reader, raw bool) (string, error) {
	var b []byte
	var one [1]byte
	for len(b) <= 4096 {
		if ctx.Err() != nil {
			return "", ErrCanceled
		}
		if err := waitInput(ctx, in); err != nil {
			return "", err
		}
		n, err := in.Read(one[:])
		if n == 1 {
			if one[0] == 3 || one[0] == 4 {
				return "", ErrCanceled
			}
			if one[0] == '\n' || raw && one[0] == '\r' {
				return strings.TrimSuffix(string(b), "\r"), nil
			}
			if raw && (one[0] == 127 || one[0] == 8) {
				if len(b) > 0 {
					_, size := utf8.DecodeLastRune(b)
					b = b[:len(b)-size]
				}
				continue
			}
			if raw && one[0] == 21 {
				clear(b)
				b = b[:0]
				continue
			}
			b = append(b, one[0])
		}
		if err != nil {
			return "", ErrCanceled
		}
		if n == 0 {
			return "", io.ErrNoProgress
		}
	}
	return "", errors.New("terminal: input exceeds 4096 bytes")
}
func (u *UI) writePrompt(label, def string) {
	prompt := Clean(label)
	if def != "" {
		prompt += " [" + Clean(def) + "]"
	}
	prompt += ": "
	if utf8.RuneCountInString(prompt) <= u.width {
		fmt.Fprint(u.Out, prompt)
	} else {
		u.Text(strings.TrimSpace(prompt))
		fmt.Fprint(u.Out, "> ")
	}
}
func (u *UI) Ask(label, def string) (string, error) {
	u.writePrompt(label, def)
	v, err := readLine(u.Context, u.In, false)
	if !u.inputTTY {
		fmt.Fprintln(u.Out)
	}
	if err != nil {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "q" {
		return "", ErrCanceled
	}
	if v == "back" {
		return "", ErrBack
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}
func (u *UI) Choose(label string, options []string, def int) (int, error) {
	return u.choose(label, options, def, "terminal.back")
}

// ChooseRoot renders a top-level menu where 0 exits rather than navigating up.
func (u *UI) ChooseRoot(label string, options []string, def int) (int, error) {
	return u.choose(label, options, def, "terminal.exit")
}

func (u *UI) choose(label string, options []string, def int, footer string) (int, error) {
	u.Section(label)
	for i, v := range options {
		line := fmt.Sprintf("  %d  %s", i+1, v)
		if i+1 == def {
			u.Success(line)
		} else {
			u.Text(line)
		}
	}
	u.Text(u.T(footer))
	for {
		d := ""
		if def > 0 {
			d = strconv.Itoa(def)
		}
		v, err := u.Ask(u.T("terminal.choice"), d)
		if err != nil {
			return 0, err
		}
		v = Digits(v)
		if v == "0" {
			return 0, ErrBack
		}
		n, e := strconv.Atoi(v)
		if e == nil && n >= 1 && n <= len(options) {
			return n, nil
		}
		u.Text(u.T("terminal.invalid", len(options)))
	}
}
func (u *UI) Confirm(label string) (bool, error) {
	return u.ConfirmDefault(label, false)
}

// ConfirmDefault is for reversible or already-reviewed choices where Enter
// has a meaningful recommendation. Destructive actions continue using
// Confirm, whose default is always no.
func (u *UI) ConfirmDefault(label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		u.writePrompt(label+" ["+hint+"]", "")
		v, err := readLine(u.Context, u.In, false)
		if !u.inputTTY {
			fmt.Fprintln(u.Out)
		}
		if err != nil {
			return false, err
		}
		v = strings.TrimSpace(v)
		if v == "q" {
			return false, ErrCanceled
		}
		if v == "back" {
			return false, ErrBack
		}
		if v == "" {
			return def, nil
		}
		switch strings.ToLower(v) {
		case "yes", "y":
			return true, nil
		case "no", "n":
			return false, nil
		}
		u.Warning(u.T("terminal.yes_no"))
	}
}
func (u *UI) Secret(label string) (string, error) {
	if f, ok := u.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		state, err := term.MakeRaw(int(f.Fd()))
		if err != nil {
			return "", err
		}
		defer func() { _ = term.Restore(int(f.Fd()), state); fmt.Fprintln(u.Out) }()
		// Raw input also disables terminal output newline translation. Keep
		// the hidden prompt at column zero without enabling echo again.
		var labelText strings.Builder
		labelUI := *u
		labelUI.Out = &labelText
		labelUI.writePrompt(label, "")
		fmt.Fprint(u.Out, strings.ReplaceAll(labelText.String(), "\n", "\r\n"))
		return readLine(u.Context, u.In, true)
	}
	u.writePrompt(label, "")
	value, err := readLine(u.Context, u.In, false)
	fmt.Fprintln(u.Out)
	return value, err
}
