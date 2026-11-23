// Package jsonl lê arquivos JSON Lines em streaming, sem carregar o arquivo
// em memória e sem o limite de 64 KB por linha do bufio.Scanner.
//
// Regras (docs/discovery/05-sqlite-fts5.md §3):
//   - só linhas terminadas em '\n' são entregues; bytes finais sem '\n' são
//     uma escrita em andamento e ficam para a próxima leitura (Partial);
//   - linhas maiores que o limite são descartadas e contadas, sem guardar
//     o conteúdo.
package jsonl

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// DefaultMaxLine é o tamanho máximo de uma linha aceita.
const DefaultMaxLine = 32 << 20

// Reader entrega linhas completas de um io.Reader.
type Reader struct {
	br  *bufio.Reader
	max int
	buf []byte

	// Offset é a posição logo após a última linha completa consumida.
	Offset int64
	// Lines conta linhas não vazias entregues.
	Lines int
	// LongLines conta linhas descartadas por excederem o limite.
	LongLines int
	// Partial indica que o arquivo termina sem '\n'.
	Partial bool
}

// NewReader cria um leitor com limite de linha max (<= 0 usa o padrão).
func NewReader(r io.Reader, max int) *Reader {
	if max <= 0 {
		max = DefaultMaxLine
	}
	return &Reader{br: bufio.NewReaderSize(r, 64<<10), max: max}
}

// Next devolve a próxima linha não vazia, sem o terminador. O slice só é
// válido até a próxima chamada. No fim devolve io.EOF.
func (r *Reader) Next() ([]byte, error) {
	for {
		r.buf = r.buf[:0]
		var n int64
		tooLong := false
		for {
			chunk, err := r.br.ReadSlice('\n')
			n += int64(len(chunk))
			if !tooLong {
				if len(r.buf)+len(chunk) > r.max+2 { // +2: "\r\n"
					tooLong = true
					r.buf = r.buf[:0]
				} else {
					r.buf = append(r.buf, chunk...)
				}
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) {
				if n > 0 {
					r.Partial = true
				}
				return nil, io.EOF
			}
			if err != nil {
				return nil, err
			}
			break
		}
		r.Offset += n
		if tooLong {
			r.LongLines++
			continue
		}
		line := bytes.TrimSpace(r.buf)
		if len(line) == 0 {
			continue
		}
		r.Lines++
		return line, nil
	}
}
