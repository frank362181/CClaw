/**
* StartupFileRepository 启动文件读取(GetStartupFiles / GetStartupFile)的单元测试
*
* 覆盖:清单元数据、单文件读取、缺失文件占位、白名单拦截、目录穿越拦截、超大文件拦截、缓存失效。
**/

package repo

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/platformutility/exception"
)

// writeRepoFile 创建测试工作区文件
func writeRepoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error:%s", err.Error())
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file error:%s", err.Error())
	}
}

func TestGetStartupFiles(t *testing.T) {
	root := t.TempDir()

	// 仅创建 AGENTS.md 与 SOUL.md,其余启动文件缺失
	writeRepoFile(t, filepath.Join(root, hCommon.AgentDocFileName), "# Rules\n")
	writeRepoFile(t, filepath.Join(root, hCommon.SoulDocFileName), "# Soul\n")

	repo := GetAgentFileRepository()
	list := repo.GetStartupFiles(root)

	if len(list) != len(hCommon.StartupDocFiles) {
		t.Fatalf("startup file count mismatch: got=%d want=%d", len(list), len(hCommon.StartupDocFiles))
	}

	// 顺序与白名单一致,且 AGENTS/SOUL 存在、其余缺失
	for i, item := range hCommon.StartupDocFiles {
		if list[i].FileName != item.Name || list[i].Kind != item.Kind || list[i].Required != item.Required {
			t.Fatalf("index %d mismatch: got=%+v want=(%s,%v,%v)", i, list[i], item.Name, item.Kind, item.Required)
		}
	}

	existFlags := map[string]bool{}
	for _, doc := range list {
		existFlags[doc.FileName] = !doc.Missing
		if len(doc.Content) != 0 {
			t.Errorf("list item %s should not carry content", doc.FileName)
		}
		if !doc.Missing && doc.ByteSize <= 0 {
			t.Errorf("file %s should have positive byteSize", doc.FileName)
		}
	}
	if !existFlags[hCommon.AgentDocFileName] || !existFlags[hCommon.SoulDocFileName] {
		t.Errorf("AGENTS.md/SOUL.md should exist: %+v", existFlags)
	}
	if existFlags[hCommon.ToolsDocFileName] || existFlags[hCommon.IdentityDocFileName] {
		t.Errorf("TOOLS.md/IDENTITY.md should be missing: %+v", existFlags)
	}
}

func TestGetStartupFile(t *testing.T) {
	root := t.TempDir()
	content := "---\nsummary: \"rules\"\n---\n# Rules\n- Read before edit.\n"
	writeRepoFile(t, filepath.Join(root, hCommon.AgentDocFileName), content)

	repo := GetAgentFileRepository()
	file, err := repo.GetStartupFile(root, hCommon.AgentDocFileName)
	if err != nil {
		t.Fatalf("GetStartupFile error:%s", err.Error())
	}

	if file == nil {
		t.Fatal("GetStartupFile returned nil")
	}
	if file.Kind != hCommon.AgentDocAgent || file.Required != true || file.Missing {
		t.Errorf("unexpected file meta: %+v", file)
	}
	if file.Content != content {
		t.Errorf("content mismatch: got=%q want=%q", file.Content, content)
	}
	if file.ByteSize != int64(len(content)) {
		t.Errorf("byteSize mismatch: got=%d want=%d", file.ByteSize, len(content))
	}
}

func TestGetStartupFileMissing(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, hCommon.AgentDocFileName), "# Rules\n")

	repo := GetAgentFileRepository()
	file, err := repo.GetStartupFile(root, hCommon.ToolsDocFileName) // 可选文件缺失
	if err != nil {
		t.Fatalf("GetStartupFile error:%s", err.Error())
	}
	if !file.Missing || len(file.Content) != 0 {
		t.Errorf("missing file should return Missing=true with empty content: %+v", file)
	}
}

func TestGetStartupFileRejectsNonWhitelisted(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "notes.txt"), "secret")

	repo := GetAgentFileRepository()
	_, err := repo.GetStartupFile(root, "notes.txt")
	if err == nil {
		t.Fatal("expected error for non-whitelisted file")
	}
	if !errors.Is(err, exception.FilePathError) {
		t.Errorf("expected FilePathError, got:%v", err)
	}
}

func TestGetStartupFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()

	// 工作区外的敏感文件
	writeRepoFile(t, filepath.Join(filepath.Dir(root), "AGENTS.md"), "outside")

	repo := GetAgentFileRepository()
	for _, name := range []string{"../AGENTS.md", "sub/AGENTS.md"} {
		if _, err := repo.GetStartupFile(root, name); err == nil {
			t.Errorf("expected error for traversal name:%s", name)
		}
	}
}

func TestGetStartupFileRejectsOversize(t *testing.T) {
	root := t.TempDir()

	big := make([]byte, hCommon.MaxAgentDocSize+1)
	writeRepoFile(t, filepath.Join(root, hCommon.AgentDocFileName), string(big))

	repo := GetAgentFileRepository()
	if _, err := repo.GetStartupFile(root, hCommon.AgentDocFileName); err == nil {
		t.Fatal("expected error for oversize file")
	}
}

func TestGetStartupFileCacheDetectsChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, hCommon.AgentDocFileName)
	writeRepoFile(t, path, "v1")

	repo := GetAgentFileRepository()

	file, err := repo.GetStartupFile(root, hCommon.AgentDocFileName)
	if err != nil || file.Content != "v1" {
		t.Fatalf("first read: content=%q err=%v", file.Content, err)
	}

	// 修改文件(size 变化 → 指纹变化)后,再次读取必须拿到新内容,不能命中旧缓存
	if err := os.WriteFile(path, []byte("v2-longer-content"), 0o644); err != nil {
		t.Fatalf("write error:%s", err.Error())
	}

	file2, err := repo.GetStartupFile(root, hCommon.AgentDocFileName)
	if err != nil || file2.Content != "v2-longer-content" {
		t.Fatalf("second read after change: content=%q err=%v", file2.Content, err)
	}
}

func TestGetStartupFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW 仅 Unix 生效")
	}

	root := t.TempDir()
	// 工作区内真实文件 + 指向它的符号链接
	writeRepoFile(t, filepath.Join(root, "real_agents.md"), "secret-content")
	if err := os.Symlink(filepath.Join(root, "real_agents.md"), filepath.Join(root, hCommon.AgentDocFileName)); err != nil {
		t.Fatalf("symlink error:%s", err.Error())
	}

	repo := GetAgentFileRepository()

	// 符号链接在列表中被视为缺失(与正文读取的 O_NOFOLLOW 语义一致)
	for _, doc := range repo.GetStartupFiles(root) {
		if doc.FileName == hCommon.AgentDocFileName && !doc.Missing {
			t.Errorf("symlink AGENTS.md should be missing in list")
		}
	}

	// 符号链接在正文读取(O_NOFOLLOW)中也被视为缺失,不泄漏目标内容
	file, err := repo.GetStartupFile(root, hCommon.AgentDocFileName)
	if err != nil {
		t.Fatalf("GetStartupFile error:%s", err.Error())
	}
	if !file.Missing || len(file.Content) != 0 {
		t.Errorf("symlink should return Missing=true with empty content: %+v", file)
	}
}
