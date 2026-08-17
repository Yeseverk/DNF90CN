package pvf

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReplaceScriptStringMagicOnlyChangesStringTokens(t *testing.T) {
	raw := make([]byte, 20)
	raw[0] = 6
	binary.LittleEndian.PutUint32(raw[1:5], 100)
	raw[5] = 0
	binary.LittleEndian.PutUint32(raw[6:10], 100)
	raw[10] = 3
	binary.LittleEndian.PutUint32(raw[11:15], 100)
	raw[15] = 6
	binary.LittleEndian.PutUint32(raw[16:20], 200)
	if changed := replaceScriptStringMagic(raw, 100, 300); changed != 2 {
		t.Fatalf("changed=%d want=2", changed)
	}
	if binary.LittleEndian.Uint32(raw[1:5]) != 300 ||
		binary.LittleEndian.Uint32(raw[6:10]) != 100 ||
		binary.LittleEndian.Uint32(raw[11:15]) != 300 ||
		binary.LittleEndian.Uint32(raw[16:20]) != 200 {
		t.Fatalf("raw=%x", raw)
	}
}

func TestFindStringMagic(t *testing.T) {
	archive := &Archive{strA: []byte("\x00alpha_beta\x00gamma_delta\x00")}
	magic, ok := archive.findStringMagic("alpha_beta")
	if !ok || magic != 2 {
		t.Fatalf("alpha_beta magic=%d ok=%v want=2", magic, ok)
	}
	magic, ok = archive.findStringMagic("gamma_delta")
	if !ok || magic != 12<<1 {
		t.Fatalf("gamma_delta magic=%d ok=%v want=%d", magic, ok, 12<<1)
	}
	// "alph" 只是 "alpha_beta" 的前缀，后面不是 NUL，不得命中。
	if _, ok = archive.findStringMagic("alph"); ok {
		t.Fatal("alph must not match a non-terminated prefix")
	}
	if _, ok = archive.findStringMagic("missing"); ok {
		t.Fatal("missing must not match")
	}
}

// buildSyntheticNKPIArchive 用包内加密助手构造一个最小合法 nkpi 归档：
// 1 个 type-1 脚本文件 dir/test.ani，字符串池含 patch_old/patch_new/unused_str。
// 加密函数均为对合（XOR 密钥流），直接复用即可完成“加密”方向。
func buildSyntheticNKPIArchive(t *testing.T) []byte {
	t.Helper()

	pool := []byte{0}
	poolOffset := func(value string) int {
		off := len(pool)
		pool = append(pool, []byte(value)...)
		pool = append(pool, 0)
		return off
	}
	oldOff := poolOffset("patch_old")
	poolOffset("patch_new")
	poolOffset("unused_str")
	dirOff := poolOffset("dir")
	nameOff := poolOffset("test.ani")

	// 脚本 token：行内字符串(old)、整数、标签(old)、行内字符串(dir)。
	script := make([]byte, 0, 20)
	appendToken := func(tokenType byte, value int) {
		script = append(script, tokenType, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(script[len(script)-4:], uint32(int32(value)))
	}
	appendToken(6, oldOff<<1)
	appendToken(0, 42)
	appendToken(3, oldOff<<1)
	appendToken(6, dirOff<<1)

	encoder := &Archive{format: FormatNKPI}

	// name 段：8 字节前缀 + sTrA/sTrW 字符串池压缩加密缓冲。
	encodeStringBuffer := func(key string, xorConst uint32, raw []byte) []byte {
		compressed, err := compressZlib(raw)
		if err != nil {
			t.Fatal(err)
		}
		decrypt2(key, compressed)
		out := make([]byte, 8, 8+len(compressed))
		binary.LittleEndian.PutUint32(out[0:4], uint32(len(compressed))^xorConst)
		binary.LittleEndian.PutUint32(out[4:8], uint32(len(raw)))
		return append(out, compressed...)
	}
	nameSection := make([]byte, 8)
	nameSection = append(nameSection, encodeStringBuffer("sTrA", 0xAA74472E, pool)...)
	nameSection = append(nameSection, encodeStringBuffer("sTrW", 0x9A82F037, []byte{0, 0})...)

	fileTable := make([]byte, fileItemSize)
	binary.LittleEndian.PutUint32(fileTable[0:4], uint32(nameOff<<1))
	binary.LittleEndian.PutUint32(fileTable[4:8], uint32(dirOff<<1))
	binary.LittleEndian.PutUint32(fileTable[8:12], 0) // chunkIndex
	binary.LittleEndian.PutUint32(fileTable[12:16], 0)
	binary.LittleEndian.PutUint32(fileTable[16:20], uint32(len(script)))
	binary.LittleEndian.PutUint32(fileTable[20:24], 1) // dataType script

	body, err := encoder.encryptChunk(script)
	if err != nil {
		t.Fatal(err)
	}
	groupTable := encoder.encodeGroups([]groupItem{{compressedSize: len(body), originalSize: len(script)}})

	var plain [headerSize]byte
	copy(plain[0:4], []byte("nkpi"))
	header := pvfHeader{
		plain:      plain,
		fileCount:  1,
		bodySize:   len(body),
		groupCount: 1,
		hashSize:   0,
		nameSize:   len(nameSection),
	}
	out := encoder.encodeHeader(header)
	out = append(out, fileTable...)
	out = append(out, nameSection...)
	out = append(out, groupTable...)
	out = append(out, body...)
	return out
}

func TestPatchScriptStringSyntheticArchive(t *testing.T) {
	raw := buildSyntheticNKPIArchive(t)
	archive, err := OpenBytes(raw)
	if err != nil {
		t.Fatalf("open synthetic archive: %v", err)
	}
	if archive.Format() != FormatNKPI {
		t.Fatalf("format=%q want %q", archive.Format(), FormatNKPI)
	}

	patched, report, err := archive.PatchScriptString([]string{"dir/test.ani"}, "patch_old", "patch_new")
	if err != nil {
		t.Fatalf("PatchScriptString: %v", err)
	}
	if report.FilesChanged != 1 || report.TokensChanged != 2 || report.ChunksChanged != 1 {
		t.Fatalf("report=%+v want {1 2 1}", report)
	}

	verified, err := OpenBytes(patched)
	if err != nil {
		t.Fatalf("reopen patched: %v", err)
	}
	text, err := verified.ReadText("dir/test.ani")
	if err != nil {
		t.Fatalf("read patched text: %v", err)
	}
	if !strings.Contains(text, "`patch_new`") || strings.Contains(text, "patch_old") {
		t.Fatalf("patched text=%q", text)
	}
	// 无关 token（dir）与原归档文本必须保持不变。
	if !strings.Contains(text, "`dir`") {
		t.Fatalf("patched text lost untouched token: %q", text)
	}
	originalText, err := archive.ReadText("dir/test.ani")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(originalText, "`patch_old`") {
		t.Fatalf("source archive mutated in memory: %q", originalText)
	}
}

func TestPatchScriptStringErrors(t *testing.T) {
	archive, err := OpenBytes(buildSyntheticNKPIArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = archive.PatchScriptString([]string{"dir/test.ani"}, "patch_old", "patch_old"); err == nil {
		t.Fatal("same old/new must fail")
	}
	if _, _, err = archive.PatchScriptString([]string{"dir/test.ani"}, "absent_old", "patch_new"); err == nil {
		t.Fatal("absent old value must fail")
	}
	if _, _, err = archive.PatchScriptString([]string{"dir/test.ani"}, "patch_old", "absent_new"); err == nil {
		t.Fatal("absent new value must fail")
	}
	if _, _, err = archive.PatchScriptString([]string{"dir/missing.ani"}, "patch_old", "patch_new"); err == nil {
		t.Fatal("missing file must fail")
	}
	// unused_str 在池中但脚本未引用，必须报错而不是产出空补丁。
	if _, _, err = archive.PatchScriptString([]string{"dir/test.ani"}, "unused_str", "patch_new"); err == nil {
		t.Fatal("unreferenced old value must fail")
	}
}

var asciiTokenPattern = regexp.MustCompile("`([^`]{3,40})`")

func isPrintableASCII(value string) bool {
	for idx := 0; idx < len(value); idx++ {
		if value[idx] < 0x20 || value[idx] > 0x7e {
			return false
		}
	}
	return true
}

// TestRealScriptPVFPatchScriptStringRoundTrip 对真实 Script.pvf 的 TempDir
// 副本执行字符串引用补丁并回开验证；原始文件绝不写入。门控：
// DNFBRIDGE_REAL_PVF_SMOKE=D:/DNF/runtime/data/dnf/Script.pvf。
func TestRealScriptPVFPatchScriptStringRoundTrip(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real PVF script string patching")
	}

	sourceSum := sha256File(t, pvfPath)
	tmpCopy := filepath.Join(t.TempDir(), "Script.pvf")
	copyFile(t, pvfPath, tmpCopy)
	copySumBefore := sha256File(t, tmpCopy)

	archive, err := LoadArchive(Options{Path: tmpCopy})
	if err != nil {
		t.Fatal(err)
	}

	// 动态挑选一对 ASCII 池字符串：old 必须是某脚本实际引用且只以
	// 完整 token 形式出现（避免子串误判），new 必须在池中且与 old 无子串关系。
	var poolTokens []string
	seenToken := make(map[string]bool)
	collect := func(token string) {
		if seenToken[token] || !isPrintableASCII(token) {
			return
		}
		if _, ok := archive.findStringMagic(token); !ok {
			return
		}
		seenToken[token] = true
		poolTokens = append(poolTokens, token)
	}

	const scanBudget = 4000
	var patchPath, oldValue, newValue string
	scanned := 0
files:
	for _, file := range archive.Files() {
		if file.DataType != 1 {
			continue
		}
		text, err := archive.ReadText(file.ArchivePath)
		if err != nil {
			continue
		}
		scanned++
		for _, match := range asciiTokenPattern.FindAllStringSubmatch(text, -1) {
			collect(match[1])
		}
		for _, line := range strings.Split(text, "\n") {
			if len(line) >= 3 {
				collect(line)
			}
		}
		for _, token := range poolTokens {
			occurrences := strings.Count(text, "`"+token+"`") + strings.Count(text, "\n"+token+"\n")
			if occurrences == 0 || strings.Count(text, token) != occurrences {
				continue
			}
			for _, candidate := range poolTokens {
				if candidate == token || strings.Contains(candidate, token) || strings.Contains(token, candidate) {
					continue
				}
				patchPath, oldValue, newValue = file.ArchivePath, token, candidate
				break files
			}
		}
		if scanned >= scanBudget {
			break
		}
	}
	if patchPath == "" {
		t.Skipf("no ASCII string-pool token pair found within %d scripts", scanned)
	}
	t.Logf("patching %s: %q -> %q (scanned %d scripts, pool tokens %d)", patchPath, oldValue, newValue, scanned, len(poolTokens))

	patched, report, err := archive.PatchScriptString([]string{patchPath}, oldValue, newValue)
	if err != nil {
		t.Fatalf("PatchScriptString: %v", err)
	}
	if report.FilesChanged != 1 || report.TokensChanged < 1 || report.ChunksChanged < 1 {
		t.Fatalf("report=%+v", report)
	}
	t.Logf("report=%+v", report)

	verified, err := OpenBytes(patched)
	if err != nil {
		t.Fatalf("reopen patched bytes: %v", err)
	}
	text, err := verified.ReadText(patchPath)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if !strings.Contains(text, newValue) || strings.Contains(text, oldValue) {
		t.Fatalf("patched text verification failed for %s", patchPath)
	}

	// 落盘一次再回开，验证输出文件作为独立归档可用。
	outPath := filepath.Join(t.TempDir(), "Script.patched.pvf")
	if err := os.WriteFile(outPath, patched, 0o644); err != nil {
		t.Fatal(err)
	}
	diskArchive, err := LoadArchive(Options{Path: outPath})
	if err != nil {
		t.Fatalf("load patched archive from disk: %v", err)
	}
	diskText, err := diskArchive.ReadText(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diskText, newValue) || strings.Contains(diskText, oldValue) {
		t.Fatalf("disk round-trip verification failed for %s", patchPath)
	}

	// 输入副本与原始 PVF 在补丁前后必须字节级不变。
	if got := sha256File(t, tmpCopy); got != copySumBefore {
		t.Fatal("temp copy was modified by patching")
	}
	if got := sha256File(t, pvfPath); got != sourceSum {
		t.Fatal("original PVF was modified by patching")
	}
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
