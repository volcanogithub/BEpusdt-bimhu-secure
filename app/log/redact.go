package log

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

var sensitive = regexp.MustCompile(`(?i)(password|passwd|token|secret|private[_ -]?key|api[_ -]?(key|credential)|authorization|cookie|signature|postgres(ql)?://|BEGIN .*PRIVATE KEY)`)
var urlQuery = regexp.MustCompile(`https?://[^\s"<>]+`)
var known = struct {
	sync.RWMutex
	values map[string]struct{}
}{values: make(map[string]struct{})}

func SensitiveName(name string) bool { return sensitive.MatchString(name) || name == "admin_secure" }

// RegisterSecrets complements label-based suppression for errors containing bare credentials.
func RegisterSecrets(values ...string) {
	known.Lock()
	defer known.Unlock()
	for _, value := range values {
		if value != "" {
			known.values[value] = struct{}{}
		}
	}
}

func Redact(value string) string {
	known.RLock()
	for secret := range known.values {
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	known.RUnlock()
	lines := strings.Split(value, "\n")
	pem := false
	for i, line := range lines {
		if strings.Contains(line, "BEGIN ") && strings.Contains(line, "PRIVATE KEY") {
			pem = true
		}
		if pem || sensitive.MatchString(line) {
			lines[i] = "[redacted sensitive log line]"
			if strings.Contains(line, "END ") && strings.Contains(line, "PRIVATE KEY") {
				pem = false
			}
			continue
		}
		lines[i] = urlQuery.ReplaceAllStringFunc(line, func(url string) string {
			if strings.ContainsAny(url, "?@") {
				return "[redacted URL]"
			}
			return url
		})
	}
	return strings.Join(lines, "\n")
}

type safeFormatter struct{ inner logrus.Formatter }

func (f safeFormatter) Format(e *logrus.Entry) ([]byte, error) {
	copy := *e
	copy.Message = Redact(e.Message)
	copy.Data = make(logrus.Fields, len(e.Data))
	for k, v := range e.Data {
		if sensitive.MatchString(k) {
			copy.Data["redacted_field"] = "[redacted]"
		} else {
			copy.Data[k] = Redact(fmt.Sprint(v))
		}
	}
	return f.inner.Format(&copy)
}

type safeWriter struct {
	mu            sync.Mutex
	out           io.Writer
	pending       []byte
	pem, dropping bool
}

func SafeWriter(out io.Writer) io.Writer { return &safeWriter{out: out} }
func (w *safeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			i = len(p)
		}
		if !w.dropping {
			if len(w.pending)+i > 65536 {
				w.pending = nil
				w.dropping = true
			} else {
				w.pending = append(w.pending, p[:i]...)
			}
		}
		if i == len(p) {
			break
		}
		line := string(w.pending)
		if strings.Contains(line, "BEGIN ") && strings.Contains(line, "PRIVATE KEY") {
			w.pem = true
		}
		var output string
		if w.dropping || w.pem {
			output = "[redacted sensitive log line]"
		} else {
			output = Redact(line)
		}
		if strings.Contains(line, "END ") && strings.Contains(line, "PRIVATE KEY") {
			w.pem = false
		}
		if _, err := io.WriteString(w.out, output+"\n"); err != nil {
			return 0, err
		}
		w.pending = nil
		w.dropping = false
		p = p[i+1:]
	}
	return n, nil
}
