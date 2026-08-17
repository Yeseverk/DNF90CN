package hotupdate

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const BundleManifestName = "longheng_hotupdate_manifest.json"
const BundleSignatureAlgorithm = "HMAC-SHA256"

const defMaxBundleBytes int64 = 256 << 20
const maxBundleManifest int64 = 1 << 20

type BundleOptions struct {
	SigningKey []byte
	HashCache  *FileHashCache
}

type BundleVerifyOptions struct {
	Checksum         string
	SigningKey       []byte
	RequireSignature bool
	MaxBytes         int64
}

type BundleManifest struct {
	Version      string       `json:"version"`
	Files        []BundleFile `json:"files"`
	Bytes        int64        `json:"bytes"`
	Checksum     string       `json:"checksum"`
	SignatureAlg string       `json:"signature_alg,omitempty"`
	Signature    string       `json:"signature,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

type BundleFile struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Checksum string `json:"checksum"`
}

func BuildBundle(ctx context.Context, sourceDir, bundlePath, version string) (BundleManifest, error) {
	return BuildBundleWithOptions(ctx, sourceDir, bundlePath, version, BundleOptions{})
}

func BuildBundleWithOptions(ctx context.Context, sourceDir, bundlePath, version string, options BundleOptions) (BundleManifest, error) {
	if err := contextErr(ctx); err != nil {
		return BundleManifest{}, err
	}
	sourceDir = strings.TrimSpace(sourceDir)
	bundlePath = strings.TrimSpace(bundlePath)
	version = strings.TrimSpace(version)
	if sourceDir == "" {
		return BundleManifest{}, ErrSourceRequired
	}
	if bundlePath == "" {
		return BundleManifest{}, fmt.Errorf("hot update bundle path is required")
	}
	if version == "" {
		return BundleManifest{}, ErrVersionRequired
	}
	info, err := os.Lstat(sourceDir)
	if err != nil {
		return BundleManifest{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return BundleManifest{}, fmt.Errorf("hot update bundle source %q must not be a symlink", sourceDir)
	}
	if !info.IsDir() {
		return BundleManifest{}, fmt.Errorf("hot update bundle source %q is not a directory", sourceDir)
	}
	files, err := bundleFilesHash(ctx, sourceDir, options.HashCache)
	if err != nil {
		return BundleManifest{}, err
	}
	manifest := BundleManifest{
		Version:   version,
		Files:     files,
		CreatedAt: time.Now().UTC(),
	}
	manifest.Bytes, manifest.Checksum = bundleSummary(files)
	if len(options.SigningKey) > 0 {
		manifest.SignatureAlg = BundleSignatureAlgorithm
		manifest.Signature = signBundleManifest(manifest, options.SigningKey)
	}
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o750); err != nil && filepath.Dir(bundlePath) != "." {
		return BundleManifest{}, err
	}
	out, err := os.Create(bundlePath) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return BundleManifest{}, err
	}
	zipper := zip.NewWriter(out)
	closeWithErr := func(first error) error {
		if err := zipper.Close(); first == nil {
			first = err
		}
		if err := out.Close(); first == nil {
			first = err
		}
		return first
	}
	for _, file := range files {
		if err := contextErr(ctx); err != nil {
			return BundleManifest{}, closeWithErr(err)
		}
		if err := addBundleFile(zipper, sourceDir, file.Path); err != nil {
			return BundleManifest{}, closeWithErr(err)
		}
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BundleManifest{}, closeWithErr(err)
	}
	entry, err := zipper.Create(BundleManifestName)
	if err != nil {
		return BundleManifest{}, closeWithErr(err)
	}
	if _, err := entry.Write(append(manifestData, '\n')); err != nil {
		return BundleManifest{}, closeWithErr(err)
	}
	if err := closeWithErr(nil); err != nil {
		return BundleManifest{}, err
	}
	return manifest, nil
}

func VerifyBundle(ctx context.Context, bundlePath, wantChecksum string) (BundleManifest, error) {
	return VerifyBundleWithOptions(ctx, bundlePath, BundleVerifyOptions{Checksum: wantChecksum})
}

func VerifyBundleWithOptions(ctx context.Context, bundlePath string, options BundleVerifyOptions) (BundleManifest, error) {
	if err := contextErr(ctx); err != nil {
		return BundleManifest{}, err
	}
	reader, err := zip.OpenReader(strings.TrimSpace(bundlePath))
	if err != nil {
		return BundleManifest{}, err
	}
	defer func() { _ = reader.Close() }()
	maxBytes := bundleReadLimit(options)
	manifest, err := readBundleManifest(&reader.Reader)
	if err != nil {
		return BundleManifest{}, err
	}
	if err := validBundleManifest(manifest); err != nil {
		return BundleManifest{}, err
	}
	if maxBytes > 0 && manifest.Bytes > maxBytes {
		return BundleManifest{}, fmt.Errorf("hot update bundle exceeds max bytes: got %d want <= %d", manifest.Bytes, maxBytes)
	}
	if strings.TrimSpace(options.Checksum) != "" && !strings.EqualFold(options.Checksum, manifest.Checksum) {
		return BundleManifest{}, fmt.Errorf("hot update bundle checksum mismatch: got %s want %s", manifest.Checksum, options.Checksum)
	}
	actual, err := bundleManifestZip(ctx, &reader.Reader, manifest.Version, maxBytes)
	if err != nil {
		return BundleManifest{}, err
	}
	if actual.Checksum != manifest.Checksum || actual.Bytes != manifest.Bytes || !sameBundleFiles(actual.Files, manifest.Files) {
		return BundleManifest{}, fmt.Errorf("hot update bundle manifest mismatch")
	}
	if err := verifyBundleSig(manifest, options); err != nil {
		return BundleManifest{}, err
	}
	return manifest, nil
}

func ExtractBundle(ctx context.Context, bundlePath, destination, wantChecksum string) (Package, error) {
	return ExtractBundleWithOptions(ctx, bundlePath, destination, BundleVerifyOptions{Checksum: wantChecksum})
}

func ExtractBundleWithOptions(ctx context.Context, bundlePath, destination string, options BundleVerifyOptions) (Package, error) {
	if err := contextErr(ctx); err != nil {
		return Package{}, err
	}
	bundlePath = strings.TrimSpace(bundlePath)
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return Package{}, fmt.Errorf("hot update bundle destination is required")
	}
	manifest, err := VerifyBundleWithOptions(ctx, bundlePath, options)
	if err != nil {
		return Package{}, err
	}
	reader, err := zip.OpenReader(bundlePath)
	if err != nil {
		return Package{}, err
	}
	defer func() { _ = reader.Close() }()
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return Package{}, err
	}
	tempDir, err := os.MkdirTemp(parent, ".longheng-hotupdate-*")
	if err != nil {
		return Package{}, err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	if err := preflightEntries(ctx, &reader.Reader, tempDir); err != nil {
		return Package{}, err
	}
	for _, file := range reader.File {
		if err := contextErr(ctx); err != nil {
			return Package{}, err
		}
		name := filepath.ToSlash(file.Name)
		if name == BundleManifestName {
			continue
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return Package{}, fmt.Errorf("hot update bundle rejects symlink %q", name)
		}
		target, err := safeBundleTarget(tempDir, name)
		if err != nil {
			return Package{}, err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return Package{}, err
			}
			continue
		}
		if err := extractBundleFile(file, target, bundleReadLimit(options)); err != nil {
			return Package{}, err
		}
	}
	if err := replaceDirectory(destination, tempDir); err != nil {
		return Package{}, err
	}
	return Package{
		Version:      manifest.Version,
		SourceURI:    bundlePath,
		Directory:    destination,
		Checksum:     manifest.Checksum,
		Bytes:        manifest.Bytes,
		SignatureAlg: manifest.SignatureAlg,
		Signature:    manifest.Signature,
	}, nil
}

func replaceDirectory(destination, source string) error {
	backup := ""
	if _, err := os.Stat(destination); err == nil {
		backup = destination + ".rollback-" + fmt.Sprintf("%d", time.Now().UnixNano())
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		if backup != "" {
			_ = os.Rename(backup, destination)
		}
		return err
	}
	if backup != "" {
		return os.RemoveAll(backup)
	}
	return nil
}

func preflightEntries(ctx context.Context, reader *zip.Reader, destination string) error {
	// 预检逐个计算目标路径，拒绝绝对路径、.. 逃逸和非法文件名，防止 zip slip。
	for _, file := range reader.File {
		if err := contextErr(ctx); err != nil {
			return err
		}
		name := filepath.ToSlash(file.Name)
		if name == BundleManifestName {
			continue
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("hot update bundle rejects symlink %q", name)
		}
		if _, err := safeBundleTarget(destination, name); err != nil {
			return err
		}
	}
	return nil
}

func bundleFiles(ctx context.Context, root string) ([]BundleFile, error) {
	var files []BundleFile
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return ctxErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("hot update bundle rejects symlink %q", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files = append(files, BundleFile{
			Path:     filepath.ToSlash(rel),
			Bytes:    int64(len(data)),
			Checksum: hex.EncodeToString(sum[:]),
		})
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func bundleSummary(files []BundleFile) (int64, string) {
	hash := sha256.New()
	var bytes int64
	for _, file := range files {
		bytes += file.Bytes
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(file.Checksum))
		_, _ = hash.Write([]byte{0})
	}
	return bytes, hex.EncodeToString(hash.Sum(nil))
}

func signBundleManifest(manifest BundleManifest, key []byte) string {
	mac := hmac.New(sha256.New, key)
	writeSigSeed(mac, manifest)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyBundleSig(manifest BundleManifest, options BundleVerifyOptions) error {
	// 签名只覆盖 manifest 中的文件 hash，不覆盖外部元信息；生产必须配置 RequireSignature 和 signing key。
	if len(options.SigningKey) == 0 {
		if options.RequireSignature {
			return fmt.Errorf("hot update bundle signing key is required")
		}
		return nil
	}
	if strings.TrimSpace(manifest.Signature) == "" {
		return fmt.Errorf("hot update bundle signature is required")
	}
	if strings.TrimSpace(manifest.SignatureAlg) == "" {
		return fmt.Errorf("hot update bundle signature algorithm is required")
	}
	if manifest.SignatureAlg != BundleSignatureAlgorithm {
		return fmt.Errorf("hot update bundle signature algorithm %q is unsupported", manifest.SignatureAlg)
	}
	want := signBundleManifest(manifest, options.SigningKey)
	got, err := hex.DecodeString(manifest.Signature)
	if err != nil {
		return fmt.Errorf("hot update bundle signature is invalid: %w", err)
	}
	expected, _ := hex.DecodeString(want)
	if !hmac.Equal(got, expected) {
		return fmt.Errorf("hot update bundle signature mismatch")
	}
	return nil
}

func writeSigSeed(w io.Writer, manifest BundleManifest) {
	files := append([]BundleFile(nil), manifest.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	_, _ = io.WriteString(w, manifest.Version)
	_, _ = w.Write([]byte{0})
	_, _ = io.WriteString(w, manifest.SignatureAlg)
	_, _ = w.Write([]byte{0})
	_, _ = io.WriteString(w, manifest.CreatedAt.UTC().Format(time.RFC3339Nano))
	_, _ = w.Write([]byte{0})
	_, _ = io.WriteString(w, fmt.Sprintf("%d", manifest.Bytes))
	_, _ = w.Write([]byte{0})
	_, _ = io.WriteString(w, manifest.Checksum)
	_, _ = w.Write([]byte{0})
	for _, file := range files {
		_, _ = io.WriteString(w, file.Path)
		_, _ = w.Write([]byte{0})
		_, _ = io.WriteString(w, fmt.Sprintf("%d", file.Bytes))
		_, _ = w.Write([]byte{0})
		_, _ = io.WriteString(w, file.Checksum)
		_, _ = w.Write([]byte{0})
	}
}

func addBundleFile(zipper *zip.Writer, root, rel string) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(rel)
	header.Method = zip.Deflate
	entry, err := zipper.CreateHeader(header)
	if err != nil {
		return err
	}
	in, err := os.Open(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	_, err = io.Copy(entry, in)
	return err
}

func readBundleManifest(reader *zip.Reader) (BundleManifest, error) {
	data, err := readBundleZipPart(reader, BundleManifestName, maxBundleManifest)
	if err != nil {
		return BundleManifest{}, err
	}
	var manifest BundleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return BundleManifest{}, err
	}
	if manifest.Version == "" || manifest.Checksum == "" {
		return BundleManifest{}, fmt.Errorf("hot update bundle manifest is invalid")
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	return manifest, nil
}

func bundleManifestZip(ctx context.Context, reader *zip.Reader, version string, maxBytes int64) (BundleManifest, error) {
	files := make([]BundleFile, 0, len(reader.File))
	seen := make(map[string]struct{}, len(reader.File))
	var totalBytes int64
	for _, file := range reader.File {
		if err := contextErr(ctx); err != nil {
			return BundleManifest{}, err
		}
		name := filepath.ToSlash(file.Name)
		if name == BundleManifestName || file.FileInfo().IsDir() {
			continue
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return BundleManifest{}, fmt.Errorf("hot update bundle rejects symlink %q", name)
		}
		if err := validateBundlePath(name); err != nil {
			return BundleManifest{}, err
		}
		name = path.Clean(name)
		if _, ok := seen[name]; ok {
			return BundleManifest{}, fmt.Errorf("hot update bundle path %q is duplicated", name)
		}
		seen[name] = struct{}{}
		data, err := readBundleZipPart(reader, name, maxBytes)
		if err != nil {
			return BundleManifest{}, err
		}
		if maxBytes > 0 && totalBytes > maxBytes-int64(len(data)) {
			return BundleManifest{}, fmt.Errorf("hot update bundle exceeds max bytes: got > %d", maxBytes)
		}
		totalBytes += int64(len(data))
		sum := sha256.Sum256(data)
		files = append(files, BundleFile{Path: name, Bytes: int64(len(data)), Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	bytes, checksum := bundleSummary(files)
	return BundleManifest{Version: version, Files: files, Bytes: bytes, Checksum: checksum}, nil
}

func readBundleZipPart(reader *zip.Reader, name string, maxBytes int64) ([]byte, error) {
	for _, file := range reader.File {
		if filepath.ToSlash(file.Name) != filepath.ToSlash(name) {
			continue
		}
		if maxBytes > 0 && file.UncompressedSize64 > uint64(maxBytes) {
			return nil, fmt.Errorf("hot update bundle part %s exceeds max bytes: got %d want <= %d", name, file.UncompressedSize64, maxBytes)
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return readAllLimited(rc, maxBytes, "hot update bundle part "+name)
	}
	return nil, fmt.Errorf("hot update bundle part %s not found", name)
}

func readAllLimited(r io.Reader, maxBytes int64, label string) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defMaxBundleBytes
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds max bytes: got > %d", label, maxBytes)
	}
	return data, nil
}

func bundleReadLimit(options BundleVerifyOptions) int64 {
	if options.MaxBytes > 0 {
		return options.MaxBytes
	}
	return defMaxBundleBytes
}

func safeBundleTarget(root, name string) (string, error) {
	// 所有 bundle 文件最终路径必须仍在 root 下，防止通过清理后的相对路径写出工作区。
	if err := validateBundlePath(name); err != nil {
		return "", err
	}
	name = path.Clean(filepath.ToSlash(name))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(absRoot, filepath.FromSlash(name)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, target)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("hot update bundle target %q escapes %q", target, absRoot)
	}
	return target, nil
}

func validBundleManifest(manifest BundleManifest) error {
	if strings.TrimSpace(manifest.Version) == "" || strings.TrimSpace(manifest.Checksum) == "" {
		return fmt.Errorf("hot update bundle manifest is invalid")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for idx, file := range manifest.Files {
		if err := validateBundlePath(file.Path); err != nil {
			return fmt.Errorf("hot update bundle manifest file %d: %w", idx, err)
		}
		clean := path.Clean(filepath.ToSlash(file.Path))
		if _, ok := seen[clean]; ok {
			return fmt.Errorf("hot update bundle manifest file %q is duplicated", clean)
		}
		seen[clean] = struct{}{}
		if file.Bytes < 0 {
			return fmt.Errorf("hot update bundle manifest file %q has negative bytes", clean)
		}
		if len(file.Checksum) != sha256.Size*2 {
			return fmt.Errorf("hot update bundle manifest file %q has invalid checksum", clean)
		}
		if _, err := hex.DecodeString(file.Checksum); err != nil {
			return fmt.Errorf("hot update bundle manifest file %q has invalid checksum: %w", clean, err)
		}
	}
	return nil
}

func validateBundlePath(name string) error {
	raw := strings.TrimSpace(filepath.ToSlash(name))
	if raw == "" || raw == "." || strings.HasPrefix(raw, "/") || filepath.IsAbs(name) || filepath.VolumeName(filepath.FromSlash(raw)) != "" {
		return fmt.Errorf("hot update bundle path %q is invalid", name)
	}
	clean := path.Clean(raw)
	trimmedRaw := strings.TrimSuffix(raw, "/")
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != trimmedRaw {
		return fmt.Errorf("hot update bundle path %q is invalid", name)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("hot update bundle path %q is invalid", name)
		}
	}
	return nil
}

func sameBundleFiles(left, right []BundleFile) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]BundleFile(nil), left...)
	right = append([]BundleFile(nil), right...)
	sort.Slice(left, func(i, j int) bool { return left[i].Path < left[j].Path })
	sort.Slice(right, func(i, j int) bool { return right[i].Path < right[j].Path })
	for idx := range left {
		leftPath := path.Clean(filepath.ToSlash(left[idx].Path))
		rightPath := path.Clean(filepath.ToSlash(right[idx].Path))
		if leftPath != rightPath || left[idx].Bytes != right[idx].Bytes || !strings.EqualFold(left[idx].Checksum, right[idx].Checksum) {
			return false
		}
	}
	return true
}

func extractBundleFile(file *zip.File, target string, maxBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	if maxBytes > 0 && file.UncompressedSize64 > uint64(maxBytes) {
		return fmt.Errorf("hot update bundle file %s exceeds max bytes: got %d want <= %d", filepath.ToSlash(file.Name), file.UncompressedSize64, maxBytes)
	}
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.FileInfo().Mode()) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return err
	}
	if maxBytes <= 0 {
		maxBytes = defMaxBundleBytes
	}
	written, err := io.Copy(out, io.LimitReader(rc, maxBytes+1))
	if err != nil {
		_ = out.Close()
		return err
	}
	if written > maxBytes {
		_ = out.Close()
		return fmt.Errorf("hot update bundle file %s exceeds max bytes: got > %d", filepath.ToSlash(file.Name), maxBytes)
	}
	return out.Close()
}
