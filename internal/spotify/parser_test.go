package spotify

import "testing"

func TestParsePlaylistID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "raw ID with query param from prompt",
			input:    "6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "quoted input",
			input:    "'6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660'",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "plain valid ID",
			input:    "6UUCMxk575eDTwSWa0qQhB",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "web url with query params",
			input:    "https://open.spotify.com/playlist/6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660&nd=1",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "spotify uri",
			input:    "spotify:playlist:6UUCMxk575eDTwSWa0qQhB",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "spotify uri with query param",
			input:    "spotify:playlist:6UUCMxk575eDTwSWa0qQhB?si=something",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "invalid characters",
			input:    "invalid!!@@##$$",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "open.spotify.com url uppercase",
			input:    "https://OPEN.SPOTIFY.COM/playlist/6UUCMxk575eDTwSWa0qQhB",
			expected: "6UUCMxk575eDTwSWa0qQhB",
			wantErr:  false,
		},
		{
			name:     "arbitrary domain url with playlist path",
			input:    "https://attacker.com/playlist/6UUCMxk575eDTwSWa0qQhB",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "other domain with open.spotify.com as subdomain prefix",
			input:    "https://open.spotify.com.attacker.com/playlist/6UUCMxk575eDTwSWa0qQhB",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "too short ID",
			input:    "abc123",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePlaylistID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePlaylistID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("ParsePlaylistID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatPlaylistURI(t *testing.T) {
	id := "6UUCMxk575eDTwSWa0qQhB"
	expected := "spotify:playlist:6UUCMxk575eDTwSWa0qQhB"
	if got := FormatPlaylistURI(id); got != expected {
		t.Errorf("FormatPlaylistURI(%q) = %q, want %q", id, got, expected)
	}
}
