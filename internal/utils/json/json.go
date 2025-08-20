package json

import (
	"errors"

	"github.com/bytedance/sonic"
)

func Encode[T any](v *T) ([]byte, error) {
	if v == nil {
		return nil, errors.New("nil value")
	}
	return sonic.Marshal(v)
}

func Decode[T any](data []byte, v *T) error {
	if v == nil {
		return errors.New("nil pointer to value")
	}
	if len(data) == 0 {
		return errors.New("empty data")
	}
	return sonic.Unmarshal(data, v)
}
