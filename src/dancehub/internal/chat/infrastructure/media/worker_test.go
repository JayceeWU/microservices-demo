package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"testing"
)

type snapshotStore struct {
	objects map[string][]byte
	onCopy  func()
	opened  string
}

func (s *snapshotStore) Copy(_ context.Context, source, destination string) error {
	s.objects[destination] = append([]byte(nil), s.objects[source]...)
	if s.onCopy != nil {
		s.onCopy()
	}
	return nil
}
func (s *snapshotStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.opened = key
	return io.NopCloser(bytes.NewReader(s.objects[key])), nil
}
func (s *snapshotStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func cleanScanner(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		command := make([]byte, len("zINSTREAM\x00"))
		if _, err = io.ReadFull(connection, command); err != nil {
			return
		}
		for {
			var length uint32
			if err = binary.Read(connection, binary.BigEndian, &length); err != nil {
				return
			}
			if length == 0 {
				break
			}
			if _, err = io.CopyN(io.Discard, connection, int64(length)); err != nil {
				return
			}
		}
		_, _ = connection.Write([]byte("stream: OK\x00"))
	}()
	return listener.Addr().String()
}

func TestScanCandidateCannotFollowUploadOverwrite(t *testing.T) {
	original := []byte("GIF89a-original-safe-image")
	hash := sha256.Sum256(original)
	store := &snapshotStore{objects: map[string][]byte{"quarantine/upload": original}}
	store.onCopy = func() { store.objects["quarantine/upload"] = []byte("replacement-after-copy") }
	w := Worker{Objects: store, ClamAV: cleanScanner(t)}
	request := scanRequest{ObjectKey: "quarantine/upload", MimeType: "image/gif", SizeBytes: int64(len(original)), SHA256: hex.EncodeToString(hash[:])}
	clean, reason, err := w.scanCandidate(context.Background(), request, "clean/unique-attempt")
	if err != nil || !clean {
		t.Fatalf("snapshot scan: clean=%v reason=%q err=%v", clean, reason, err)
	}
	if store.opened != "clean/unique-attempt" {
		t.Fatalf("scanned browser-writable key %q", store.opened)
	}
	store.objects["quarantine/upload"] = []byte("replacement-after-CLEAN")
	if !bytes.Equal(store.objects["clean/unique-attempt"], original) {
		t.Fatal("published snapshot changed with upload")
	}
}

func TestScanCandidateRejectsChangedBytesBeforeSnapshot(t *testing.T) {
	original := []byte("GIF89a-original-safe-image")
	hash := sha256.Sum256(original)
	store := &snapshotStore{objects: map[string][]byte{"quarantine/upload": []byte("GIF89a-replacement-image")}}
	w := Worker{Objects: store, ClamAV: cleanScanner(t)}
	request := scanRequest{ObjectKey: "quarantine/upload", MimeType: "image/gif", SizeBytes: int64(len(original)), SHA256: hex.EncodeToString(hash[:])}
	clean, reason, err := w.scanCandidate(context.Background(), request, "clean/other-attempt")
	if err != nil || clean || reason != "sha256 mismatch" {
		t.Fatalf("clean=%v reason=%q err=%v", clean, reason, err)
	}
}

func TestValidMagic(t *testing.T) {
	tests := []struct {
		name, mime string
		header     []byte
		valid      bool
	}{
		{"jpeg", "image/jpeg", []byte{0xff, 0xd8, 0xff}, true},
		{"png", "image/png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, true},
		{"gif", "image/gif", []byte("GIF89a"), true},
		{"webp", "image/webp", []byte("RIFF0000WEBP"), true},
		{"mp4", "video/mp4", []byte("0000ftypisom"), true},
		{"mime spoof", "image/png", []byte("GIF89a"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := validMagic(test.mime, test.header); actual != test.valid {
				t.Fatalf("validMagic()=%v, want %v", actual, test.valid)
			}
		})
	}
}

func TestContainsBrowserVideoCodec(t *testing.T) {
	for _, codec := range []string{"avc1", "avc3", "hvc1", "hev1", "vp09", "av01"} {
		if !containsBrowserVideoCodec([]byte("sample-entry-" + codec)) {
			t.Fatalf("expected %s to be accepted", codec)
		}
	}
	if containsBrowserVideoCodec([]byte("sample-entry-theora")) {
		t.Fatal("unexpected unsupported codec acceptance")
	}
}
