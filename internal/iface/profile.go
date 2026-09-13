package iface

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// ProfilePolicy identifies a server-owned AWG profile-generation policy.
// Custom is a persisted classification, not a generatable policy.
type ProfilePolicy string

const (
	ProfilePlain       ProfilePolicy = "plain"
	ProfileRecommended ProfilePolicy = "recommended"
	ProfilePerformance ProfilePolicy = "performance"
	ProfileBalanced    ProfilePolicy = "balanced"
	ProfileResilient   ProfilePolicy = "resilient"
	ProfileSuggested   ProfilePolicy = "suggested"
	ProfileRandomized  ProfilePolicy = "randomized"
	ProfileCustom      ProfilePolicy = "custom"
)

// SuggestedMTU is the interface MTU paired with the Suggested/Expert profile.
// DNS remains the existing global client setting, whose product
// default is 1.1.1.1, 1.0.0.1.
const SuggestedMTU = 1280

// The legacy recommended values remain available for compatibility within the
// pinned upstream constraints. Headers are generated per profile because fixed
// magic headers make installations needlessly recognizable.
const (
	RecommendedJc   = 4
	RecommendedJmin = 40
	RecommendedJmax = 70
	RecommendedS1   = 15
	RecommendedS2   = 64

	RecommendedHeaderMin uint32 = 5
	RecommendedHeaderMax uint32 = 2147483647

	RandomizedJcMin   = 4
	RandomizedJcMax   = 12
	RandomizedJminMin = 20
	RandomizedJminMax = 60
	RandomizedJmaxMin = 80
	RandomizedJmaxMax = 240
	RandomizedSMin    = 12
	RandomizedSMax    = 256
	RandomizedS34Max  = 64

	randomizedPaddingLowMin       = 10
	randomizedPaddingLowMax       = 30
	randomizedPaddingSpanMin      = 10
	randomizedPaddingSpanMax      = 60
	randomizedRekeyAfterLowMin    = 90
	randomizedRekeyAfterLowMax    = 120
	randomizedRekeyAfterSpanMin   = 10
	randomizedRekeyAfterSpanMax   = 20
	randomizedRekeyTimeoutLowMin  = 3
	randomizedRekeyTimeoutLowMax  = 7
	randomizedRekeyTimeoutSpanMin = 1
	randomizedRekeyTimeoutSpanMax = 5
	randomizedRejectGapMin        = 20
	randomizedRejectLowWindow     = 30
	randomizedRejectSpanMin       = 20
	randomizedRejectSpanMax       = 60
	randomizedKeepaliveLowMin     = 5
	randomizedKeepaliveLowMax     = 12
	randomizedKeepaliveSpanMin    = 2
	randomizedKeepaliveSpanMax    = 8
	randomizedHandshakeLowMin     = 15
	randomizedHandshakeLowMax     = 25
	randomizedHandshakeSpanMin    = 5
	randomizedHandshakeSpanMax    = 20
)

type headerBand struct {
	low  uint32
	high uint32
}

// Separate bands make non-overlap a construction invariant instead of a
// probabilistic retry condition. Every band stays inside upstream's
// recommended positive signed-32-bit header domain.
var generatedHeaderBands = [4]headerBand{
	{low: 5, high: 536870911},
	{low: 536870912, high: 1073741823},
	{low: 1073741824, high: 1610612735},
	{low: 1610612736, high: 2147483647},
}

const randomizedHeaderMaxSpan uint32 = 100_000_000

// ProfileGenerator centralizes profile creation for the API, panel, and
// service. Entropy is injectable so error handling and deterministic shape can
// be tested without weakening production randomness.
type ProfileGenerator struct {
	entropy io.Reader
}

// NewProfileGenerator creates a generator backed by entropy. A nil reader
// uses crypto/rand.Reader.
func NewProfileGenerator(entropy io.Reader) *ProfileGenerator {
	if entropy == nil {
		entropy = rand.Reader
	}
	return &ProfileGenerator{entropy: entropy}
}

// Generate constructs and validates one complete policy profile. It never
// returns a partially generated profile after an entropy or validation error.
func (g *ProfileGenerator) Generate(policy ProfilePolicy) (Obfuscation, error) {
	switch policy {
	case ProfilePlain:
		return Obfuscation{}, nil
	case ProfileRecommended:
		profile, err := g.recommended()
		if err != nil {
			return Obfuscation{}, err
		}
		return validatedGenerated(policy, profile)
	case ProfilePerformance:
		profile, err := g.performance()
		if err != nil {
			return Obfuscation{}, err
		}
		return validatedGenerated(policy, profile)
	case ProfileBalanced:
		profile, err := g.balanced()
		if err != nil {
			return Obfuscation{}, err
		}
		return validatedGenerated(policy, profile)
	case ProfileResilient:
		profile, err := g.resilient()
		if err != nil {
			return Obfuscation{}, err
		}
		return validatedGenerated(policy, profile)
	case ProfileSuggested:
		profile, err := g.suggested()
		if err != nil {
			return Obfuscation{}, err
		}
		return validatedGenerated(policy, profile)
	case ProfileRandomized:
		profile, err := g.randomized()
		if err != nil {
			return Obfuscation{}, err
		}
		return validatedGenerated(policy, profile)
	default:
		return Obfuscation{}, domain.E(domain.CodeParamConstraint, "profile policy %q cannot be generated", policy)
	}
}

type operationalProfileValues struct {
	jc, jmin, jmax     int
	s1, s2, s3, s4     int
	padding            [2]uint16
	rekeyAfter         [2]uint16
	rekeyTimeout       [2]uint16
	rejectAfter        [2]uint16
	keepalive          [2]uint16
	maxHandshake       [2]uint16
	headerRangeMaxSpan uint32
}

func (g *ProfileGenerator) performance() (Obfuscation, error) {
	return g.operational(operationalProfileValues{
		jc: 4, jmin: 10, jmax: 40, s1: 18, s2: 31, s3: 16, s4: 20,
		padding: [2]uint16{5, 35}, rekeyAfter: [2]uint16{110, 130},
		rekeyTimeout: [2]uint16{4, 7}, rejectAfter: [2]uint16{170, 200},
		keepalive: [2]uint16{8, 15}, maxHandshake: [2]uint16{12, 18},
	})
}

func (g *ProfileGenerator) balanced() (Obfuscation, error) {
	return g.operational(operationalProfileValues{
		jc: 6, jmin: 20, jmax: 80, s1: 32, s2: 64, s3: 24, s4: 32,
		padding: [2]uint16{10, 70}, rekeyAfter: [2]uint16{100, 125},
		rekeyTimeout: [2]uint16{3, 7}, rejectAfter: [2]uint16{165, 210},
		keepalive: [2]uint16{6, 15}, maxHandshake: [2]uint16{15, 22},
		headerRangeMaxSpan: 25_000_000,
	})
}

func (g *ProfileGenerator) resilient() (Obfuscation, error) {
	return g.operational(operationalProfileValues{
		jc: 12, jmin: 40, jmax: 180, s1: 96, s2: 180, s3: 48, s4: 64,
		padding: [2]uint16{30, 180}, rekeyAfter: [2]uint16{90, 120},
		rekeyTimeout: [2]uint16{3, 7}, rejectAfter: [2]uint16{180, 260},
		keepalive: [2]uint16{5, 15}, maxHandshake: [2]uint16{20, 32},
		headerRangeMaxSpan: randomizedHeaderMaxSpan,
	})
}

func (g *ProfileGenerator) suggested() (Obfuscation, error) {
	profile := Obfuscation{
		Enabled: true, Jc: 5, Jmin: 10, Jmax: 50,
		S1: 21, S2: 28, S3: 53, S4: 12,
		H1: awgparam.ScalarU32(1), H2: awgparam.ScalarU32(2),
		H3: awgparam.ScalarU32(3), H4: awgparam.ScalarU32(4),
		RandomTrailers: true, DisableCookies: true,
	}
	var err error
	for value, bounds := range map[*awgparam.U16Range][2]uint16{
		&profile.ContentPaddingAddition: {10, 100},
		&profile.RekeyAfterTime:         {100, 120},
		&profile.RekeyTimeout:           {3, 7},
		&profile.RejectAfterTime:        {150, 180},
		&profile.KeepaliveTimeout:       {5, 15},
		&profile.MaxHandshakeAttempts:   {15, 20},
	} {
		*value, err = awgparam.NewU16Range(bounds[0], bounds[1])
		if err != nil {
			return Obfuscation{}, err
		}
	}
	if err := g.setHeaderProtectionKey(&profile); err != nil {
		return Obfuscation{}, err
	}
	return profile, nil
}

func (g *ProfileGenerator) operational(values operationalProfileValues) (Obfuscation, error) {
	profile := Obfuscation{
		Enabled: true, Jc: values.jc, Jmin: values.jmin, Jmax: values.jmax,
		S1: values.s1, S2: values.s2, S3: values.s3, S4: values.s4,
		RandomTrailers: true, DisableCookies: true,
	}
	headers := [4]*awgparam.U32Range{&profile.H1, &profile.H2, &profile.H3, &profile.H4}
	for i, band := range generatedHeaderBands {
		if values.headerRangeMaxSpan == 0 {
			value, err := g.uint32Inclusive(band.low, band.high)
			if err != nil {
				return Obfuscation{}, fmt.Errorf("generate operational H%d: %w", i+1, err)
			}
			*headers[i] = awgparam.ScalarU32(value)
			continue
		}
		low, err := g.uint32Inclusive(band.low, band.high-values.headerRangeMaxSpan)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("generate operational H%d low: %w", i+1, err)
		}
		span, err := g.uint32Inclusive(1, values.headerRangeMaxSpan)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("generate operational H%d span: %w", i+1, err)
		}
		*headers[i], err = awgparam.NewU32Range(low, low+span)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("construct operational H%d: %w", i+1, err)
		}
	}
	var err error
	for value, bounds := range map[*awgparam.U16Range][2]uint16{
		&profile.ContentPaddingAddition: values.padding,
		&profile.RekeyAfterTime:         values.rekeyAfter,
		&profile.RekeyTimeout:           values.rekeyTimeout,
		&profile.RejectAfterTime:        values.rejectAfter,
		&profile.KeepaliveTimeout:       values.keepalive,
		&profile.MaxHandshakeAttempts:   values.maxHandshake,
	} {
		*value, err = awgparam.NewU16Range(bounds[0], bounds[1])
		if err != nil {
			return Obfuscation{}, err
		}
	}
	if err := g.setHeaderProtectionKey(&profile); err != nil {
		return Obfuscation{}, err
	}
	return profile, nil
}

func (g *ProfileGenerator) setHeaderProtectionKey(profile *Obfuscation) error {
	key := make([]byte, 32)
	if _, err := io.ReadFull(g.entropy, key); err != nil {
		return fmt.Errorf("generate header protection key: %w", err)
	}
	profile.HeaderProtectionKey = base64.StdEncoding.EncodeToString(key)
	return nil
}

func (g *ProfileGenerator) recommended() (Obfuscation, error) {
	profile := Obfuscation{
		Enabled: true,
		Jc:      RecommendedJc,
		Jmin:    RecommendedJmin,
		Jmax:    RecommendedJmax,
		S1:      RecommendedS1,
		S2:      RecommendedS2,
	}
	headers := [4]*awgparam.U32Range{&profile.H1, &profile.H2, &profile.H3, &profile.H4}
	for i, band := range generatedHeaderBands {
		value, err := g.uint32Inclusive(band.low, band.high)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("generate recommended H%d: %w", i+1, err)
		}
		*headers[i] = awgparam.ScalarU32(value)
	}
	return profile, nil
}

func (g *ProfileGenerator) randomized() (Obfuscation, error) {
	jc, err := g.intInclusive(RandomizedJcMin, RandomizedJcMax)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate Jc: %w", err)
	}
	jmin, err := g.intInclusive(RandomizedJminMin, RandomizedJminMax)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate Jmin: %w", err)
	}
	jmax, err := g.intInclusive(RandomizedJmaxMin, RandomizedJmaxMax)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate Jmax: %w", err)
	}
	s1, err := g.intInclusive(RandomizedSMin, RandomizedSMax)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate S1: %w", err)
	}
	s2, err := g.intInclusiveExcept(RandomizedSMin, RandomizedSMax, s1+56)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate S2: %w", err)
	}
	s3, err := g.intInclusive(RandomizedSMin, RandomizedS34Max)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate S3: %w", err)
	}
	s4, err := g.intInclusive(RandomizedSMin, RandomizedS34Max)
	if err != nil {
		return Obfuscation{}, fmt.Errorf("generate S4: %w", err)
	}

	profile := Obfuscation{Enabled: true, Jc: jc, Jmin: jmin, Jmax: jmax, S1: s1, S2: s2, S3: s3, S4: s4}
	profile.RandomTrailers = true
	profile.DisableCookies = true
	headers := [4]*awgparam.U32Range{&profile.H1, &profile.H2, &profile.H3, &profile.H4}
	for i, band := range generatedHeaderBands {
		low, err := g.uint32Inclusive(band.low, band.high-randomizedHeaderMaxSpan)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("generate randomized H%d low: %w", i+1, err)
		}
		span, err := g.uint32Inclusive(1, randomizedHeaderMaxSpan)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("generate randomized H%d span: %w", i+1, err)
		}
		*headers[i], err = awgparam.NewU32Range(low, low+span)
		if err != nil {
			return Obfuscation{}, fmt.Errorf("construct randomized H%d: %w", i+1, err)
		}
	}

	if err := g.setHeaderProtectionKey(&profile); err != nil {
		return Obfuscation{}, err
	}

	if profile.ContentPaddingAddition, err = g.u16Range(randomizedPaddingLowMin, randomizedPaddingLowMax, randomizedPaddingSpanMin, randomizedPaddingSpanMax); err != nil {
		return Obfuscation{}, fmt.Errorf("generate content padding: %w", err)
	}
	if profile.RekeyAfterTime, err = g.u16Range(randomizedRekeyAfterLowMin, randomizedRekeyAfterLowMax, randomizedRekeyAfterSpanMin, randomizedRekeyAfterSpanMax); err != nil {
		return Obfuscation{}, fmt.Errorf("generate rekey-after time: %w", err)
	}
	if profile.RekeyTimeout, err = g.u16Range(randomizedRekeyTimeoutLowMin, randomizedRekeyTimeoutLowMax, randomizedRekeyTimeoutSpanMin, randomizedRekeyTimeoutSpanMax); err != nil {
		return Obfuscation{}, fmt.Errorf("generate rekey timeout: %w", err)
	}
	rejectLowMin := int(profile.RekeyAfterTime.High()) + randomizedRejectGapMin
	if profile.RejectAfterTime, err = g.u16Range(rejectLowMin, rejectLowMin+randomizedRejectLowWindow, randomizedRejectSpanMin, randomizedRejectSpanMax); err != nil {
		return Obfuscation{}, fmt.Errorf("generate reject-after time: %w", err)
	}
	if profile.KeepaliveTimeout, err = g.u16Range(randomizedKeepaliveLowMin, randomizedKeepaliveLowMax, randomizedKeepaliveSpanMin, randomizedKeepaliveSpanMax); err != nil {
		return Obfuscation{}, fmt.Errorf("generate keepalive timeout: %w", err)
	}
	if profile.MaxHandshakeAttempts, err = g.u16Range(randomizedHandshakeLowMin, randomizedHandshakeLowMax, randomizedHandshakeSpanMin, randomizedHandshakeSpanMax); err != nil {
		return Obfuscation{}, fmt.Errorf("generate max handshake attempts: %w", err)
	}
	return profile, nil
}

func (g *ProfileGenerator) intInclusive(minimum, maximum int) (int, error) {
	value, err := rand.Int(g.entropy, big.NewInt(int64(maximum-minimum+1)))
	if err != nil {
		return 0, err
	}
	return minimum + int(value.Int64()), nil
}

// intInclusiveExcept samples uniformly from the inclusive range while
// excluding one value. Mapping the excluded slot avoids an unbounded retry
// loop when the entropy source is deterministic or adversarially faulty.
func (g *ProfileGenerator) intInclusiveExcept(minimum, maximum, excluded int) (int, error) {
	if excluded < minimum || excluded > maximum {
		return g.intInclusive(minimum, maximum)
	}
	if minimum >= maximum {
		return 0, fmt.Errorf("cannot exclude the only value in an integer range")
	}
	value, err := g.intInclusive(minimum, maximum-1)
	if err != nil {
		return 0, err
	}
	if value >= excluded {
		value++
	}
	return value, nil
}

func (g *ProfileGenerator) uint32Inclusive(minimum, maximum uint32) (uint32, error) {
	value, err := rand.Int(g.entropy, new(big.Int).SetUint64(uint64(maximum-minimum)+1))
	if err != nil {
		return 0, err
	}
	return minimum + uint32(value.Uint64()), nil
}

func (g *ProfileGenerator) u16Range(lowMin, lowMax, spanMin, spanMax int) (awgparam.U16Range, error) {
	low, err := g.intInclusive(lowMin, lowMax)
	if err != nil {
		return awgparam.U16Range{}, err
	}
	span, err := g.intInclusive(spanMin, spanMax)
	if err != nil {
		return awgparam.U16Range{}, err
	}
	return awgparam.NewU16Range(uint16(low), uint16(low+span))
}

func validatedGenerated(policy ProfilePolicy, profile Obfuscation) (Obfuscation, error) {
	if err := ValidateGeneratedProfile(policy, profile); err != nil {
		return Obfuscation{}, err
	}
	return profile, nil
}

// IsGeneratedProfilePolicy reports whether policy is constructed and sealed by
// the server rather than accepted as explicit custom values.
func IsGeneratedProfilePolicy(policy ProfilePolicy) bool {
	switch policy {
	case ProfileRecommended, ProfilePerformance, ProfileBalanced, ProfileResilient, ProfileSuggested, ProfileRandomized:
		return true
	default:
		return false
	}
}

// ValidateGeneratedProfile enforces the narrower product policy on top of the
// general pinned-runtime constraints. It is also used when a server-generated
// panel preview is submitted for persistence.
func ValidateGeneratedProfile(policy ProfilePolicy, profile Obfuscation) error {
	if err := ValidateObfuscation(profile); err != nil {
		return err
	}
	switch policy {
	case ProfilePlain:
		if profile != (Obfuscation{}) {
			return domain.E(domain.CodeParamConstraint, "plain profile must not contain AWG parameters")
		}
		return nil
	case ProfileRecommended:
		if profile.Jc != RecommendedJc || profile.Jmin != RecommendedJmin || profile.Jmax != RecommendedJmax ||
			profile.S1 != RecommendedS1 || profile.S2 != RecommendedS2 {
			return domain.E(domain.CodeParamConstraint, "recommended profile baseline does not match policy")
		}
		if profile.S3 != 0 || profile.S4 != 0 || profile.HeaderProtectionKey != "" || hasAdvancedGeneratedFields(profile) {
			return domain.E(domain.CodeParamConstraint, "recommended profile contains advanced or client-specific parameters")
		}
		for i, header := range []awgparam.U32Range{profile.H1, profile.H2, profile.H3, profile.H4} {
			band := generatedHeaderBands[i]
			if header.Low() != header.High() || header.Low() < band.low || header.High() > band.high {
				return domain.E(domain.CodeParamConstraint, "recommended H%d must be a scalar in its assigned band %d..%d", i+1, band.low, band.high)
			}
		}
		return nil
	case ProfilePerformance, ProfileBalanced, ProfileResilient, ProfileSuggested:
		return validateOperationalGeneratedProfile(policy, profile)
	case ProfileRandomized:
		if profile.Jc < RandomizedJcMin || profile.Jc > RandomizedJcMax ||
			profile.Jmin < RandomizedJminMin || profile.Jmin > RandomizedJminMax ||
			profile.Jmax < RandomizedJmaxMin || profile.Jmax > RandomizedJmaxMax {
			return domain.E(domain.CodeParamConstraint, "randomized junk parameters are outside policy")
		}
		if profile.S1 < RandomizedSMin || profile.S1 > RandomizedSMax ||
			profile.S2 < RandomizedSMin || profile.S2 > RandomizedSMax ||
			profile.S3 < RandomizedSMin || profile.S3 > RandomizedS34Max ||
			profile.S4 < RandomizedSMin || profile.S4 > RandomizedS34Max || profile.HeaderProtectionKey == "" {
			return domain.E(domain.CodeParamConstraint, "randomized padding/HPK parameters are outside policy")
		}
		for i, header := range []awgparam.U32Range{profile.H1, profile.H2, profile.H3, profile.H4} {
			band := generatedHeaderBands[i]
			if header.Low() >= header.High() || header.Low() < band.low || header.High() > band.high ||
				header.High()-header.Low() > randomizedHeaderMaxSpan {
				return domain.E(domain.CodeParamConstraint, "randomized H%d range is outside its non-overlapping policy band", i+1)
			}
		}
		if profile.I1 != "" || profile.I2 != "" || profile.I3 != "" || profile.I4 != "" || profile.I5 != "" ||
			!profile.RandomTrailers || !profile.DisableCookies {
			return domain.E(domain.CodeParamConstraint, "randomized profile advanced flags/signatures do not match policy")
		}
		rejectLowMin := int(profile.RekeyAfterTime.High()) + randomizedRejectGapMin
		if !validGeneratedRangeShape(profile.ContentPaddingAddition, randomizedPaddingLowMin, randomizedPaddingLowMax, randomizedPaddingSpanMin, randomizedPaddingSpanMax) ||
			!validGeneratedRangeShape(profile.RekeyAfterTime, randomizedRekeyAfterLowMin, randomizedRekeyAfterLowMax, randomizedRekeyAfterSpanMin, randomizedRekeyAfterSpanMax) ||
			!validGeneratedRangeShape(profile.RekeyTimeout, randomizedRekeyTimeoutLowMin, randomizedRekeyTimeoutLowMax, randomizedRekeyTimeoutSpanMin, randomizedRekeyTimeoutSpanMax) ||
			!validGeneratedRangeShape(profile.RejectAfterTime, rejectLowMin, rejectLowMin+randomizedRejectLowWindow, randomizedRejectSpanMin, randomizedRejectSpanMax) ||
			!validGeneratedRangeShape(profile.KeepaliveTimeout, randomizedKeepaliveLowMin, randomizedKeepaliveLowMax, randomizedKeepaliveSpanMin, randomizedKeepaliveSpanMax) ||
			!validGeneratedRangeShape(profile.MaxHandshakeAttempts, randomizedHandshakeLowMin, randomizedHandshakeLowMax, randomizedHandshakeSpanMin, randomizedHandshakeSpanMax) {
			return domain.E(domain.CodeParamConstraint, "randomized timer ranges are outside policy")
		}
		return nil
	default:
		return domain.E(domain.CodeParamConstraint, "profile policy %q is not generated", policy)
	}
}

func validateOperationalGeneratedProfile(policy ProfilePolicy, profile Obfuscation) error {
	if profile.I1 != "" || profile.I2 != "" || profile.I3 != "" || profile.I4 != "" || profile.I5 != "" ||
		!profile.RandomTrailers || !profile.DisableCookies || profile.HeaderProtectionKey == "" ||
		profile.ContentPaddingAddition.IsZero() || profile.RekeyAfterTime.IsZero() ||
		profile.RekeyTimeout.IsZero() || profile.RejectAfterTime.IsZero() ||
		profile.KeepaliveTimeout.IsZero() || profile.MaxHandshakeAttempts.IsZero() {
		return domain.E(domain.CodeParamConstraint, "%s profile advanced fields/signatures do not match policy", policy)
	}
	exactRanges := func(padding, rekeyAfter, rekeyTimeout, rejectAfter, keepalive, handshake string) bool {
		return profile.ContentPaddingAddition.String() == padding && profile.RekeyAfterTime.String() == rekeyAfter &&
			profile.RekeyTimeout.String() == rekeyTimeout && profile.RejectAfterTime.String() == rejectAfter &&
			profile.KeepaliveTimeout.String() == keepalive && profile.MaxHandshakeAttempts.String() == handshake
	}
	switch policy {
	case ProfilePerformance:
		if profile.Jc != 4 || profile.Jmin != 10 || profile.Jmax != 40 ||
			profile.S1 != 18 || profile.S2 != 31 || profile.S3 != 16 || profile.S4 != 20 ||
			!exactRanges("5-35", "110-130", "4-7", "170-200", "8-15", "12-18") ||
			!generatedHeadersMatch(profile, false, 0) {
			return domain.E(domain.CodeParamConstraint, "performance profile does not match policy")
		}
	case ProfileBalanced:
		if profile.Jc != 6 || profile.Jmin != 20 || profile.Jmax != 80 ||
			profile.S1 != 32 || profile.S2 != 64 || profile.S3 != 24 || profile.S4 != 32 ||
			!exactRanges("10-70", "100-125", "3-7", "165-210", "6-15", "15-22") ||
			!generatedHeadersMatch(profile, true, 25_000_000) {
			return domain.E(domain.CodeParamConstraint, "balanced profile does not match policy")
		}
	case ProfileResilient:
		if profile.Jc != 12 || profile.Jmin != 40 || profile.Jmax != 180 ||
			profile.S1 != 96 || profile.S2 != 180 || profile.S3 != 48 || profile.S4 != 64 ||
			!exactRanges("30-180", "90-120", "3-7", "180-260", "5-15", "20-32") ||
			!generatedHeadersMatch(profile, true, randomizedHeaderMaxSpan) {
			return domain.E(domain.CodeParamConstraint, "resilient profile does not match policy")
		}
	case ProfileSuggested:
		if profile.Jc != 5 || profile.Jmin != 10 || profile.Jmax != 50 ||
			profile.S1 != 21 || profile.S2 != 28 || profile.S3 != 53 || profile.S4 != 12 ||
			profile.H1 != awgparam.ScalarU32(1) || profile.H2 != awgparam.ScalarU32(2) ||
			profile.H3 != awgparam.ScalarU32(3) || profile.H4 != awgparam.ScalarU32(4) ||
			!exactRanges("10-100", "100-120", "3-7", "150-180", "5-15", "15-20") {
			return domain.E(domain.CodeParamConstraint, "suggested profile does not match policy")
		}
	default:
		return domain.E(domain.CodeParamConstraint, "profile policy %q is not operational", policy)
	}
	return nil
}

func generatedHeadersMatch(profile Obfuscation, ranges bool, maxSpan uint32) bool {
	for i, header := range []awgparam.U32Range{profile.H1, profile.H2, profile.H3, profile.H4} {
		band := generatedHeaderBands[i]
		if header.Low() < band.low || header.High() > band.high {
			return false
		}
		if ranges {
			if header.Low() >= header.High() || header.High()-header.Low() > maxSpan {
				return false
			}
		} else if header.Low() != header.High() {
			return false
		}
	}
	return true
}

func hasAdvancedGeneratedFields(profile Obfuscation) bool {
	return profile.I1 != "" || profile.I2 != "" || profile.I3 != "" || profile.I4 != "" || profile.I5 != "" ||
		!profile.ContentPaddingAddition.IsZero() || !profile.RekeyAfterTime.IsZero() ||
		!profile.RekeyTimeout.IsZero() || !profile.RejectAfterTime.IsZero() ||
		!profile.KeepaliveTimeout.IsZero() || !profile.MaxHandshakeAttempts.IsZero() ||
		profile.RandomTrailers || profile.DisableCookies
}

func validGeneratedRangeShape(value awgparam.U16Range, lowMinimum, lowMaximum, spanMinimum, spanMaximum int) bool {
	if value.IsZero() || value.Low() >= value.High() {
		return false
	}
	low := int(value.Low())
	span := int(value.High() - value.Low())
	return low >= lowMinimum && low <= lowMaximum && span >= spanMinimum && span <= spanMaximum
}
