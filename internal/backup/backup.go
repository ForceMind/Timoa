// Package backup implements consistent backup and verified restore.
//
// 一致性：快照用 SQLite VACUUM INTO（不是复制正在写入的主库文件）。
// 包内容：数据库快照 + 附件 + manifest（格式版本、校验和、恢复说明）。
// 未完成包不标记成功；备份后做完整性校验。加密用 age（成熟库），
// 未配置密钥时明确 unencrypted 状态，不生成假加密文件。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
)

const FormatVersion = 1

type Manifest struct {
	FormatVersion int               `json:"format_version"`
	App           string            `json:"app"`
	CreatedAt     string            `json:"created_at"`
	Encrypted     bool              `json:"encrypted"`
	Files         map[string]string `json:"files"` // 相对路径 → sha256
	Note          string            `json:"note"`
}

// Create 生成一致性备份包（.tar.gz 或 .tar.gz.age），返回包路径。
// passphrase 非空时用 age 口令加密；口令不放同一个备份包。
func Create(db *sql.DB, dataDir, passphrase string, now time.Time) (string, error) {
	stamp := now.Format("20060102-150405")
	tmp, err := os.MkdirTemp("", "xz-backup-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	files := map[string]string{}
	put := func(rel, abs string) error {
		sum, err := fileSHA256(abs)
		if err != nil {
			return err
		}
		files[rel] = sum
		return nil
	}

	// 1) 一致性快照（SQLite 官方推荐 VACUUM INTO）
	snapPath := filepath.Join(tmp, "xiaozhang.db")
	if _, err := db.Exec(`VACUUM INTO ?`, snapPath); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	// 快照完整性校验（失败不产出包）
	if err := checkDB(snapPath); err != nil {
		return "", fmt.Errorf("snapshot integrity: %w", err)
	}
	if err := put("xiaozhang.db", snapPath); err != nil {
		return "", err
	}

	// 2) 附件
	attDir := filepath.Join(dataDir, "attachments")
	if entries, err := os.ReadDir(attDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(attDir, e.Name())
			dst := filepath.Join(tmp, "attachments", e.Name())
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return "", err
			}
			if err := copyFile(src, dst); err != nil {
				return "", err
			}
			if err := put("attachments/"+e.Name(), dst); err != nil {
				return "", err
			}
		}
	}

	// 3) manifest
	m := Manifest{
		FormatVersion: FormatVersion, App: "xiaozhang",
		CreatedAt: now.UTC().Format(time.RFC3339),
		Encrypted: passphrase != "",
		Files:     files,
		Note:      "恢复：xiaozhang restore <此包> -data <数据目录>；口令不随包保存。",
	}
	mbuf, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), mbuf, 0o600); err != nil {
		return "", err
	}

	// 4) 打包（先写临时文件，完整后再改名 —— 未完成包不标记成功）
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	name := "xiaozhang-" + stamp + ".tar.gz"
	if m.Encrypted {
		name += ".age"
	}
	final := filepath.Join(backupDir, name)
	tmpOut := final + ".part"
	out, err := os.Create(tmpOut)
	if err != nil {
		return "", err
	}
	var w io.Writer = out
	var ageW io.WriteCloser
	if m.Encrypted {
		rcpt, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			out.Close()
			os.Remove(tmpOut)
			return "", err
		}
		ageW, err = age.Encrypt(out, rcpt)
		if err != nil {
			out.Close()
			os.Remove(tmpOut)
			return "", err
		}
		w = ageW
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := writeTar(tw, tmp); err != nil {
		tw.Close(); gz.Close(); out.Close()
		os.Remove(tmpOut)
		return "", err
	}
	if err := tw.Close(); err != nil {
		out.Close(); os.Remove(tmpOut)
		return "", err
	}
	if err := gz.Close(); err != nil {
		out.Close(); os.Remove(tmpOut)
		return "", err
	}
	if ageW != nil {
		if err := ageW.Close(); err != nil {
			out.Close(); os.Remove(tmpOut)
			return "", err
		}
	}
	if err := out.Sync(); err != nil {
		out.Close(); os.Remove(tmpOut)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpOut)
		return "", err
	}
	if err := os.Rename(tmpOut, final); err != nil {
		os.Remove(tmpOut)
		return "", err
	}
	return final, nil
}

// Status 返回最近备份信息（诊断页用）。
type Status struct {
	LastBackup  string `json:"last_backup,omitempty"`
	LastFile    string `json:"last_file,omitempty"`
	Encrypted   bool   `json:"encrypted"`
	BackupCount int    `json:"backup_count"`
}

func LatestStatus(dataDir string) Status {
	dir := filepath.Join(dataDir, "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Status{}
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "xiaozhang-") && !strings.HasSuffix(e.Name(), ".part") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	st := Status{BackupCount: len(names)}
	if len(names) > 0 {
		st.LastFile = names[len(names)-1]
		st.Encrypted = strings.HasSuffix(st.LastFile, ".age")
		if fi, err := os.Stat(filepath.Join(dir, st.LastFile)); err == nil {
			st.LastBackup = fi.ModTime().Format(time.RFC3339)
		}
	}
	return st
}

// Prune 保留最近 keep 个包。
func Prune(dataDir string, keep int) error {
	if keep <= 0 {
		keep = 14
	}
	dir := filepath.Join(dataDir, "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "xiaozhang-") && !strings.HasSuffix(e.Name(), ".part") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		// 磁盘不足时不得删掉唯一可恢复的好备份：至少保留 1 个
		if len(names) <= 1 {
			break
		}
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// ---------------------------------------------------------------------------
// 恢复：独立临时目录校验 → 调用方负责停服换文件
// ---------------------------------------------------------------------------

// ValidateAndExtract 解包到临时目录并校验：格式版本、校验和、数据库
// 完整性、外键、账务平衡、附件存在性。返回临时目录（调用方负责清理）。
func ValidateAndExtract(pkgPath, passphrase string) (string, error) {
	f, err := os.Open(pkgPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(pkgPath, ".age") {
		if passphrase == "" {
			return "", errors.New("backup is encrypted; passphrase required")
		}
		id, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return "", err
		}
		dr, err := age.Decrypt(f, id)
		if err != nil {
			return "", fmt.Errorf("decrypt: %w", err)
		}
		r = dr
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tmp, err := os.MkdirTemp("", "xz-restore-*")
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			os.RemoveAll(tmp)
			return "", err
		}
		// 防路径穿越
		name := filepath.Clean(hdr.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) || strings.Contains(name, ".."+string(filepath.Separator)) {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}
		dst := filepath.Join(tmp, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o700); err != nil {
				os.RemoveAll(tmp)
				return "", err
			}
		case tar.TypeReg:
			if hdr.Size > 2<<30 {
				os.RemoveAll(tmp)
				return "", fmt.Errorf("file too large in archive: %q", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				os.RemoveAll(tmp)
				return "", err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				os.RemoveAll(tmp)
				return "", err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, hdr.Size+1)); err != nil {
				out.Close()
				os.RemoveAll(tmp)
				return "", err
			}
			out.Close()
		}
	}

	// 校验 manifest
	mbuf, err := os.ReadFile(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("manifest missing: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(mbuf, &m); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	if m.FormatVersion > FormatVersion {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("unsupported format version %d", m.FormatVersion)
	}
	for rel, want := range m.Files {
		got, err := fileSHA256(filepath.Join(tmp, rel))
		if err != nil {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("missing file %s: %w", rel, err)
		}
		if got != want {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("checksum mismatch: %s", rel)
		}
	}
	// 数据库完整性 + 外键 + 账务平衡
	if err := checkDB(filepath.Join(tmp, "xiaozhang.db")); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("database integrity: %w", err)
	}
	return tmp, nil
}

// checkDB 打开快照做完整性、外键与分录平衡检查。
func checkDB(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity_check: %s", integrity)
	}
	var fkViolations int
	if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_foreign_key_check`).Scan(&fkViolations); err != nil {
		return err
	}
	if fkViolations > 0 {
		return fmt.Errorf("foreign key violations: %d", fkViolations)
	}
	// 账务平衡：全库分录借贷合计
	var debits, credits int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN direction='debit' THEN amount_cents ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN direction='credit' THEN amount_cents ELSE 0 END),0) FROM entries`).Scan(&debits, &credits); err != nil {
		return err
	}
	if debits != credits {
		return fmt.Errorf("unbalanced entries: debit %d credit %d", debits, credits)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeTar(tw *tar.Writer, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}
