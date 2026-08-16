package tooling

import (
	"fmt"
	"io"
)

const maxToolResponseBytes int64 = 1 << 20

func readToolResponse(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxToolResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxToolResponseBytes {
		return nil, fmt.Errorf("tool response exceeds %d bytes", maxToolResponseBytes)
	}
	return data, nil
}
