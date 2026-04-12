package termscroll

import "testing"

func TestANSIStripper(t *testing.T) {
	s := newANSIStripper()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "CSI color",
			input: "\x1b[31mred\x1b[0m",
			want:  "red",
		},
		{
			name:  "CSI cursor move",
			input: "\x1b[2Jhello",
			want:  "hello",
		},
		{
			name:  "OSC with ST",
			input: "\x1b]0;window title\x1b\\hello",
			want:  "hello",
		},
		{
			name:  "OSC with BEL",
			input: "\x1b]0;window title\x07hello",
			want:  "hello",
		},
		{
			name:  "mixed sequences",
			input: "\x1b[1m\x1b[31mbold red\x1b[0m normal \x1b]0;title\x07end",
			want:  "bold red normal end",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "single ESC other",
			input: "\x1bMreverse line feed",
			want:  "reverse line feed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Strip(tt.input)
			if got != tt.want {
				t.Errorf("Strip(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
