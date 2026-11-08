// Package security contiene controles transversales: escaneo de secretos y
// rate limit en memoria.
package security

import (
	"errors"
	"regexp"
	"sync"
	"time"
)

// ErrSecretDetected indica que un contenido parece contener una credencial.
var ErrSecretDetected = errors.New("contenido rechazado: parece contener un secreto")

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`ab_[A-Za-z0-9_-]{30,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN (RSA |OPENSSH |EC |PGP )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)postgres(ql)?://[^\s]+:[^\s]+@`),
}

// ScanSecrets rechaza contenido con tokens o claves evidentes.
func ScanSecrets(content string) error {
	for _, pattern := range secretPatterns {
		if pattern.MatchString(content) {
			return ErrSecretDetected
		}
	}
	return nil
}

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter es un token bucket por clave (participante o IP).
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
	now     func() time.Time
}

// NewLimiter crea un limiter con rate por minuto y burst igual al rate.
func NewLimiter(perMinute int) *Limiter {
	if perMinute <= 0 {
		perMinute = 240
	}
	return &Limiter{
		buckets: make(map[string]*bucket),
		rate:    float64(perMinute) / 60.0,
		burst:   float64(perMinute),
		now:     time.Now,
	}
}

// Allow descuenta un token si hay disponible.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	current, ok := l.buckets[key]
	if !ok {
		current = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = current
	}
	elapsed := now.Sub(current.last).Seconds()
	if elapsed > 0 {
		current.tokens += elapsed * l.rate
		if current.tokens > l.burst {
			current.tokens = l.burst
		}
		current.last = now
	}
	if current.tokens < 1 {
		return false
	}
	current.tokens--
	return true
}
