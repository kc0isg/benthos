// Copyright (c) 2018 Ashley Jeffs
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
	"encoding/json"
	"fmt"

	"github.com/Jeffail/benthos/lib/log"
	"github.com/Jeffail/benthos/lib/message"
	"github.com/Jeffail/benthos/lib/message/mapper"
	"github.com/Jeffail/benthos/lib/metrics"
	"github.com/Jeffail/benthos/lib/processor/condition"
	"github.com/Jeffail/benthos/lib/types"
)

//------------------------------------------------------------------------------

func init() {
	Constructors[TypeProcessMap] = TypeSpec{
		constructor: func(conf Config, mgr types.Manager, log log.Modular, stats metrics.Type) (Type, error) {
			return NewProcessMap(conf.ProcessMap, mgr, log, stats)
		},
		description: `
A processor that extracts and maps fields from the original payload into new
objects, applies a list of processors to the newly constructed objects, and
finally maps the result back into the original payload.

This processor is useful for performing processors on subsections of a payload.
For example, you could extract sections of a JSON object in order to construct
a request object for an ` + "`http`" + ` processor, then map the result back
into a field within the original object.

The order of stages of this processor are as follows:

- Conditions are applied to each _individual_ message part in the batch,
  determining whether the part will be mapped. If the conditions are empty all
  message parts will be mapped. If the field ` + "`parts`" + ` is populated the
  message parts not in this list are also excluded from mapping.
- Message parts that are flagged for mapping are mapped according to the premap
  fields, creating a new object. If the premap stage fails (targets are not
  found) the message part will not be processed.
- Message parts that are mapped are processed as a batch. You may safely break
  the batch into individual parts during processing with the ` + "`split`" + `
  processor.
- After all child processors are applied to the mapped messages they are mapped
  back into the original message parts they originated from as per your postmap.
  If the postmap stage fails the mapping is skipped and the message payload
  remains as it started.

Map paths are arbitrary dot paths, target path hierarchies are constructed if
they do not yet exist. Processing is skipped for message parts where the premap
targets aren't found, for optional premap targets use ` + "`premap_optional`" + `.

If postmap targets are not found the merge is abandoned, for optional postmap
targets use ` + "`postmap_optional`" + `.

If the premap is empty then the full payload is sent to the processors, if the
postmap is empty then the processed result replaces the original contents
entirely.

Maps can reference the root of objects either with an empty string or '.', for
example the maps:

` + "``` yaml" + `
premap:
  .: foo.bar
postmap:
  foo.bar: .
` + "```" + `

Would create a new object where the root is the value of ` + "`foo.bar`" + ` and
would map the full contents of the result back into ` + "`foo.bar`" + `.

If the number of total message parts resulting from the processing steps does
not match the original count then this processor fails and the messages continue
unchanged. Therefore, you should avoid using batch and filter type processors in
this list.

### Batch Ordering

This processor supports batch messages. When message parts are post-mapped after
processing they will be correctly aligned with the original batch. However, the
ordering of premapped message parts as they are sent through processors are not
guaranteed to match the ordering of the original batch.`,
		sanitiseConfigFunc: func(conf Config) (interface{}, error) {
			return conf.ProcessMap.Sanitise()
		},
	}
}

//------------------------------------------------------------------------------

// ProcessMapConfig is a config struct containing fields for the
// ProcessMap processor.
type ProcessMapConfig struct {
	Parts           []int              `json:"parts" yaml:"parts"`
	Conditions      []condition.Config `json:"conditions" yaml:"conditions"`
	Premap          map[string]string  `json:"premap" yaml:"premap"`
	PremapOptional  map[string]string  `json:"premap_optional" yaml:"premap_optional"`
	Postmap         map[string]string  `json:"postmap" yaml:"postmap"`
	PostmapOptional map[string]string  `json:"postmap_optional" yaml:"postmap_optional"`
	Processors      []Config           `json:"processors" yaml:"processors"`
}

// NewProcessMapConfig returns a default ProcessMapConfig.
func NewProcessMapConfig() ProcessMapConfig {
	return ProcessMapConfig{
		Parts:           []int{},
		Conditions:      []condition.Config{},
		Premap:          map[string]string{},
		PremapOptional:  map[string]string{},
		Postmap:         map[string]string{},
		PostmapOptional: map[string]string{},
		Processors:      []Config{},
	}
}

// Sanitise the configuration into a minimal structure that can be printed
// without changing the intent.
func (p ProcessMapConfig) Sanitise() (map[string]interface{}, error) {
	var err error
	condConfs := make([]interface{}, len(p.Conditions))
	for i, cConf := range p.Conditions {
		if condConfs[i], err = condition.SanitiseConfig(cConf); err != nil {
			return nil, err
		}
	}
	procConfs := make([]interface{}, len(p.Processors))
	for i, pConf := range p.Processors {
		if procConfs[i], err = SanitiseConfig(pConf); err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{
		"parts":            p.Parts,
		"conditions":       condConfs,
		"premap":           p.Premap,
		"premap_optional":  p.PremapOptional,
		"postmap":          p.Postmap,
		"postmap_optional": p.PostmapOptional,
		"processors":       procConfs,
	}, nil
}

//------------------------------------------------------------------------------

// UnmarshalJSON ensures that when parsing configs that are in a slice the
// default values are still applied.
func (p *ProcessMapConfig) UnmarshalJSON(bytes []byte) error {
	type confAlias ProcessMapConfig
	aliased := confAlias(NewProcessMapConfig())

	if err := json.Unmarshal(bytes, &aliased); err != nil {
		return err
	}

	*p = ProcessMapConfig(aliased)
	return nil
}

// UnmarshalYAML ensures that when parsing configs that are in a slice the
// default values are still applied.
func (p *ProcessMapConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type confAlias ProcessMapConfig
	aliased := confAlias(NewProcessMapConfig())

	if err := unmarshal(&aliased); err != nil {
		return err
	}

	*p = ProcessMapConfig(aliased)
	return nil
}

//------------------------------------------------------------------------------

// ProcessMap is a processor that applies a list of child processors to a new
// payload mapped from the original, and after processing attempts to overlay
// the results back onto the original payloads according to more mappings.
type ProcessMap struct {
	parts []int

	mapper   *mapper.Type
	children []Type

	log log.Modular

	mCount        metrics.StatCounter
	mCountParts   metrics.StatCounter
	mSkipped      metrics.StatCounter
	mSkippedParts metrics.StatCounter
	mErr          metrics.StatCounter
	mErrPre       metrics.StatCounter
	mErrProc      metrics.StatCounter
	mErrPost      metrics.StatCounter
	mSent         metrics.StatCounter
	mSentParts    metrics.StatCounter
}

// NewProcessMap returns a ProcessField processor.
func NewProcessMap(
	conf ProcessMapConfig, mgr types.Manager, log log.Modular, stats metrics.Type,
) (*ProcessMap, error) {
	nsStats := metrics.Namespaced(stats, "processor.process_map")
	nsLog := log.NewModule(".processor.process_map")

	var children []Type
	for _, pconf := range conf.Processors {
		proc, err := New(pconf, mgr, nsLog, nsStats)
		if err != nil {
			return nil, err
		}
		children = append(children, proc)
	}

	var conditions []types.Condition
	for _, cconf := range conf.Conditions {
		cond, err := condition.New(cconf, mgr, nsLog, nsStats)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, cond)
	}

	p := &ProcessMap{
		parts: conf.Parts,

		children: children,

		log: nsLog,

		mCount:        stats.GetCounter("processor.process_map.count"),
		mCountParts:   stats.GetCounter("processor.process_map.parts.count"),
		mSkipped:      stats.GetCounter("processor.process_map.skipped"),
		mSkippedParts: stats.GetCounter("processor.process_map.parts.skipped"),
		mErr:          stats.GetCounter("processor.process_map.error"),
		mErrPre:       stats.GetCounter("processor.process_map.error.premap"),
		mErrProc:      stats.GetCounter("processor.process_map.error.processors"),
		mErrPost:      stats.GetCounter("processor.process_map.error.postmap"),
		mSent:         stats.GetCounter("processor.process_map.sent"),
		mSentParts:    stats.GetCounter("processor.process_map.parts.sent"),
	}

	var err error
	if p.mapper, err = mapper.New(
		mapper.OptSetLogger(nsLog),
		mapper.OptSetStats(nsStats),
		mapper.OptSetConditions(conditions),
		mapper.OptSetReqMap(conf.Premap),
		mapper.OptSetOptReqMap(conf.PremapOptional),
		mapper.OptSetResMap(conf.Postmap),
		mapper.OptSetOptResMap(conf.PostmapOptional),
	); err != nil {
		return nil, err
	}

	return p, nil
}

//------------------------------------------------------------------------------

// ProcessMessage applies the processor to a message, either creating >0
// resulting messages or a response to be sent back to the message source.
func (p *ProcessMap) ProcessMessage(msg types.Message) ([]types.Message, types.Response) {
	alignedResult, err := p.CreateResult(msg)
	if err != nil {
		msgs := [1]types.Message{msg}
		return msgs[:], nil
	}

	result := msg.Copy()
	if err = p.OverlayResult(result, alignedResult); err != nil {
		msgs := [1]types.Message{msg}
		return msgs[:], nil
	}

	msgs := [1]types.Message{result}
	return msgs[:], nil
}

// TargetsUsed returns a list of target dependencies of this processor derived
// from its premap and premap_optional fields.
func (p *ProcessMap) TargetsUsed() []string {
	return p.mapper.TargetsUsed()
}

// TargetsProvided returns a list of targets provided by this processor derived
// from its postmap and postmap_optional fields.
func (p *ProcessMap) TargetsProvided() []string {
	return p.mapper.TargetsProvided()
}

// CreateResult performs reduction and child processors to a payload and returns
// the result. The resulting message should have the same dimension as the
// payload, where reduced indexes are nil. This result can be overlayed onto the
// original message in order to complete the map.
func (p *ProcessMap) CreateResult(msg types.Message) (types.Message, error) {
	p.mCount.Incr(1)
	p.mCountParts.Incr(int64(msg.Len()))

	mapMsg := msg
	if len(p.parts) > 0 {
		mapMsg = message.New(make([][]byte, msg.Len()))
		for _, sel := range p.parts {
			mapMsg.Get(sel).Set(msg.Get(sel).Get())
		}
	}

	mappedMsg, skipped, err := p.mapper.MapRequests(mapMsg)
	if err != nil {
		p.mErr.Incr(1)
		p.mErrPre.Incr(1)
		p.log.Errorf("Failed to map request: %v\n", err)
		return nil, err
	}

	if mappedMsg.Len() == 0 {
		p.mSkipped.Incr(1)
		p.mSkippedParts.Incr(int64(msg.Len()))
		parts := make([]types.Part, msg.Len())
		mapMsg.SetAll(parts)
		return mapMsg, nil
	}

	var procResults []types.Message
	if procResults, err = processMap(mappedMsg, p.children); err != nil {
		p.mErrProc.Incr(1)
		p.mErr.Incr(1)
		p.log.Errorf("Processors failed: %v\n", err)
		return nil, err
	}

	i := 0
	for _, m := range procResults {
		m.Iter(func(_ int, part types.Part) error {
			p.log.Tracef("Processed request part '%v': %q\n", i, part.Get())
			i++
			return nil
		})
	}

	var alignedResult types.Message
	if alignedResult, err = p.mapper.AlignResult(msg.Len(), skipped, procResults); err != nil {
		p.mErrPost.Incr(1)
		p.mErr.Incr(1)
		p.log.Errorf("Postmap failed: %v\n", err)
		return nil, err
	}

	return alignedResult, nil
}

// OverlayResult attempts to merge the result of a process_map with the original
//  payload as per the map specified in the postmap and postmap_optional fields.
func (p *ProcessMap) OverlayResult(payload, response types.Message) error {
	if err := p.mapper.MapResponses(payload, response); err != nil {
		p.mErrPost.Incr(1)
		p.mErr.Incr(1)
		p.log.Errorf("Postmap failed: %v\n", err)
		return err
	}

	p.mSent.Incr(1)
	p.mSentParts.Incr(int64(payload.Len()))
	return nil
}

func processMap(mappedMsg types.Message, processors []Type) ([]types.Message, error) {
	requestMsgs := []types.Message{mappedMsg}
	i := 0
	for ; len(requestMsgs) > 0 && i < len(processors); i++ {
		var nextRequestMsgs []types.Message
		for _, m := range requestMsgs {
			rMsgs, _ := processors[i].ProcessMessage(m)
			nextRequestMsgs = append(nextRequestMsgs, rMsgs...)
		}
		requestMsgs = nextRequestMsgs
	}

	if len(requestMsgs) == 0 {
		return nil, fmt.Errorf("processor index '%v' returned zero messages", i)
	}

	return requestMsgs, nil
}
