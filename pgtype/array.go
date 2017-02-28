package pgtype

import (
	"bytes"
	"fmt"
	"io"
	"unicode"

	"github.com/jackc/pgx/pgio"
)

// Information on the internals of PostgreSQL arrays can be found in
// src/include/utils/array.h and src/backend/utils/adt/arrayfuncs.c. Of
// particular interest is the array_send function.

type ArrayHeader struct {
	ContainsNull bool
	ElementOID   int32
	Dimensions   []ArrayDimension
}

type ArrayDimension struct {
	Length     int32
	LowerBound int32
}

func (ah *ArrayHeader) DecodeBinary(r io.Reader) error {
	numDims, err := pgio.ReadInt32(r)
	if err != nil {
		return err
	}

	if numDims > 0 {
		ah.Dimensions = make([]ArrayDimension, numDims)
	}

	containsNull, err := pgio.ReadInt32(r)
	if err != nil {
		return err
	}
	ah.ContainsNull = containsNull == 1

	ah.ElementOID, err = pgio.ReadInt32(r)
	if err != nil {
		return err
	}

	for i := range ah.Dimensions {
		ah.Dimensions[i].Length, err = pgio.ReadInt32(r)
		if err != nil {
			return err
		}

		ah.Dimensions[i].LowerBound, err = pgio.ReadInt32(r)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ah *ArrayHeader) EncodeBinary(w io.Writer) error {
	_, err := pgio.WriteInt32(w, int32(len(ah.Dimensions)))
	if err != nil {
		return err
	}

	var containsNull int32
	if ah.ContainsNull {
		containsNull = 1
	}
	_, err = pgio.WriteInt32(w, containsNull)
	if err != nil {
		return err
	}

	_, err = pgio.WriteInt32(w, ah.ElementOID)
	if err != nil {
		return err
	}

	for i := range ah.Dimensions {
		_, err = pgio.WriteInt32(w, ah.Dimensions[i].Length)
		if err != nil {
			return err
		}

		_, err = pgio.WriteInt32(w, ah.Dimensions[i].LowerBound)
		if err != nil {
			return err
		}
	}

	return nil
}

type UntypedTextArray struct {
	Elements   []string
	Dimensions []ArrayDimension
}

func ParseUntypedTextArray(src string) (*UntypedTextArray, error) {
	uta := &UntypedTextArray{
		Elements: []string{},
	}

	buf := bytes.NewBufferString(src)

	skipWhitespace(buf)

	r, _, err := buf.ReadRune()
	if err != nil {
		return nil, fmt.Errorf("invalid array: %v", err)
	}

	var explicitDimensions []ArrayDimension

	// Array has explicit dimensions
	if r == '[' {
		// TODO - parse explicit dimensions
		panic(explicitDimensions)
	}

	// Consume all initial opening brackets. This provides number of dimensions.
	var implicitDimensions []ArrayDimension
	if r != '{' {
		return nil, fmt.Errorf("invalid array, expected '{': %v", err)
	}
	buf.UnreadRune()

	for {
		r, _, err = buf.ReadRune()
		if err != nil {
			return nil, fmt.Errorf("invalid array: %v", err)
		}

		if r == '{' {
			implicitDimensions = append(implicitDimensions, ArrayDimension{LowerBound: 1})
		} else {
			buf.UnreadRune()
			break
		}
	}
	currentDim := len(implicitDimensions) - 1
	counterDim := currentDim
	elemCount := 0

	fmt.Println("-------", currentDim, buf.String())

	for {
		r, _, err = buf.ReadRune()
		if err != nil {
			return nil, fmt.Errorf("invalid array: %v", err)
		}

		switch r {
		case '{':
			fmt.Println("{", buf.String())

			if counterDim == currentDim {
				elemCount++
			}
			currentDim++
		case ',':
		case '}':
			fmt.Println("}", buf.String())
			if counterDim == currentDim {
				implicitDimensions[counterDim].Length = int32(elemCount)
				elemCount = 0
			}

			currentDim--
		default:
			buf.UnreadRune()
			fmt.Println("default", buf.String())
			value, err := arrayParseValue(buf)
			if err != nil {
				return nil, fmt.Errorf("invalid array value: %v", err)
			}
			if counterDim == currentDim {
				elemCount++
			}
			uta.Elements = append(uta.Elements, value)
		}

		if currentDim < 0 {
			break
		}

	}

	skipWhitespace(buf)

	if buf.Len() > 0 {
		return nil, fmt.Errorf("unexpected trailing data: %v", buf.String())
	}

	if len(explicitDimensions) > 0 {
		uta.Dimensions = explicitDimensions
	} else {
		uta.Dimensions = implicitDimensions
		if len(uta.Dimensions) == 1 && uta.Dimensions[0].Length == 0 {
			uta.Dimensions = []ArrayDimension{}
		}
	}

	return uta, nil
}

func skipWhitespace(buf *bytes.Buffer) {
	var r rune
	var err error
	for r, _, _ = buf.ReadRune(); unicode.IsSpace(r); r, _, _ = buf.ReadRune() {
	}

	if err != io.EOF {
		buf.UnreadRune()
	}
}

func arrayParseValue(buf *bytes.Buffer) (string, error) {
	r, _, err := buf.ReadRune()
	if err != nil {
		return "", err
	}
	if r == '"' {
		return arrayParseQuotedValue(buf)
	}
	buf.UnreadRune()

	s := &bytes.Buffer{}

	for {
		r, _, err := buf.ReadRune()
		if err != nil {
			return "", err
		}

		switch r {
		case ',', '}':
			buf.UnreadRune()
			return s.String(), nil
		}

		s.WriteRune(r)
	}
}

func arrayParseQuotedValue(buf *bytes.Buffer) (string, error) {
	s := &bytes.Buffer{}

	for {
		r, _, err := buf.ReadRune()
		if err != nil {
			return "", err
		}

		switch r {
		case '\\':
			r, _, err = buf.ReadRune()
			if err != nil {
				return "", err
			}
		case '"':
			r, _, err = buf.ReadRune()
			if err != nil {
				return "", err
			}
			buf.UnreadRune()
			return s.String(), nil
		}
		s.WriteRune(r)
	}
}
