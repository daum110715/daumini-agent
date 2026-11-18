package compaction

import (
	"fmt"
	"strings"

	"github.com/daum110715/daumini-agent/internal/types"
)

const (
	minMaskBatchCount = 8
	keepRecentTurns   = 4
	contextMaskRatio  = 0.75
	minMaskedChars    = 360
)

func EstimateMessageTokens(msgs []types.Message) int64 {
	var chars int64
	for _, m := range msgs {
		for _, b := range m.Blocks {
			chars += int64(len(b.Text) + len(b.Thinking) + len(b.Input) + 32)
		}
	}
	return chars / 3
}

// MaskObservationsBatch replaces stale oversized tool_result bodies in large batches so KV cache isn't thrashed every turn.
func MaskObservationsBatch(msgs []types.Message, contextWindow int64, lastInputTokens int64, force bool) ([]types.Message, int) {
	if len(msgs) <= keepRecentTurns*2 {
		return msgs, 0
	}
	if contextWindow <= 0 {
		contextWindow = 128000
	}
	used := lastInputTokens
	if used <= 0 {
		used = EstimateMessageTokens(msgs)
	}
	if !force && float64(used) < float64(contextWindow)*contextMaskRatio {
		return msgs, 0
	}

	cutoff := len(msgs) - keepRecentTurns*2
	candidates := 0
	for i := 0; i < cutoff; i++ {
		for _, b := range msgs[i].Blocks {
			if b.Type == types.BlockToolResult && !b.Masked && len(b.Text) >= minMaskedChars {
				candidates++
			}
		}
	}
	if !force && candidates < minMaskBatchCount {
		return msgs, 0
	}
	if candidates == 0 {
		return msgs, 0
	}

	maskedCount := 0
	out := make([]types.Message, len(msgs))
	copy(out, msgs)
	for i := 0; i < cutoff; i++ {
		blocks := make([]types.Block, len(out[i].Blocks))
		copy(blocks, out[i].Blocks)
		changed := false
		for j := range blocks {
			b := &blocks[j]
			if b.Type == types.BlockToolResult && !b.Masked && len(b.Text) >= minMaskedChars {
				lineCount := strings.Count(b.Text, "\n") + 1
				status := "ok"
				if b.IsError {
					status = "error"
				}
				b.Text = fmt.Sprintf("[observation masked (%s): %d chars, %d lines; re-read if needed]", status, len(b.Text), lineCount)
				b.Masked = true
				maskedCount++
				changed = true
			}
		}
		if changed {
			out[i].Blocks = blocks
		}
	}
	return out, maskedCount
}
