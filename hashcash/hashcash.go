package hashcash

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

type HashFunc func(in []byte) (out [28]byte)

func CountLeadingBits[T [28]byte | []byte](s T) (n int) {
	for index := range len(s) {
		zeroes := bits.LeadingZeros8(s[index])
		if zeroes != 8 {
			return n + zeroes
		}
		n += zeroes
	}
	return n
}

// TimeLayout avoids colons, which separate the stamp fields.
const TimeLayout = "2006-01-02T15/04/05.999999999Z0700"

const ctxCheckEvery = 1 << 12

// Creates a hashcash (hc)
// hash is reset on every try
func (d *Details) Hashcash(ctx context.Context, h HashFunc) (hc string, err error) {
	if d.Time.IsZero() {
		d.Time = time.Now()
	}
	var counterBytes [8]byte

	var ticketBase = "1:" +
		strconv.FormatInt(int64(d.LeadingZeroes), 10) + ":" +
		d.Time.Format(TimeLayout) + ":" +
		d.Payload + ":" +
		d.Extension + ":" +
		d.Random + ":"
	var ticket = make([]byte, 0, len(ticketBase)+base64.StdEncoding.EncodedLen(len(counterBytes)))
	ticket = append(ticket, ticketBase...)

	for counter := uint64(0); ; counter++ {
		if counter%ctxCheckEvery == 0 {
			if err = ctx.Err(); err != nil {
				return "", fmt.Errorf("context error: %w", err)
			}
		}

		ticket = base64.StdEncoding.AppendEncode(ticket[:len(ticketBase)], binary.BigEndian.AppendUint64(counterBytes[:0], counter))

		if CountLeadingBits(h(ticket)) >= d.LeadingZeroes {
			d.Counter = counter
			return string(ticket), nil
		}
	}
}

// Verifies a hashcash is valid. Returns nil on valid
// And error if the hash is invalid
func Verify(h HashFunc, hc string, minDifficulty int) (err error) {
	var parts = strings.Split(hc, ":")
	if len(parts) != 7 {
		return errors.New("invalid hashcash")
	}

	promisedBits, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse bits part: %w", err)
	}

	if int(promisedBits) < minDifficulty {
		return fmt.Errorf("expecting at least a difficulty of: %d", minDifficulty)
	}

	computedBits := uint64(CountLeadingBits(h([]byte(hc))))
	if computedBits < promisedBits {
		return fmt.Errorf("expecting at least %d bits but got %d", promisedBits, computedBits)
	}
	return nil
}

type Details struct {
	Random        string
	Payload       string
	Extension     string
	Counter       uint64
	Time          time.Time
	LeadingZeroes int
}

// Imports the details from a hashcash string
func (d *Details) FromHashcash(hc string) (err error) {
	// 1:%d:%s::%s:%s:%s
	split := strings.Split(hc, ":")
	if len(split) != 7 {
		return fmt.Errorf("incomplete hashcash")
	}
	if split[0] != "1" {
		return fmt.Errorf("expecting hashcash version 1")
	}

	lZeros, err := strconv.ParseInt(split[1], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse leading zeroes: %w", err)
	}
	t, err := time.Parse(TimeLayout, split[2])
	if err != nil {
		return fmt.Errorf("failed to parse hashcash time: %w", err)
	}
	rawCounter, err := base64.StdEncoding.DecodeString(split[6])
	if err != nil {
		return fmt.Errorf("failed to base64 decode counter: %w", err)
	}
	if len(rawCounter) != 8 {
		return fmt.Errorf("counter must be 8 bytes, got %d", len(rawCounter))
	}

	*d = Details{
		Random:        split[5],
		Payload:       split[3],
		Extension:     split[4],
		Counter:       binary.BigEndian.Uint64(rawCounter),
		Time:          t,
		LeadingZeroes: int(lZeros),
	}

	return nil
}
