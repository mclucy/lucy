package cas_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cas"
)

func digestText(content []byte) (cas.Digest, string) {
	sum := sha256.Sum256(content)
	return cas.Digest(sum), "sha256:" + hex.EncodeToString(sum[:])
}

func TestParseDigestAcceptsCanonicalTextOnly(t *testing.T) {
	t.Parallel()
	digest, text := digestText([]byte("canonical digest"))
	tests := []struct {
		name  string
		value string
	}{
		{name: "uppercase hex", value: "sha256:" + strings.ToUpper(hex.EncodeToString(digest[:]))},
		{name: "wrong algorithm", value: "sha1:" + hex.EncodeToString(digest[:])},
		{name: "missing prefix", value: hex.EncodeToString(digest[:])},
		{name: "short", value: text[:len(text)-1]},
		{name: "long", value: text + "0"},
		{name: "non-hex", value: "sha256:" + string(bytes.Repeat([]byte("g"), sha256.Size*2))},
		{name: "empty", value: ""},
		{name: "newline", value: text + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := cas.ParseDigest(test.value); err == nil {
				t.Fatalf("ParseDigest(%q) accepted a non-canonical digest", test.value)
			}
		})
	}
}

func TestDigestTextCodecRoundTrips(t *testing.T) {
	t.Parallel()
	digest, text := digestText([]byte("round-tripped digest"))
	if got := digest.String(); got != text {
		t.Fatalf("String() = %q, want %q", got, text)
	}
	if got := digest.Hex(); got != hex.EncodeToString(digest[:]) {
		t.Fatalf("Hex() = %q, want the bare digest", got)
	}

	marshaled, err := digest.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(marshaled) != text {
		t.Fatalf("MarshalText() = %q, want %q", marshaled, text)
	}

	var decoded cas.Digest
	if err := decoded.UnmarshalText([]byte(text)); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}
	if decoded != digest {
		t.Fatalf("UnmarshalText produced %s, want %s", decoded, digest)
	}

	encoded, err := json.Marshal(digest)
	if err != nil {
		t.Fatalf("marshal digest as JSON: %v", err)
	}
	quoted, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal digest text: %v", err)
	}
	if !bytes.Equal(encoded, quoted) {
		t.Fatalf("JSON digest = %s, want %s", encoded, quoted)
	}
	var fromJSON cas.Digest
	if err := json.Unmarshal(encoded, &fromJSON); err != nil {
		t.Fatalf("unmarshal digest JSON: %v", err)
	}
	if fromJSON != digest {
		t.Fatalf("JSON round trip produced %s, want %s", fromJSON, digest)
	}
}

func TestDigestUnmarshalTextLeavesReceiverOnError(t *testing.T) {
	t.Parallel()
	original, _ := digestText([]byte("original digest"))
	invalid := []string{
		"",
		"sha256:",
		"sha256:" + string(bytes.Repeat([]byte("x"), sha256.Size*2)),
		"SHA256:" + hex.EncodeToString(original[:]),
	}
	for _, value := range invalid {
		digest := original
		if err := digest.UnmarshalText([]byte(value)); err == nil {
			t.Fatalf("UnmarshalText(%q) accepted invalid text", value)
		}
		if digest != original {
			t.Fatalf("UnmarshalText(%q) changed the receiver to %s", value, digest)
		}
	}
}

func FuzzDigestText(f *testing.F) {
	_, valid := digestText([]byte("fuzz seed"))
	f.Add(valid)
	f.Add("")
	f.Add("sha256:")
	f.Add("SHA256:" + valid[7:])
	f.Add(valid + "\n")
	f.Fuzz(func(t *testing.T, text string) {
		digest, err := cas.ParseDigest(text)
		if err != nil {
			return
		}
		if got := digest.String(); got != text {
			t.Fatalf("ParseDigest(%q) round-tripped to %q", text, got)
		}
		marshaled, err := digest.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q): %v", text, err)
		}
		if string(marshaled) != text {
			t.Fatalf("MarshalText(%q) = %q", text, marshaled)
		}
	})
}
