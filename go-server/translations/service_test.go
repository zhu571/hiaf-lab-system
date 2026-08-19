package translations

import "testing"

func TestDetectLocaleAndHash(t *testing.T) {
	if DetectLocale("真空达到 5e-6 Pa") != "zh" || DetectLocale("vacuum reached 5e-6 Pa") != "en" || DetectLocale("RF 匹配 passed") != "mixed" {
		t.Fatal("locale detection")
	}
	if Hash("x") == Hash("y") || len(Hash("x")) != 64 {
		t.Fatal("hash")
	}
}

func TestProtectedTerms(t *testing.T) {
	terms := ProtectedTerms("E5063A at 5e-6 Pa, PV:RF:POWER")
	for _, want := range []string{"E5063A", "5e-6 Pa", "PV:RF:POWER"} {
		found := false
		for _, got := range terms {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q in %#v", want, terms)
		}
	}
}
