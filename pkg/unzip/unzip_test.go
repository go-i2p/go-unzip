package unzip

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// createZipWithEntry creates a zip file at path containing a single entry
// with the given name and content.
func createZipWithEntry(t *testing.T, path, entryName string, content []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	fw, err := w.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// createZipWithEntries creates a zip file at path containing multiple entries.
func createZipWithEntries(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtract_NoLimits(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")
	content := []byte("hello world")
	createZipWithEntry(t, zipPath, "greeting.txt", content)

	uz := New()
	dest := filepath.Join(tmpDir, "out")
	files, err := uz.Extract(zipPath, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "greeting.txt" {
		t.Fatalf("unexpected files: %v", files)
	}
	got, err := os.ReadFile(filepath.Join(dest, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestExtract_MaxEntrySize_Exceeded(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "bomb.zip")

	// Create an entry that decompresses to 1 MiB of zeros (highly compressible)
	bigContent := make([]byte, 1<<20) // 1 MiB
	createZipWithEntry(t, zipPath, "big.bin", bigContent)

	uz := &Unzip{
		MaxEntrySize: 512 * 1024, // 512 KiB limit
	}
	dest := filepath.Join(tmpDir, "out")
	_, err := uz.Extract(zipPath, dest)
	if err == nil {
		t.Fatal("expected error for oversized entry, got nil")
	}
	if !errors.Is(err, ErrExceededMaxEntrySize) {
		t.Fatalf("expected ErrExceededMaxEntrySize, got: %v", err)
	}
}

func TestExtract_MaxEntrySize_NotExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "small.zip")
	content := make([]byte, 1024) // 1 KiB
	createZipWithEntry(t, zipPath, "small.bin", content)

	uz := &Unzip{
		MaxEntrySize: 2048, // 2 KiB limit — entry fits
	}
	dest := filepath.Join(tmpDir, "out")
	files, err := uz.Extract(zipPath, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestExtract_MaxTotalSize_Exceeded(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "bomb.zip")

	// 4 entries of 256 KiB each = 1 MiB total
	entries := make(map[string][]byte)
	for i := 0; i < 4; i++ {
		name := filepath.Join("entries", string(rune('a'+i))+".bin")
		entries[name] = make([]byte, 256*1024)
	}
	createZipWithEntries(t, zipPath, entries)

	uz := &Unzip{
		MaxTotalSize: 512 * 1024, // 512 KiB total limit — 4 × 256 KiB exceeds it
	}
	dest := filepath.Join(tmpDir, "out")
	_, err := uz.Extract(zipPath, dest)
	if err == nil {
		t.Fatal("expected error for total size exceeded, got nil")
	}
	if !errors.Is(err, ErrExceededMaxTotalSize) {
		t.Fatalf("expected ErrExceededMaxTotalSize, got: %v", err)
	}
}

func TestExtract_MaxTotalSize_NotExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "ok.zip")

	entries := make(map[string][]byte)
	entries["a.bin"] = make([]byte, 100)
	entries["b.bin"] = make([]byte, 100)
	createZipWithEntries(t, zipPath, entries)

	uz := &Unzip{
		MaxTotalSize: 1024, // 1 KiB total — 200 bytes fits
	}
	dest := filepath.Join(tmpDir, "out")
	files, err := uz.Extract(zipPath, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestExtract_BothLimits(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "bomb.zip")

	// Single entry of 1 MiB — exceeds both per-entry and total
	createZipWithEntry(t, zipPath, "huge.bin", make([]byte, 1<<20))

	uz := &Unzip{
		MaxEntrySize: 128 * 1024,
		MaxTotalSize: 64 * 1024 * 1024,
	}
	dest := filepath.Join(tmpDir, "out")
	_, err := uz.Extract(zipPath, dest)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// per-entry should trigger first
	if !errors.Is(err, ErrExceededMaxEntrySize) {
		t.Fatalf("expected ErrExceededMaxEntrySize, got: %v", err)
	}
}

func TestExtract_BackwardCompatible(t *testing.T) {
	// New() returns zero-valued limits = no limits, same as old behavior
	uz := New()
	if uz.MaxEntrySize != 0 || uz.MaxTotalSize != 0 {
		t.Fatal("New() should return zero limits by default")
	}
}

func TestExtract_ZipBomb_HighCompressionRatio(t *testing.T) {
	// Simulate a zip-bomb: highly compressible content that decompresses far
	// beyond the allowed limit.
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "zipbomb.zip")

	// 10 MiB of zeros — compresses to a few KB
	bigContent := make([]byte, 10<<20)
	createZipWithEntry(t, zipPath, "bomb.bin", bigContent)

	// Verify the compressed zip is much smaller than the decompressed content
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1<<20 {
		t.Logf("warning: zip file is %d bytes, expected much smaller", info.Size())
	}

	uz := &Unzip{
		MaxEntrySize: 128 * 1024,   // 128 KiB per entry
		MaxTotalSize: 64 * 1 << 20, // 64 MiB total
	}
	dest := filepath.Join(tmpDir, "out")
	_, err = uz.Extract(zipPath, dest)
	if err == nil {
		t.Fatal("expected error for zip bomb, got nil")
	}
	if !errors.Is(err, ErrExceededMaxEntrySize) {
		t.Fatalf("expected ErrExceededMaxEntrySize, got: %v", err)
	}

	// Verify the written file did NOT grow to 10 MiB — the LimitReader
	// should have capped it at MaxEntrySize+1 bytes read.
	outPath := filepath.Join(dest, "bomb.bin")
	outInfo, err := os.Stat(outPath)
	if err != nil {
		// File might not exist if error was caught early enough
		return
	}
	if outInfo.Size() > 256*1024 {
		t.Fatalf("zip bomb protection failed: output file is %d bytes, expected ≤%d", outInfo.Size(), 256*1024)
	}
}

func TestExtract_CloseError_NoPanic(t *testing.T) {
	// Build a valid zip and then corrupt the DEFLATE stream so that the
	// decompressor surfaces an error when the entry reader is closed
	// (Go's archive/zip verifies the CRC-32 on Close).  Before the fix
	// this triggered a panic; now it must return an error.

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "bad.zip")

	// Step 1: create a legitimate zip with one compressed entry.
	content := bytes.Repeat([]byte("A"), 4096) // easily compressible
	createZipWithEntry(t, zipPath, "entry.bin", content)

	// Step 2: tamper with the stored CRC-32 in the central directory header
	// so that archive/zip detects a checksum mismatch on Close().
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// Central directory header signature is PK\x01\x02; CRC-32 is at
	// offset 16 from it.
	sig := []byte{0x50, 0x4b, 0x01, 0x02}
	idx := bytes.Index(data, sig)
	if idx < 0 {
		t.Fatal("could not find central directory header signature")
	}
	crcOff := idx + 16
	origCRC := binary.LittleEndian.Uint32(data[crcOff : crcOff+4])
	binary.LittleEndian.PutUint32(data[crcOff:crcOff+4], origCRC^0xDEADBEEF)
	if err := os.WriteFile(zipPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 3: extract — must return an error, NOT panic.
	uz := New()
	dest := filepath.Join(tmpDir, "out")
	_, err = uz.Extract(zipPath, dest)
	if err == nil {
		t.Fatal("expected error from corrupted zip entry close, got nil")
	}
	t.Logf("got expected error: %v", err)
}
