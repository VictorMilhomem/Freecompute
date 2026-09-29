package ipfs

import "io"

type limitedReadCloser struct {
	rc    io.ReadCloser
	limit int64
	read  int64
}

func newLimitedReader(rc io.ReadCloser, maxBytes int64) io.ReadCloser {
	return &limitedReadCloser{
		rc:    rc,
		limit: maxBytes,
	}
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	if l.limit >= 0 && l.read >= l.limit {
		var buf [1]byte
		n, err := l.rc.Read(buf[:])
		if n > 0 {
			return 0, ErrTooLarge
		}
		if err != nil {
			return 0, err
		}
	}

	toRead := len(p)
	if l.limit >= 0 {
		allowed := int(l.limit - l.read)
		if toRead > allowed+1 {
			toRead = allowed + 1
		}
	}

	n, err := l.rc.Read(p[:toRead])
	if l.limit >= 0 && l.read+int64(n) > l.limit {
		return 0, ErrTooLarge
	}
	l.read += int64(n)
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.rc.Close()
}
