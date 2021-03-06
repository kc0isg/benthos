// Copyright (c) 2017 Ashley Jeffs
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package processor

import (
	"math/rand"

	"github.com/Jeffail/benthos/lib/log"
	"github.com/Jeffail/benthos/lib/metrics"
	"github.com/Jeffail/benthos/lib/response"
	"github.com/Jeffail/benthos/lib/types"
)

//------------------------------------------------------------------------------

func init() {
	Constructors[TypeSample] = TypeSpec{
		constructor: NewSample,
		description: `
Retains a randomly sampled percentage of messages (0 to 100) and drops all
others. The random seed is static in order to sample deterministically, but can
be set in config to allow parallel samples that are unique.`,
	}
}

//------------------------------------------------------------------------------

// SampleConfig contains configuration fields for the Sample processor.
type SampleConfig struct {
	Retain     float64 `json:"retain" yaml:"retain"`
	RandomSeed int64   `json:"seed" yaml:"seed"`
}

// NewSampleConfig returns a SampleConfig with default values.
func NewSampleConfig() SampleConfig {
	return SampleConfig{
		Retain:     10.0, // 10%
		RandomSeed: 0,
	}
}

//------------------------------------------------------------------------------

// Sample is a processor that drops messages based on a random sample.
type Sample struct {
	conf  Config
	log   log.Modular
	stats metrics.Type

	retain float64
	gen    *rand.Rand

	mCount     metrics.StatCounter
	mDropped   metrics.StatCounter
	mSent      metrics.StatCounter
	mSentParts metrics.StatCounter
}

// NewSample returns a Sample processor.
func NewSample(
	conf Config, mgr types.Manager, log log.Modular, stats metrics.Type,
) (Type, error) {
	gen := rand.New(rand.NewSource(conf.Sample.RandomSeed))
	return &Sample{
		conf:   conf,
		log:    log.NewModule(".processor.sample"),
		stats:  stats,
		retain: conf.Sample.Retain / 100.0,
		gen:    gen,

		mCount:     stats.GetCounter("processor.sample.count"),
		mDropped:   stats.GetCounter("processor.sample.dropped"),
		mSent:      stats.GetCounter("processor.sample.sent"),
		mSentParts: stats.GetCounter("processor.sample.parts.sent"),
	}, nil
}

//------------------------------------------------------------------------------

// ProcessMessage applies the processor to a message, either creating >0
// resulting messages or a response to be sent back to the message source.
func (s *Sample) ProcessMessage(msg types.Message) ([]types.Message, types.Response) {
	s.mCount.Incr(1)
	if s.gen.Float64() > s.retain {
		s.mDropped.Incr(1)
		return nil, response.NewAck()
	}
	s.mSent.Incr(1)
	s.mSentParts.Incr(int64(msg.Len()))
	msgs := [1]types.Message{msg}
	return msgs[:], nil
}

//------------------------------------------------------------------------------
