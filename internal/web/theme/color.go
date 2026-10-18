package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RGB is a linear-free sRGB color with components in 0..1.
type RGB struct{ R, G, B float64 }

// ParseColor understands the color forms tweakcn exports: #hex, oklch(),
// hsl() and rgb(). Alpha is ignored; contrast checks use opaque tokens.
func ParseColor(v string) (RGB, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch {
	case strings.HasPrefix(v, "#"):
		return parseHex(v)
	case strings.HasPrefix(v, "oklch("):
		f, err := colorArgs(v, "oklch(")
		if err != nil {
			return RGB{}, err
		}
		return oklchToRGB(f[0], f[1], f[2]), nil
	case strings.HasPrefix(v, "hsl("):
		f, err := colorArgs(v, "hsl(")
		if err != nil {
			return RGB{}, err
		}
		return hslToRGB(f[0], f[1]/100, f[2]/100), nil
	case strings.HasPrefix(v, "rgb("):
		f, err := colorArgs(v, "rgb(")
		if err != nil {
			return RGB{}, err
		}
		return RGB{f[0] / 255, f[1] / 255, f[2] / 255}, nil
	}
	return RGB{}, fmt.Errorf("unsupported color %q", v)
}

func parseHex(v string) (RGB, error) {
	h := strings.TrimPrefix(v, "#")
	if len(h) == 3 || len(h) == 4 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) == 8 {
		h = h[:6]
	}
	if len(h) != 6 {
		return RGB{}, fmt.Errorf("bad hex %q", v)
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return RGB{}, err
	}
	return RGB{float64(n>>16&0xff) / 255, float64(n>>8&0xff) / 255, float64(n&0xff) / 255}, nil
}

// colorArgs returns the first three numeric arguments; a trailing percent
// on lightness is normalized for oklch, and "/ alpha" is dropped.
func colorArgs(v, prefix string) ([3]float64, error) {
	var out [3]float64
	body := strings.TrimSuffix(strings.TrimPrefix(v, prefix), ")")
	if i := strings.Index(body, "/"); i >= 0 {
		body = body[:i]
	}
	parts := strings.Fields(strings.ReplaceAll(body, ",", " "))
	if len(parts) < 3 {
		return out, fmt.Errorf("bad color %q", v)
	}
	for i := 0; i < 3; i++ {
		p := strings.TrimSuffix(strings.TrimSuffix(parts[i], "deg"), "%")
		f, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return out, fmt.Errorf("bad color %q", v)
		}
		if i == 0 && prefix == "oklch(" && strings.HasSuffix(parts[i], "%") {
			f /= 100
		}
		out[i] = f
	}
	return out, nil
}

func oklchToRGB(l, c, h float64) RGB {
	a, b := c*math.Cos(h*math.Pi/180), c*math.Sin(h*math.Pi/180)
	l_ := l + 0.3963377774*a + 0.2158037573*b
	m_ := l - 0.1055613458*a - 0.0638541728*b
	s_ := l - 0.0894841775*a - 1.2914855480*b
	L, M, S := l_*l_*l_, m_*m_*m_, s_*s_*s_
	r := 4.0767416621*L - 3.3077115913*M + 0.2309699292*S
	g := -1.2684380046*L + 2.6097574011*M - 0.3413193965*S
	bl := -0.0041960863*L - 0.7034186147*M + 1.7076147010*S
	return RGB{gammaEncode(r), gammaEncode(g), gammaEncode(bl)}
}

func gammaEncode(x float64) float64 {
	x = math.Max(0, math.Min(1, x))
	if x <= 0.0031308 {
		return 12.92 * x
	}
	return 1.055*math.Pow(x, 1/2.4) - 0.055
}

func hslToRGB(h, s, l float64) RGB {
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
	}
	return RGB{f(0), f(8), f(4)}
}

func (c RGB) luminance() float64 {
	lin := func(x float64) float64 {
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// Contrast is the WCAG 2 contrast ratio of two colors.
func Contrast(a, b RGB) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Hex formats c as #rrggbb.
func (c RGB) Hex() string {
	to := func(x float64) int { return int(math.Round(math.Max(0, math.Min(1, x)) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", to(c.R), to(c.G), to(c.B))
}

// Mix blends c toward t by amount 0..1 in sRGB.
func (c RGB) Mix(t RGB, amount float64) RGB {
	return RGB{c.R + (t.R-c.R)*amount, c.G + (t.G-c.G)*amount, c.B + (t.B-c.B)*amount}
}
