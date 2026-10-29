package secretfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectRoundTrip(t *testing.T) {
	secret := []byte(`{"mpay_token":"1-eyJ...敏感凭证..."}`)
	enc, err := Protect(secret)
	if err != nil {
		t.Fatalf("Protect: %v", err)
	}
	if bytes.Contains(enc, secret) {
		t.Fatal("密文里出现了明文")
	}
	dec, err := Unprotect(enc)
	if err != nil {
		t.Fatalf("Unprotect: %v", err)
	}
	if !bytes.Equal(dec, secret) {
		t.Fatal("往返不一致")
	}
}

func TestReadWriteUpgrade(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "acc.json")

	// 旧明文文件 → ReadFile 原样返回
	plain := []byte(`[{"name":"账号A"}]`)
	if err := os.WriteFile(p, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(p)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("明文应原样返回: %v %q", err, got)
	}
	if IsEncrypted(p) {
		t.Fatal("明文被误判为加密")
	}

	// 升级写加密 → 读回一致，且盘上无明文
	if err := WriteFile(p, plain, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err = ReadFile(p)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("加密后读回不一致: %v", err)
	}
	if !IsEncrypted(p) {
		t.Fatal("写后应识别为加密")
	}
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte("账号A")) {
		t.Fatal("盘上文件仍含明文")
	}
}
