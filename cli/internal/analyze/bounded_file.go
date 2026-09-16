package analyze

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

type boundedTextInput struct {
	file    *os.File
	limited *io.LimitedReader
	Scanner *bufio.Scanner
	path    string
	label   string
	max     int64
}

func openBoundedTextInput(path, label string, maxBytes int64, initialBuffer, maxLineBytes int) (*boundedTextInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() || info.Size() > maxBytes {
		_ = file.Close()
		return nil, boundedTextFileSizeError(path, label, maxBytes)
	}
	limited := &io.LimitedReader{R: file, N: maxBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, initialBuffer), maxLineBytes)
	return &boundedTextInput{file: file, limited: limited, Scanner: scanner, path: path, label: label, max: maxBytes}, nil
}

func (input *boundedTextInput) Close() error {
	return input.file.Close()
}

func (input *boundedTextInput) Err() error {
	if err := input.Scanner.Err(); err != nil {
		return err
	}
	if input.limited.N == 0 {
		return boundedTextFileSizeError(input.path, input.label, input.max)
	}
	return nil
}

func readBoundedFile(path, label string, maxBytes int64) (data []byte, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxBytes {
		return nil, boundedFileSizeError(path, label, maxBytes)
	}

	data, err = io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > maxBytes {
		return nil, boundedFileSizeError(path, label, maxBytes)
	}
	return data, nil
}

func boundedFileSizeError(path, label string, maxBytes int64) error {
	return fmt.Errorf("%s: %s size must be between 1 and %d bytes", path, label, maxBytes)
}

func boundedTextFileSizeError(path, label string, maxBytes int64) error {
	return fmt.Errorf("%s: %s exceeds size limit %d bytes", path, label, maxBytes)
}
