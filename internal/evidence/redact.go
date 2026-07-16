package evidence

import "regexp"

// secretPatterns matches common credential shapes so no secret leaks into an
// evidence record even if a caller mistakenly passes one. This is a backstop,
// not the primary control: tools are written to pass only content-safe summaries.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),                                                              // OpenAI-style
	regexp.MustCompile(`(?i)ghp_[A-Za-z0-9]{20,}`),                                                           // GitHub PAT
	regexp.MustCompile(`(?i)github_pat_[A-Za-z0-9_]{20,}`),                                                   // GitHub fine-grained PAT
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{20,}`),                                                             // Google API key
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                                                       // Slack token
	regexp.MustCompile(`A[KS]IA[0-9A-Z]{16}`),                                                                // AWS access key id
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),                      // JWT
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),          // PEM private key
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{16,}`),                                                   // bearer tokens
	regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|access[_-]?key)\s*[:=]\s*[^\s"']{6,}`), // key=value
}

// redactString is the placeholder substituted for any matched secret.
const redactString = "[REDACTED]"

// Redact replaces any substring matching a known credential pattern with a
// placeholder, returning a content-safe version of s.
func Redact(s string) string {
	if s == "" {
		return s
	}
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redactString)
	}
	return s
}
