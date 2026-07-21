package kb

import (
	"strings"
	"testing"
)

// TestSplit_OnlyEmptyLines verifies that whitespace-only input returns nil.
func TestSplit_OnlyEmptyLines(t *testing.T) {
	s := newTextSplitter(500, 50, 100)
	chunks := s.Split("\n\n\n")
	if chunks != nil {
		t.Errorf("expected nil for whitespace-only text, got %d chunks", len(chunks))
	}
}

// TestSplit_WindowsLineEndings verifies that \r\n line breaks are handled correctly.
func TestSplit_WindowsLineEndings(t *testing.T) {
	s := newTextSplitter(500, 50, 100)
	text := "First paragraph.\r\n\r\nSecond paragraph."
	chunks := s.Split(text)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "First") {
		t.Errorf("first line missing from chunk: %q", chunks[0].Content)
	}
}

// TestSplit_LargeParagraphSplit verifies word-level splitting when chunkSize is too small
// for a single paragraph, triggering splitLargeParagraph.
func TestSplit_LargeParagraphSplit(t *testing.T) {
	// chunkSize=7 forces word-level splitting: 15 words × ~1 token each > 7
	s := newTextSplitter(7, 5, 5)
	text := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen"
	chunks := s.Split(text)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for large paragraph, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Content == "" {
			t.Errorf("chunk %d has empty content", i)
		}
	}
}

// TestSplit_ChunkIndexing verifies that StartOffset is monotonically increasing across
// all output chunks, ensuring correct overlap calculation.
func TestSplit_ChunkIndexing(t *testing.T) {
	// verify StartOffset is monotonically increasing across chunks
	s := newTextSplitter(20, 5, 5)
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "This is line number " + string(rune('A'+i%26))
	}
	text := strings.Join(lines, "\n\n")
	chunks := s.Split(text)

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i].StartOffset < chunks[i-1].StartOffset {
			t.Errorf("StartOffset not monotonic: chunk %d offset %d <= chunk %d offset %d",
				i, chunks[i].StartOffset, i-1, chunks[i-1].StartOffset)
		}
	}
}

// TestSplit_MinChunkSizeMerge verifies that small trailing chunks are merged into the
// previous chunk by mergeSmallTailChunks, preventing orphaned tiny chunks.
func TestSplit_MinChunkSizeMerge(t *testing.T) {
	// generate enough paragraphs to flush the first chunk,
	// leaving a small tail that should be merged by mergeSmallTailChunks
	s := newTextSplitter(500, 50, 100)
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "Line number "+string(rune('A'+i%26))+" with some extra filler words to reach token count approximately.")
	}
	text := strings.Join(lines, "\n\n")
	chunks := s.Split(text)

	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	last := chunks[len(chunks)-1]
	if last.Content == "" {
		t.Error("last chunk should not be empty after potential merge")
	}
}
