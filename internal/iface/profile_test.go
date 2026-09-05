package iface

import (
	"bytes"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
)

func BenchmarkInterfaceProfileGeneration(b *testing.B) {
	for _, policy := range []ProfilePolicy{ProfileRecommended, ProfileRandomized} {
		b.Run(string(policy), func(b *testing.B) {
			generator := NewProfileGenerator(nil)
			b.ReportAllocs()
			for range b.N {
				profile, err := generator.Generate(policy)
				if err != nil {
					b.Fatal(err)
				}
				if !profile.Enabled {
					b.Fatal("generated profile is disabled")
				}
			}
		})
	}
}

func deterministicProfileEntropy() io.Reader {
	pattern := make([]byte, 251)
	for i := range pattern {
		pattern[i] = byte(i)
	}
	return bytes.NewReader(bytes.Repeat(pattern, 4096))
}

func TestProfileGenerationDeterministicShape(t *testing.T) {
	plain, err := NewProfileGenerator(errorReader{}).Generate(ProfilePlain)
	if err != nil || plain != (Obfuscation{}) {
		t.Fatalf("plain profile = %+v, %v", plain, err)
	}

	recommendedA, err := NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRecommended)
	if err != nil {
		t.Fatal(err)
	}
	recommendedB, err := NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRecommended)
	if err != nil {
		t.Fatal(err)
	}
	if recommendedA != recommendedB {
		t.Fatal("same deterministic entropy produced different recommended profiles")
	}
	if recommendedA.Jc != RecommendedJc || recommendedA.Jmin != RecommendedJmin ||
		recommendedA.Jmax != RecommendedJmax || recommendedA.S1 != RecommendedS1 ||
		recommendedA.S2 != RecommendedS2 {
		t.Fatalf("recommended baseline changed: %+v", recommendedA)
	}
	for i, h := range []uint32{
		recommendedA.H1.Low(), recommendedA.H2.Low(), recommendedA.H3.Low(), recommendedA.H4.Low(),
	} {
		if h < RecommendedHeaderMin || h > RecommendedHeaderMax ||
			[]uint32{recommendedA.H1.High(), recommendedA.H2.High(), recommendedA.H3.High(), recommendedA.H4.High()}[i] != h {
			t.Fatalf("recommended H%d is not an in-policy scalar: %+v", i+1, recommendedA)
		}
	}
	if recommendedA.S3 != 0 || recommendedA.S4 != 0 || recommendedA.HeaderProtectionKey != "" ||
		!recommendedA.ContentPaddingAddition.IsZero() || recommendedA.RandomTrailers || recommendedA.DisableCookies {
		t.Fatalf("recommended profile enabled advanced/client-risk fields: %+v", recommendedA)
	}

	randomizedA, err := NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRandomized)
	if err != nil {
		t.Fatal(err)
	}
	randomizedB, err := NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRandomized)
	if err != nil {
		t.Fatal(err)
	}
	if randomizedA != randomizedB {
		t.Fatal("same deterministic entropy produced different randomized profiles")
	}
	if err := ValidateGeneratedProfile(ProfileRandomized, randomizedA); err != nil {
		t.Fatalf("randomized shape: %v (%+v)", err, randomizedA)
	}
	if randomizedA.S3 < RandomizedSMin || randomizedA.S4 < RandomizedSMin || randomizedA.HeaderProtectionKey == "" {
		t.Fatalf("randomized HPK/S3/S4 coupling missing: %+v", randomizedA)
	}
	for i, h := range []struct{ low, high uint32 }{
		{randomizedA.H1.Low(), randomizedA.H1.High()}, {randomizedA.H2.Low(), randomizedA.H2.High()},
		{randomizedA.H3.Low(), randomizedA.H3.High()}, {randomizedA.H4.Low(), randomizedA.H4.High()},
	} {
		if h.low >= h.high || h.low < RecommendedHeaderMin || h.high > RecommendedHeaderMax {
			t.Fatalf("randomized H%d range outside policy: %d-%d", i+1, h.low, h.high)
		}
	}
	if randomizedA.RandomTrailers || randomizedA.DisableCookies ||
		randomizedA.I1 != "" || randomizedA.I2 != "" || randomizedA.I3 != "" || randomizedA.I4 != "" || randomizedA.I5 != "" {
		t.Fatalf("randomized profile enabled unsafe/client-specific options: %+v", randomizedA)
	}
}

func TestGeneratedProfilesProperty(t *testing.T) {
	generator := NewProfileGenerator(cryptorand.Reader)
	var previous Obfuscation
	for i := 0; i < 10_000; i++ {
		profile, err := generator.Generate(ProfileRandomized)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if err := ValidateObfuscation(profile); err != nil {
			t.Fatalf("iteration %d invalid: %v (%+v)", i, err, profile)
		}
		if err := ValidateGeneratedProfile(ProfileRandomized, profile); err != nil {
			t.Fatalf("iteration %d outside policy: %v", i, err)
		}
		if i > 0 && profile == previous {
			t.Fatalf("iterations %d and %d were identical", i-1, i)
		}
		previous = profile
	}
}

func TestGeneratedProfileValidatorRejectsValuesOutsideConstructionPolicy(t *testing.T) {
	recommended, err := NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRecommended)
	if err != nil {
		t.Fatal(err)
	}
	recommended.H1 = awgparam.ScalarU32(generatedHeaderBands[1].low)
	recommended.H2 = awgparam.ScalarU32(generatedHeaderBands[2].low)
	recommended.H3 = awgparam.ScalarU32(generatedHeaderBands[3].low)
	recommended.H4 = awgparam.ScalarU32(generatedHeaderBands[3].high)
	if err := ValidateGeneratedProfile(ProfileRecommended, recommended); err == nil {
		t.Fatal("recommended headers outside their assigned bands were accepted")
	}

	randomized, err := NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRandomized)
	if err != nil {
		t.Fatal(err)
	}
	randomized.H1, err = awgparam.NewU32Range(generatedHeaderBands[0].low, generatedHeaderBands[0].low+randomizedHeaderMaxSpan+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGeneratedProfile(ProfileRandomized, randomized); err == nil {
		t.Fatal("randomized header span above the generator maximum was accepted")
	}

	randomized, err = NewProfileGenerator(deterministicProfileEntropy()).Generate(ProfileRandomized)
	if err != nil {
		t.Fatal(err)
	}
	randomized.ContentPaddingAddition, err = awgparam.NewU16Range(31, 41)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGeneratedProfile(ProfileRandomized, randomized); err == nil {
		t.Fatal("randomized padding outside the generator low range was accepted")
	}
}

func TestIntInclusiveExceptMapsExcludedValueWithoutRetry(t *testing.T) {
	generator := NewProfileGenerator(bytes.NewReader([]byte{56}))
	got, err := generator.intInclusiveExcept(12, 256, 68)
	if err != nil {
		t.Fatalf("single entropy draw failed: %v", err)
	}
	if got != 69 {
		t.Fatalf("excluded value mapping = %d, want 69", got)
	}
}

func TestProfileGenerationReturnsEntropyErrors(t *testing.T) {
	for _, policy := range []ProfilePolicy{ProfileRecommended, ProfileRandomized} {
		profile, err := NewProfileGenerator(errorReader{}).Generate(policy)
		if err == nil {
			t.Fatalf("%s accepted failed entropy", policy)
		}
		if profile != (Obfuscation{}) {
			t.Fatalf("%s returned partial profile after entropy failure: %+v", policy, profile)
		}
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
