package sandbox

import "bytes"

type boundedLogBuffer struct {
	maxBytes  int64
	buf       bytes.Buffer
	truncated bool
}

func newBoundedLogBuffer(maxBytes int64) *boundedLogBuffer {
	return &boundedLogBuffer{maxBytes: maxBytes}
}

func (b *boundedLogBuffer) Write(p []byte) (int, error) {
	if b.maxBytes <= 0 {
		return b.buf.Write(p)
	}

	remaining := b.maxBytes - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		// Discard excess without error
		return len(p), nil
	}

	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}

	return b.buf.Write(p)
}

func (b *boundedLogBuffer) WriteString(s string) {
	_, _ = b.Write([]byte(s))
}

func (b *boundedLogBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *boundedLogBuffer) Truncated() bool {
	return b.truncated
}
