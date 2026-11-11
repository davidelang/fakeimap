package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"io"
)

// RFC 4978 DEFLATE is raw deflate (windowBits -15). A zlib header is the
// wrong format. Each write is sync-flushed so the peer can read it.

type syncDeflateWriter struct {
	w *flate.Writer
}

func newSyncDeflateWriter(dst io.Writer) (io.Writer, error) {
	w, err := flate.NewWriter(dst, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	return &syncDeflateWriter{w: w}, nil
}

func (w *syncDeflateWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		return n, err
	}
	if err := w.w.Flush(); err != nil {
		return n, err
	}
	return n, nil
}

// lazyDeflateReader starts inflate on the first read, after the peer has
// sent compressed bytes.
type lazyDeflateReader struct {
	r  io.Reader
	fr io.Reader
}

func (z *lazyDeflateReader) Read(p []byte) (int, error) {
	if z.fr == nil {
		z.fr = flate.NewReader(z.r)
	}
	return z.fr.Read(p)
}

func (s *session) startDeflate() error {
	src := io.Reader(s.conn)
	if n := s.br.Buffered(); n > 0 {
		pre := make([]byte, n)
		if _, err := io.ReadFull(s.br, pre); err != nil {
			return err
		}
		src = io.MultiReader(bytes.NewReader(pre), s.conn)
	}
	zw, err := newSyncDeflateWriter(s.conn)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	s.br = bufio.NewReader(&lazyDeflateReader{r: src})
	s.bw = bufio.NewWriter(zw)
	s.compressed = true
	s.wmu.Unlock()
	return nil
}

func (c *imapTalk) startDeflate() error {
	src := io.Reader(c.conn)
	if n := c.r.Buffered(); n > 0 {
		pre := make([]byte, n)
		if _, err := io.ReadFull(c.r, pre); err != nil {
			return err
		}
		src = io.MultiReader(bytes.NewReader(pre), c.conn)
	}
	zw, err := newSyncDeflateWriter(c.conn)
	if err != nil {
		return err
	}
	c.r = bufio.NewReader(&lazyDeflateReader{r: src})
	c.w = bufio.NewWriter(zw)
	return nil
}
