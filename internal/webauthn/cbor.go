package webauthn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
)

// cborKind is the subset of CBOR major types WebAuthn uses: attestation
// objects are maps of text keys, and COSE keys are maps of integer keys.
type cborKind uint8

const (
	cborUint cborKind = iota
	cborNegInt
	cborBytes
	cborText
	cborArray
	cborMap
)

// cborValue is one decoded CBOR item; only the field for its kind is set.
type cborValue struct {
	kind cborKind
	u    uint64
	i    int64
	b    []byte
	s    string
	arr  []cborValue
	m    map[any]cborValue
}

// maxCBORDepth bounds recursion. A WebAuthn attestation is a handful of
// levels deep; anything deeper is not something this package should read.
const maxCBORDepth = 16

// decodeCBOR decodes one item and returns it with the bytes that follow.
func decodeCBOR(data []byte) (cborValue, []byte, error) {
	return decodeCBORAt(data, 0)
}

func decodeCBORAt(data []byte, depth int) (cborValue, []byte, error) {
	if depth > maxCBORDepth {
		return cborValue{}, nil, errors.New("cbor: nesting is too deep")
	}
	if len(data) == 0 {
		return cborValue{}, nil, errors.New("cbor: unexpected end of data")
	}
	major := data[0] >> 5
	info := data[0] & 0x1f
	n, rest, err := cborLength(info, data[1:])
	if err != nil {
		return cborValue{}, nil, err
	}
	switch major {
	case 0:
		return cborValue{kind: cborUint, u: n}, rest, nil
	case 1:
		if n > math.MaxInt64 {
			return cborValue{}, nil, errors.New("cbor: negative integer is out of range")
		}
		return cborValue{kind: cborNegInt, i: -1 - int64(n)}, rest, nil
	case 2:
		if uint64(len(rest)) < n {
			return cborValue{}, nil, errors.New("cbor: byte string is truncated")
		}
		return cborValue{kind: cborBytes, b: rest[:n]}, rest[n:], nil
	case 3:
		if uint64(len(rest)) < n {
			return cborValue{}, nil, errors.New("cbor: text string is truncated")
		}
		return cborValue{kind: cborText, s: string(rest[:n])}, rest[n:], nil
	case 4:
		if uint64(len(rest)) < n {
			return cborValue{}, nil, errors.New("cbor: array is truncated")
		}
		arr := make([]cborValue, 0, n)
		for i := uint64(0); i < n; i++ {
			var v cborValue
			v, rest, err = decodeCBORAt(rest, depth+1)
			if err != nil {
				return cborValue{}, nil, err
			}
			arr = append(arr, v)
		}
		return cborValue{kind: cborArray, arr: arr}, rest, nil
	case 5:
		if uint64(len(rest)) < 2*n {
			return cborValue{}, nil, errors.New("cbor: map is truncated")
		}
		m := make(map[any]cborValue, n)
		for i := uint64(0); i < n; i++ {
			var k, v cborValue
			k, rest, err = decodeCBORAt(rest, depth+1)
			if err != nil {
				return cborValue{}, nil, err
			}
			v, rest, err = decodeCBORAt(rest, depth+1)
			if err != nil {
				return cborValue{}, nil, err
			}
			switch k.kind {
			case cborUint:
				m[int64(k.u)] = v
			case cborNegInt:
				m[k.i] = v
			case cborText:
				m[k.s] = v
			default:
				return cborValue{}, nil, errors.New("cbor: unsupported map key")
			}
		}
		return cborValue{kind: cborMap, m: m}, rest, nil
	default:
		return cborValue{}, nil, fmt.Errorf("cbor: unsupported major type %d", major)
	}
}

func cborLength(info byte, data []byte) (uint64, []byte, error) {
	switch {
	case info < 24:
		return uint64(info), data, nil
	case info == 24:
		if len(data) < 1 {
			return 0, nil, errors.New("cbor: truncated length")
		}
		return uint64(data[0]), data[1:], nil
	case info == 25:
		if len(data) < 2 {
			return 0, nil, errors.New("cbor: truncated length")
		}
		return uint64(binary.BigEndian.Uint16(data)), data[2:], nil
	case info == 26:
		if len(data) < 4 {
			return 0, nil, errors.New("cbor: truncated length")
		}
		return uint64(binary.BigEndian.Uint32(data)), data[4:], nil
	case info == 27:
		if len(data) < 8 {
			return 0, nil, errors.New("cbor: truncated length")
		}
		return binary.BigEndian.Uint64(data), data[8:], nil
	default:
		return 0, nil, fmt.Errorf("cbor: unsupported length encoding %d", info)
	}
}

// encodeCBOR writes the subset of CBOR the software authenticator needs. It
// sorts map keys so a test's fixture is deterministic.
func encodeCBOR(v cborValue) []byte {
	switch v.kind {
	case cborUint:
		return cborHead(0, v.u)
	case cborNegInt:
		return cborHead(1, uint64(-1-v.i))
	case cborBytes:
		return append(cborHead(2, uint64(len(v.b))), v.b...)
	case cborText:
		return append(cborHead(3, uint64(len(v.s))), v.s...)
	case cborArray:
		out := cborHead(4, uint64(len(v.arr)))
		for _, e := range v.arr {
			out = append(out, encodeCBOR(e)...)
		}
		return out
	case cborMap:
		keys := make([]any, 0, len(v.m))
		for k := range v.m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return bytes.Compare(encodeCBOR(cborKey(keys[i])), encodeCBOR(cborKey(keys[j]))) < 0
		})
		out := cborHead(5, uint64(len(v.m)))
		for _, k := range keys {
			out = append(out, encodeCBOR(cborKey(k))...)
			out = append(out, encodeCBOR(v.m[k])...)
		}
		return out
	default:
		panic("webauthn: cannot encode this CBOR value")
	}
}

func cborKey(k any) cborValue {
	switch k := k.(type) {
	case int64:
		if k >= 0 {
			return cborValue{kind: cborUint, u: uint64(k)}
		}
		return cborValue{kind: cborNegInt, i: k}
	case string:
		return cborValue{kind: cborText, s: k}
	case []byte:
		return cborValue{kind: cborBytes, b: k}
	default:
		panic("webauthn: unsupported CBOR map key")
	}
}

func cborHead(major byte, n uint64) []byte {
	head := major << 5
	switch {
	case n < 24:
		return []byte{head | byte(n)}
	case n <= math.MaxUint8:
		return []byte{head | 24, byte(n)}
	case n <= math.MaxUint16:
		return []byte{head | 25, byte(n >> 8), byte(n)}
	case n <= math.MaxUint32:
		return []byte{head | 26, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	default:
		out := []byte{head | 27, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(out[1:], n)
		return out
	}
}
